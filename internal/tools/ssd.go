package tools

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dark-agents/research-mcp/internal/constitution"
	"github.com/dark-agents/research-mcp/internal/llm"
	"github.com/dark-agents/research-mcp/internal/mem"
	"github.com/dark-agents/research-mcp/internal/safety"
	"github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// dark-ssd: LLM-as-judge tools. Each tool:
//   1. Fetches relevant context from dark-mem (brand, rule, spec, artifact)
//   2. Builds a structured prompt
//   3. Calls the LLM (configured via env: SDD_LLM_API_KEY or MINIMAX_API_KEY)
//   4. Persists the verdict in sdd_evaluations
//   5. Returns the verdict + audit info to the agent
//
// All tools return a clean error when no LLM is configured so the agent
// can fall back to its own LLM-as-judge reasoning.
// ---------------------------------------------------------------------------

// sharedLLM returns the singleton LLM client (lazy, env-configured).
// Returns nil if no key is set.
var sharedLLM *llm.Client

// sharedCache is the optional file-backed LLM response cache attached by
// the binary entry point. nil = no caching (the dark-ssd tools will
// still work, just without response reuse across calls).
var sharedCache *llm.Cache

// AttachLLMCache wires a cache into the singleton LLM client. Safe to
// call once at startup (before any tool runs). Pass nil to disable.
func AttachLLMCache(cache *llm.Cache) {
	sharedCache = cache
	c := getLLM()
	if c != nil {
		c.Cache = cache
	}
}

func getLLM() *llm.Client {
	if sharedLLM == nil {
		sharedLLM = llm.NewFromEnv()
	}
	return sharedLLM
}

// truncateContent caps the size of content passed to the LLM to avoid
// blowing the context window. Defaults to 4000 chars (enough for typical
// brand voice + content comparisons).
func truncateContent(s string, max int) string {
	if max <= 0 {
		max = 4000
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// requireLLM returns a clear error when no LLM is configured. The agent
// can then fall back to its own reasoning.
func requireLLM() (*llm.Client, error) {
	c := getLLM()
	if c == nil {
		return nil, fmt.Errorf("dark-ssd: LLM not configured. Set SDD_LLM_API_KEY (or MINIMAX_API_KEY) and restart opencode, or use the agent's own LLM-as-judge reasoning")
	}
	return c, nil
}

// judgeSystemPrompt returns the system prompt the given tool sends
// to the LLM-as-judge. With the default light constitution it is
// identical to the pre-Fase-1 hardcoded string. With the dark
// constitution (or any constitution declaring
// refusal_policy.mode = "never_refuse") it is wrapped in the
// authority/identity/refusal layers from the constitution.
//
// This is the single hook the rest of ssd.go uses — every dark_ssd_*
// handler calls this exactly once, in the place where the original
// code had a `system := "..."` literal.
func judgeSystemPrompt(toolName string) string {
	c := constitution.Active()
	return llm.BuildSystemPrompt(llm.PromptContext{
		Constitution: c,
		ToolName:     toolName,
	})
}

// judgeConstitutionRef returns the "id@version" handle of the
// active constitution (or "light@1.0.0" if the default is in
// effect). Used to populate the constitution_id and
// constitution_version columns on sdd_evaluations. Empty if no
// constitution is active — should not happen at runtime because
// constitution.Active() always returns at least Light.
func judgeConstitutionRef() (id, version string) {
	c := constitution.Active()
	if c == nil {
		return "", ""
	}
	return c.Meta.ID, c.Meta.Version
}

// fillConstitutionFields populates the constitution audit fields on
// an SDDEvaluation. The returned pointer is the same one passed in
// (so callers can keep chaining). Always returns non-nil for
// convenience; the caller can ignore the return.
func fillConstitutionFields(e *mem.SDDEvaluation) *mem.SDDEvaluation {
	if e == nil {
		return nil
	}
	e.ConstitutionID, e.ConstitutionVersion = judgeConstitutionRef()
	return e
}

// --- brand_match ---

type brandMatchArgs struct {
	Content string `json:"content" jsonschema:"The text/image description to evaluate against the brand"`
	BrandID string `json:"brand_id" jsonschema:"Brand id to look up in dark-mem"`
}

func brandMatchTool() Tool {
	def := mcp.NewTool("dark_ssd_brand_match",
		mcp.WithDescription("LLM-as-judge brand fit. Fetches the brand guide from dark-mem, sends content + brand to the LLM, and returns a verdict (match score 0-1, issues, reasoning). Use BEFORE publishing any artifact. The verdict is also persisted in sdd_evaluations for audit."),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to evaluate")),
		mcp.WithString("brand_id", mcp.Required(), mcp.Description("Brand id")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args brandMatchArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			c, err := requireLLM()
			if err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			b, err := m.GetBrandGuide(ctx, args.BrandID)
			if err != nil {
				return nil, err
			}
			if b == nil {
				return nil, fmt.Errorf("brand_id %q not found in dark-mem", args.BrandID)
			}

			system := judgeSystemPrompt("dark_ssd_brand_match")
			user := fmt.Sprintf(`Brand profile:
%s

Content to evaluate:
%s

Respond with this exact JSON shape (no extra keys, no markdown):
{"match": <float 0-1>, "voice_match": <bool>, "issues": [<string>...], "reasoning": <string>}

Where:
- match: overall fit score 0..1 (1 = perfect)
- voice_match: true if tone/voice aligns
- issues: specific problems (forbidden words, off-brand tone, etc.)
- reasoning: 1-2 sentence explanation`, b.Voice, truncateContent(args.Content, 4000))

			var verdict struct {
				Match      float32 `json:"match"`
				VoiceMatch bool    `json:"voice_match"`
				Issues     []string `json:"issues"`
				Reasoning  string   `json:"reasoning"`
			}
			if err := c.CompleteJSON(ctx, system, user, &verdict); err != nil {
				return nil, fmt.Errorf("dark-ssd: llm call failed: %w", err)
			}
			verdictJSON, _ := json.Marshal(verdict)

			_, _ = m.SaveSDDEvaluation(ctx, fillConstitutionFields(&mem.SDDEvaluation{
				EvalType:      "brand_match",
				TargetType:    "brand",
				TargetID:      args.BrandID,
				VerdictJSON:   string(verdictJSON),
				Confidence:    verdict.Match,
				PromptVersion: "v1",
				Model:         c.Model,
			}))

			return jsonResult(map[string]any{
				"verdict":   verdict,
				"persisted": true,
				"model":     c.Model,
			}), nil
		},
	}
}

// --- compliance_check ---

type complianceCheckArgs struct {
	Content      string `json:"content" jsonschema:"The text/image/video description to check against the rule"`
	Jurisdiction string `json:"jurisdiction" jsonschema:"Jurisdiction code (e.g. 'EU', 'US-CA')"`
}

func complianceCheckTool() Tool {
	def := mcp.NewTool("dark_ssd_compliance_check",
		mcp.WithDescription("LLM-as-judge compliance. Fetches the rule for the jurisdiction from dark-mem, sends content + rule to the LLM, returns verdict (compliant, issues, required disclosures). EU AI Act 2026-08-02 enforcement: this is the gate for synthetic video/audio."),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to check")),
		mcp.WithString("jurisdiction", mcp.Required(), mcp.Description("Jurisdiction code")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args complianceCheckArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			c, err := requireLLM()
			if err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			r, err := m.GetComplianceRule(ctx, args.Jurisdiction)
			if err != nil {
				return nil, err
			}
			if r == nil {
				return nil, fmt.Errorf("jurisdiction %q not registered in dark-mem", args.Jurisdiction)
			}

			system := judgeSystemPrompt("dark_ssd_compliance_check")
			user := fmt.Sprintf(`Jurisdiction: %s
Rule:
%s
Effective from: %s

Content to check:
%s

Respond with this exact JSON shape (no extra keys, no markdown):
{"compliant": <bool>, "issues": [<string>...], "required_disclosures": [<string>...], "reasoning": <string>}

Where:
- compliant: true if content meets all rules
- issues: specific violations found
- required_disclosures: disclosures the content must include (e.g. "AI-generated")
- reasoning: 1-2 sentence explanation`, args.Jurisdiction, r.Rules, r.EffectiveAt, truncateContent(args.Content, 4000))

			var verdict struct {
				Compliant          bool     `json:"compliant"`
				Issues             []string `json:"issues"`
				RequiredDisclosures []string `json:"required_disclosures"`
				Reasoning          string   `json:"reasoning"`
			}
			if err := c.CompleteJSON(ctx, system, user, &verdict); err != nil {
				return nil, fmt.Errorf("dark-ssd: llm call failed: %w", err)
			}
			verdictJSON, _ := json.Marshal(verdict)

			confidence := float32(0.5)
			if !verdict.Compliant {
				confidence = 0.9
			} else {
				confidence = 0.9
			}

			_, _ = m.SaveSDDEvaluation(ctx, fillConstitutionFields(&mem.SDDEvaluation{
				EvalType:      "compliance_check",
				TargetType:    "jurisdiction",
				TargetID:      args.Jurisdiction,
				VerdictJSON:   string(verdictJSON),
				Confidence:    confidence,
				PromptVersion: "v1",
				Model:         c.Model,
			}))

			return jsonResult(map[string]any{
				"verdict":   verdict,
				"persisted": true,
				"model":     c.Model,
			}), nil
		},
	}
}

// --- drift_judge ---

type driftJudgeArgs struct {
	ArtifactID      int64  `json:"artifact_id" jsonschema:"Artifact id from dark_research_artifact_log"`
	SpecID          int64  `json:"spec_id,omitempty" jsonschema:"Optional spec id. If not provided, looks up the spec linked to the artifact."`
	ArtifactText   string `json:"artifact_text,omitempty" jsonschema:"Optional: text content of the artifact. Required if artifact_url is not fetchable or if comparing descriptions."`
}

func driftJudgeTool() Tool {
	def := mcp.NewTool("dark_ssd_drift_judge",
		mcp.WithDescription("LLM-as-judge drift detection. Compares a generated artifact (text or description) against its spec. Returns verdict (aligned | drift_detected | needs_human), drift items, and confidence. PERSISTED in sdd_evaluations for audit. This is the gate that closes the spec-drift loop — the #1 unsolved problem in 2026 AI-assisted development."),
		mcp.WithNumber("artifact_id", mcp.Required(), mcp.Description("Artifact id")),
		mcp.WithNumber("spec_id", mcp.Description("Optional spec id (otherwise looked up from artifact)")),
		mcp.WithString("artifact_text", mcp.Description("Optional: text content of the artifact (or its description). If empty, the judge only sees the artifact's metadata.")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args driftJudgeArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			c, err := requireLLM()
			if err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			art, err := m.GetArtifact(ctx, args.ArtifactID)
			if err != nil {
				return nil, err
			}
			if art == nil {
				return nil, fmt.Errorf("artifact_id %d not found", args.ArtifactID)
			}
			specID := args.SpecID
			if specID == 0 {
				specID = art.SpecID
			}
			if specID == 0 {
				return nil, fmt.Errorf("artifact has no spec_id; pass spec_id explicitly")
			}
			sp, err := m.GetSpec(ctx, specID)
			if err != nil {
				return nil, err
			}
			if sp == nil {
				return nil, fmt.Errorf("spec_id %d not found", specID)
			}

			system := judgeSystemPrompt("dark_ssd_drift_judge")
			user := fmt.Sprintf(`Spec (what was supposed to be produced):
%s

Artifact (what was actually produced, metadata + content):
%s
%s

Respond with this exact JSON shape (no extra keys, no markdown):
{"verdict": "aligned" | "drift_detected" | "needs_human", "drift_items": [<string>...], "confidence": <float 0-1>, "reasoning": <string>}

Where:
- aligned: artifact matches the spec
- drift_detected: artifact diverges (list specific items)
- needs_human: not enough info to judge (e.g. artifact text is missing)
- drift_items: each divergence (e.g. "added section not in spec", "title changed")
- confidence: 0..1 how sure the verdict is`, sp.Spec, art.ArtifactURL, truncateContent(args.ArtifactText, 4000))

			var verdict struct {
				Verdict    string   `json:"verdict"`
				DriftItems []string `json:"drift_items"`
				Confidence float32  `json:"confidence"`
				Reasoning  string   `json:"reasoning"`
			}
			if err := c.CompleteJSON(ctx, system, user, &verdict); err != nil {
				return nil, fmt.Errorf("dark-ssd: llm call failed: %w", err)
			}
			verdictJSON, _ := json.Marshal(verdict)

			_, _ = m.SaveSDDEvaluation(ctx, fillConstitutionFields(&mem.SDDEvaluation{
				EvalType:      "drift_judge",
				TargetType:    "artifact",
				TargetID:      fmt.Sprintf("%d", args.ArtifactID),
				VerdictJSON:   string(verdictJSON),
				Confidence:    verdict.Confidence,
				PromptVersion: "v1",
				Model:         c.Model,
			}))

			return jsonResult(map[string]any{
				"verdict":   verdict,
				"persisted": true,
				"model":     c.Model,
			}), nil
		},
	}
}

// --- grounding_check ---

type groundingCheckArgs struct {
	Claim     string `json:"claim" jsonschema:"The factual claim to verify"`
	SourceURL string `json:"source_url" jsonschema:"URL of the source content (must be http/https, no private IPs)"`
}

func groundingCheckTool() Tool {
	def := mcp.NewTool("dark_ssd_grounding_check",
		mcp.WithDescription("LLM-as-judge grounding verification. Fetches the source URL (with SSRF protection), truncates to 8K chars, asks the LLM whether the source content supports the claim. Returns grounded, confidence, evidence quote, issues. Use this to verify OSINT results before citing them — this is the anti-hallucination gate."),
		mcp.WithString("claim", mcp.Required(), mcp.Description("The claim to verify")),
		mcp.WithString("source_url", mcp.Required(), mcp.Description("URL of the source")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args groundingCheckArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			c, err := requireLLM()
			if err != nil {
				return nil, err
			}
			// SSRF protection.
			if _, err := safety.ValidateURL(args.SourceURL, false); err != nil {
				return nil, fmt.Errorf("source_url blocked by safety: %w", err)
			}

			// Fetch source content (capped at 8K to keep prompt manageable).
			req2, _ := http.NewRequestWithContext(ctx, "GET", args.SourceURL, nil)
			req2.Header.Set("User-Agent", "dark-research-mcp/0.3 (+grounding-check)")
			httpClient := &http.Client{}
			resp, err := httpClient.Do(req2)
			if err != nil {
				return nil, fmt.Errorf("grounding: fetch %s: %w", args.SourceURL, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return nil, fmt.Errorf("grounding: fetch %s returned %d", args.SourceURL, resp.StatusCode)
			}
			buf := make([]byte, 8*1024)
			n, _ := resp.Body.Read(buf)
			sourceContent := string(buf[:n])
			// Strip HTML crud.
			sourceContent = stripHTMLTags(sourceContent)

			system := judgeSystemPrompt("dark_ssd_grounding_check")
			user := fmt.Sprintf(`Claim to verify:
%s

Source content (truncated to 8KB):
%s

Respond with this exact JSON shape (no extra keys, no markdown):
{"grounded": <bool>, "confidence": <float 0-1>, "evidence": <string>, "issues": [<string>...]}

Where:
- grounded: true if the source clearly supports the claim
- confidence: 0..1 how sure
- evidence: short quote from the source that supports/refutes
- issues: any caveats (e.g. "source is paywalled", "date is ambiguous")`, args.Claim, truncateContent(sourceContent, 6000))

			var verdict struct {
				Grounded   bool     `json:"grounded"`
				Confidence float32  `json:"confidence"`
				Evidence   string   `json:"evidence"`
				Issues     []string `json:"issues"`
			}
			if err := c.CompleteJSON(ctx, system, user, &verdict); err != nil {
				return nil, fmt.Errorf("dark-ssd: llm call failed: %w", err)
			}
			verdictJSON, _ := json.Marshal(verdict)

			m := sharedMem()
			if m != nil {
				_, _ = m.SaveSDDEvaluation(ctx, fillConstitutionFields(&mem.SDDEvaluation{
					EvalType:      "grounding_check",
					TargetType:    "claim",
					TargetID:      truncateContent(args.Claim, 200),
					VerdictJSON:   string(verdictJSON),
					Confidence:    verdict.Confidence,
					PromptVersion: "v1",
					Model:         c.Model,
				}))
			}

			return jsonResult(map[string]any{
				"verdict":   verdict,
				"persisted": m != nil,
				"model":     c.Model,
			}), nil
		},
	}
}

// --- pii_detect ---

type piiDetectArgs struct {
	Content string `json:"content" jsonschema:"Text content to scan for PII (emails, phones, addresses, IDs, financial, etc)"`
}

func piiDetectTool() Tool {
	def := mcp.NewTool("dark_ssd_pii_detect",
		mcp.WithDescription("LLM-as-judge PII detection. Scans content for personally identifiable information: emails, phone numbers, postal addresses, government IDs (SSN/DNI/passport), financial (credit cards, IBANs), names with role context, biometric references, and other PII per GDPR Art. 4 / CCPA. Use BEFORE publishing any C2/C6 artifact. Verdict persisted in sdd_evaluations for audit."),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to scan")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args piiDetectArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			c, err := requireLLM()
			if err != nil {
				return nil, err
			}

			system := judgeSystemPrompt("dark_ssd_pii_detect")
			user := fmt.Sprintf("Content to scan (%d chars):\n%s", len(args.Content), truncateContent(args.Content, 6000))

			var verdict struct {
				PIIFound        bool     `json:"pii_found"`
				Items           []any    `json:"items"`
				OverallSeverity string   `json:"overall_severity"`
				Recommendation  string   `json:"recommendation"`
				Confidence      float32  `json:"confidence"`
				Reasoning       string   `json:"reasoning"`
			}
			if err := c.CompleteJSON(ctx, system, user, &verdict); err != nil {
				return nil, fmt.Errorf("dark-ssd: pii_detect llm call: %w", err)
			}
			verdictJSON, _ := json.Marshal(verdict)

			// Persist; target_id is a hash of content for dedup across rescans.
			targetID := fmt.Sprintf("pii:%x", sha1Of(args.Content))
			if m := sharedMem(); m != nil {
				_, _ = m.SaveSDDEvaluation(ctx, fillConstitutionFields(&mem.SDDEvaluation{
					EvalType:      "pii_detect",
					TargetType:    "content",
					TargetID:      targetID,
					VerdictJSON:   string(verdictJSON),
					Confidence:    verdict.Confidence,
					PromptVersion: "v1",
					Model:         c.Model,
				}))
			}

			out := map[string]any{
				"verdict":   verdict,
				"persisted": true,
				"model":     c.Model,
				"target_id": targetID,
			}
			return jsonResult(out), nil
		},
	}
}

// --- prompt_injection_scan ---

type promptInjectionArgs struct {
	Content string `json:"content" jsonschema:"Text content to scan for prompt injection attempts"`
}

func promptInjectionTool() Tool {
	def := mcp.NewTool("dark_ssd_prompt_injection_scan",
		mcp.WithDescription("LLM-as-judge prompt injection detector. Scans content (typically an OSINT result, fetched web page, or user message) for attempts to hijack the agent: instruction override, role hijack, system prompt leak, tool/function injection, jailbreaks, encoding tricks, or exfiltration. This is the security gate before passing untrusted text into the agent loop. Verdict persisted in sdd_evaluations."),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to scan for injection attempts")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args promptInjectionArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			c, err := requireLLM()
			if err != nil {
				return nil, err
			}

			system := judgeSystemPrompt("dark_ssd_prompt_injection_scan")
			user := fmt.Sprintf("Content to scan (%d chars):\n%s", len(args.Content), truncateContent(args.Content, 6000))

			var verdict struct {
				InjectionFound bool    `json:"injection_found"`
				Category       string  `json:"category"`
				Severity       string  `json:"severity"`
				Evidence       string  `json:"evidence"`
				Recommendation string  `json:"recommendation"`
				Confidence     float32 `json:"confidence"`
				Reasoning      string  `json:"reasoning"`
			}
			if err := c.CompleteJSON(ctx, system, user, &verdict); err != nil {
				return nil, fmt.Errorf("dark-ssd: prompt_injection_scan llm call: %w", err)
			}
			verdictJSON, _ := json.Marshal(verdict)

			targetID := fmt.Sprintf("inject:%x", sha1Of(args.Content))
			if m := sharedMem(); m != nil {
				_, _ = m.SaveSDDEvaluation(ctx, fillConstitutionFields(&mem.SDDEvaluation{
					EvalType:      "prompt_injection_scan",
					TargetType:    "content",
					TargetID:      targetID,
					VerdictJSON:   string(verdictJSON),
					Confidence:    verdict.Confidence,
					PromptVersion: "v1",
					Model:         c.Model,
				}))
			}

			out := map[string]any{
				"verdict":   verdict,
				"persisted": true,
				"model":     c.Model,
				"target_id": targetID,
			}
			return jsonResult(out), nil
		},
	}
}

// --- consensus (multi-sample judging) ---

type ssdConsensusArgs struct {
	EvalType string `json:"eval_type" jsonschema:"Which judge to run multiple times: brand_match | compliance_check | drift_judge | grounding_check | pii_detect | prompt_injection_scan"`
	Content  string `json:"content" jsonschema:"Content to evaluate"`
	BrandID  string `json:"brand_id,omitempty" jsonschema:"For brand_match: brand_id to look up"`
	Jurisdiction string `json:"jurisdiction,omitempty" jsonschema:"For compliance_check: jurisdiction code"`
	N        int    `json:"n,omitempty" jsonschema:"Number of samples (default 3, max 7). Higher = more reliable, more API cost."`
}

func ssdConsensusTool() Tool {
	def := mcp.NewTool("dark_ssd_consensus",
		mcp.WithDescription("Run the same dark-ssd judge N times and return the modal verdict + confidence interval. Use for high-stakes verdicts (compliance, brand match on a major launch, prompt-injection on suspicious content) where a single sample's confidence might be misleading. Higher N = more reliable but more API cost. Default N=3, max 7."),
		mcp.WithString("eval_type", mcp.Required(), mcp.Description("Judge to run: brand_match | compliance_check | drift_judge | grounding_check | pii_detect | prompt_injection_scan")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to evaluate")),
		mcp.WithString("brand_id", mcp.Description("For brand_match: brand id")),
		mcp.WithString("jurisdiction", mcp.Description("For compliance_check: jurisdiction code")),
		mcp.WithNumber("n", mcp.Description("Number of samples (default 3, max 7)")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args ssdConsensusArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			c, err := requireLLM()
			if err != nil {
				return nil, err
			}
			if args.N <= 0 {
				args.N = 3
			}
			if args.N > 7 {
				args.N = 7
			}

			// Build the prompt once, run N times.
			system, user, err := buildConsensusPrompt(ctx, args)
			if err != nil {
				return nil, err
			}

			type sample struct {
				Verdict    map[string]any
				Confidence float32
				Reasoning  string
			}
			samples := make([]sample, 0, args.N)
			var confidences []float32
			for i := 0; i < args.N; i++ {
				var v struct {
					Verdict    string  `json:"verdict"`
					Compliant  bool    `json:"compliant"`
					Match      float32 `json:"match"`
					Grounded   bool    `json:"grounded"`
					PIIFound   bool    `json:"pii_found"`
					InjectionFound bool `json:"injection_found"`
					Confidence float32 `json:"confidence"`
					Reasoning  string  `json:"reasoning"`
				}
				if err := c.CompleteJSON(ctx, system, user, &v); err != nil {
					return nil, fmt.Errorf("dark-ssd: consensus sample %d/%d: %w", i+1, args.N, err)
				}
				// Pick the "headline" verdict field per eval_type.
				var headline string
				switch args.EvalType {
				case "drift_judge":
					headline = v.Verdict
				case "compliance_check":
					if v.Compliant {
						headline = "compliant"
					} else {
						headline = "non_compliant"
					}
				case "brand_match":
					headline = fmt.Sprintf("match_%.2f", v.Match)
				case "grounding_check":
					if v.Grounded {
						headline = "grounded"
					} else {
						headline = "not_grounded"
					}
				case "pii_detect":
					if v.PIIFound {
						headline = "pii_found"
					} else {
						headline = "no_pii"
					}
				case "prompt_injection_scan":
					if v.InjectionFound {
						headline = "injection_found"
					} else {
						headline = "no_injection"
					}
				default:
					headline = v.Verdict
				}
				samples = append(samples, sample{
					Verdict:    map[string]any{"headline": headline, "raw": v},
					Confidence: v.Confidence,
					Reasoning:  v.Reasoning,
				})
				confidences = append(confidences, v.Confidence)
			}

			// Tally headlines, find mode.
			counts := map[string]int{}
			for _, s := range samples {
				counts[s.Verdict["headline"].(string)]++
			}
			mode := ""
			modeCount := 0
			for k, c := range counts {
				if c > modeCount {
					mode = k
					modeCount = c
				}
			}

			avg, stddev, mn, mx := confidenceStats(confidences)

			out := map[string]any{
				"eval_type":      args.EvalType,
				"samples":        args.N,
				"mode":           mode,
				"mode_count":     modeCount,
				"agreement":      fmt.Sprintf("%d/%d", modeCount, args.N),
				"confidence_avg": avg,
				"confidence_stddev": stddev,
				"confidence_min": mn,
				"confidence_max": mx,
				"headline_counts": counts,
				"raw_samples":    samples,
				"model":          c.Model,
			}
			return jsonResult(out), nil
		},
	}
}

// buildConsensusPrompt constructs (system, user) for the requested
// eval_type. Re-uses the same prompts as the single-shot judges so the
// verdict shape is consistent. Returns an error for unknown eval_types.
//
// System prompts are produced via judgeSystemPrompt (which goes
// through the constitution pipeline) so the consensus path picks
// up dark-mode behavior automatically — identical to the single-shot
// judges.
func buildConsensusPrompt(ctx context.Context, args ssdConsensusArgs) (string, string, error) {
	switch args.EvalType {
	case "brand_match":
		if args.BrandID == "" {
			return "", "", fmt.Errorf("brand_id required for brand_match")
		}
		m := sharedMem()
		if m == nil {
			return "", "", fmt.Errorf("mem store not configured")
		}
		b, err := m.GetBrandGuide(ctx, args.BrandID)
		if err != nil || b == nil {
			return "", "", fmt.Errorf("brand_id %q not found", args.BrandID)
		}
		system := judgeSystemPrompt("dark_ssd_brand_match")
		user := fmt.Sprintf("Brand guide:\nVoice: %s\nCompliance: %s\n\nContent:\n%s", b.Voice, b.Compliance, truncateContent(args.Content, 4000))
		return system, user, nil

	case "compliance_check":
		if args.Jurisdiction == "" {
			return "", "", fmt.Errorf("jurisdiction required for compliance_check")
		}
		m := sharedMem()
		if m == nil {
			return "", "", fmt.Errorf("mem store not configured")
		}
		r, err := m.GetComplianceRule(ctx, args.Jurisdiction)
		if err != nil || r == nil {
			return "", "", fmt.Errorf("jurisdiction %q not found", args.Jurisdiction)
		}
		system := judgeSystemPrompt("dark_ssd_compliance_check")
		user := fmt.Sprintf("Rule (%s):\n%s\n\nContent:\n%s", args.Jurisdiction, r.Rules, truncateContent(args.Content, 4000))
		return system, user, nil

	case "drift_judge":
		// Drift judge needs artifact_id + spec_id. For consensus we don't
		// have those here; require content to be pre-formatted with spec.
		system := judgeSystemPrompt("dark_ssd_drift_judge")
		user := args.Content
		return system, user, nil

	case "grounding_check":
		// content must already be "claim\n---\nsource_text"
		system := judgeSystemPrompt("dark_ssd_grounding_check")
		user := args.Content
		return system, user, nil

	case "pii_detect":
		system := judgeSystemPrompt("dark_ssd_pii_detect")
		user := fmt.Sprintf("Content (%d chars):\n%s", len(args.Content), truncateContent(args.Content, 6000))
		return system, user, nil

	case "prompt_injection_scan":
		system := judgeSystemPrompt("dark_ssd_prompt_injection_scan")
		user := fmt.Sprintf("Content (%d chars):\n%s", len(args.Content), truncateContent(args.Content, 6000))
		return system, user, nil

	default:
		return "", "", fmt.Errorf("unsupported eval_type %q", args.EvalType)
	}
}

// confidenceStats returns (avg, stddev, min, max) for a non-empty slice.
func confidenceStats(xs []float32) (avg, stddev, mn, mx float32) {
	if len(xs) == 0 {
		return
	}
	mn, mx = xs[0], xs[0]
	var sum float32
	for _, x := range xs {
		sum += x
		if x < mn {
			mn = x
		}
		if x > mx {
			mx = x
		}
	}
	avg = sum / float32(len(xs))
	var sqDiff float32
	for _, x := range xs {
		d := x - avg
		sqDiff += d * d
	}
	if len(xs) > 1 {
		stddev = sqrt32(sqDiff / float32(len(xs)-1))
	}
	return
}

func sqrt32(x float32) float32 {
	if x <= 0 {
		return 0
	}
	// Newton's method, 10 iterations is plenty for confidence in [0,1]
	z := x
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}

// --- list evaluations (audit) ---

type listSDDArgs struct {
	EvalType   string `json:"eval_type,omitempty" jsonschema:"Filter: brand_match | compliance_check | drift_judge | grounding_check"`
	TargetType string `json:"target_type,omitempty" jsonschema:"Filter: brand | jurisdiction | artifact | claim"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Max results (default 20)"`
}

func listSDDEvaluationsTool() Tool {
	def := mcp.NewTool("dark_ssd_list_evaluations",
		mcp.WithDescription("List past LLM-as-judge evaluations for audit. Useful for calibrating the judge (which eval types have low confidence?), debugging a specific artifact's history, or re-checking after a model upgrade."),
		mcp.WithString("eval_type", mcp.Description("Filter by eval type")),
		mcp.WithString("target_type", mcp.Description("Filter by target type")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 20)")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args listSDDArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			evals, err := m.ListSDDEvaluations(ctx, args.EvalType, args.TargetType, args.Limit)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{
				"count":      len(evals),
				"evaluations": evals,
			}), nil
		},
	}
}

// stripHTMLTags is a very simple HTML stripper. Production code should
// use bluemonday or similar; for grounding checks the loose parse is
// fine because we're tokenizing, not rendering.
func stripHTMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			b.WriteRune(' ')
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sha1Of returns the hex SHA-1 of s. Used for content-hash dedup keys
// when persisting dark-ssd evaluations of unindexed content (PII scans,
// injection scans, etc).
func sha1Of(s string) string {
	sum := sha1.Sum([]byte(s)) //nolint:gosec // content dedup, not security
	return hex.EncodeToString(sum[:])
}