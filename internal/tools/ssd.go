package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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

			system := "You are a strict brand compliance judge. Score how well content matches a brand profile. Respond with JSON only, no prose, no markdown fences."
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

			_, _ = m.SaveSDDEvaluation(ctx, &mem.SDDEvaluation{
				EvalType:      "brand_match",
				TargetType:    "brand",
				TargetID:      args.BrandID,
				VerdictJSON:   string(verdictJSON),
				Confidence:    verdict.Match,
				PromptVersion: "v1",
				Model:         c.Model,
			})

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

			system := "You are a strict regulatory compliance judge. Evaluate content against the given rule. Respond with JSON only, no prose, no markdown fences."
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

			_, _ = m.SaveSDDEvaluation(ctx, &mem.SDDEvaluation{
				EvalType:      "compliance_check",
				TargetType:    "jurisdiction",
				TargetID:      args.Jurisdiction,
				VerdictJSON:   string(verdictJSON),
				Confidence:    confidence,
				PromptVersion: "v1",
				Model:         c.Model,
			})

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

			system := "You are a strict spec-vs-artifact drift detector. Compare a spec's intent against what was actually produced. Respond with JSON only, no prose, no markdown fences."
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

			_, _ = m.SaveSDDEvaluation(ctx, &mem.SDDEvaluation{
				EvalType:      "drift_judge",
				TargetType:    "artifact",
				TargetID:      fmt.Sprintf("%d", args.ArtifactID),
				VerdictJSON:   string(verdictJSON),
				Confidence:    verdict.Confidence,
				PromptVersion: "v1",
				Model:         c.Model,
			})

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

			system := "You are a strict fact-checker. Verify whether a source content supports a specific claim. Respond with JSON only, no prose, no markdown fences."
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
				_, _ = m.SaveSDDEvaluation(ctx, &mem.SDDEvaluation{
					EvalType:      "grounding_check",
					TargetType:    "claim",
					TargetID:      truncateContent(args.Claim, 200),
					VerdictJSON:   string(verdictJSON),
					Confidence:    verdict.Confidence,
					PromptVersion: "v1",
					Model:         c.Model,
				})
			}

			return jsonResult(map[string]any{
				"verdict":   verdict,
				"persisted": m != nil,
				"model":     c.Model,
			}), nil
		},
	}
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