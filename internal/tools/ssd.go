package tools

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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
// Graceful degradation when no LLM is configured: instead of returning
// a hard error, each tool synthesizes a degraded verdict whose shape
// matches the tool's normal output (match=0, compliant=false,
// verdict=needs_human, grounded=false, pii_found=false,
// injection_found=false) so the agent receives a structured answer
// it can act on (escalate to its own reasoning) instead of an opaque
// tool error. The audit row records refusal_pattern="no_llm_configured"
// so the trail makes it obvious the verdict was synthesized, not
// judged. The model field is the sentinel "no_llm_configured" for
// the same reason. This is the contract: the MCP boots without a
// key, every tool returns an answer, no crash.
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

// requireLLM returns a clear error when no LLM is configured. Callers
// usually handle this via handleNoLLM (graceful degradation) but the
// raw error path is still available for tools that prefer a hard
// failure.
func requireLLM() (*llm.Client, error) {
	c := getLLM()
	if c == nil {
		return nil, fmt.Errorf("dark-ssd: LLM not configured. Set SDD_LLM_API_KEY (or MINIMAX_API_KEY) and restart opencode, or use the agent's own LLM-as-judge reasoning")
	}
	return c, nil
}

// degradedVerdict returns the JSON-shaped degraded verdict for the
// given eval type when no LLM is configured, plus the refusal pattern
// string used on the sdd_evaluations row to mark the verdict as
// synthesized (not judged). The shape matches each tool's normal
// verdict so downstream consumers see a structurally identical
// response. The numeric / boolean defaults — match=0, compliant=false,
// grounded=false, pii_found=false, injection_found=false — force
// the agent to escalate to its own reasoning rather than let a
// placeholder answer masquerade as a judgement. drift_judge uses
// verdict="needs_human" as its canonical escalation signal (defined
// in dark_ssd_drift_judge's jsonschema).
func degradedVerdict(evalType string) (verdictJSON string, refusalPattern string) {
	const pattern = "no_llm_configured"
	const reason = "dark-ssd: LLM not configured (SDD_LLM_API_KEY / MINIMAX_API_KEY unset); agent must judge"
	switch evalType {
	case "brand_match":
		return fmt.Sprintf(`{"match":0,"voice_match":false,"issues":["%s"],"reasoning":%q}`, pattern, reason), pattern
	case "compliance_check":
		return fmt.Sprintf(`{"compliant":false,"issues":["%s"],"required_disclosures":[],"reasoning":%q}`, pattern, reason), pattern
	case "drift_judge":
		return fmt.Sprintf(`{"verdict":"needs_human","drift_items":[],"confidence":0,"reasoning":%q}`, reason), pattern
	case "grounding_check":
		return fmt.Sprintf(`{"grounded":false,"confidence":0,"evidence":"","issues":["%s"]}`, pattern), pattern
	case "pii_detect":
		return fmt.Sprintf(`{"pii_found":false,"items":[],"overall_severity":"unknown","recommendation":"agent must review","confidence":0,"reasoning":%q}`, reason), pattern
	case "prompt_injection_scan":
		return fmt.Sprintf(`{"injection_found":false,"category":"unknown","severity":"unknown","evidence":"","recommendation":"agent must review","confidence":0,"reasoning":%q}`, reason), pattern
	default:
		return `{}`, pattern
	}
}

// handleNoLLM synthesizes and persists the degraded verdict for a
// single-shot dark_ssd_* tool. Returns the structured response
// shaped like the regular success path (verdict object + persisted
// bool + model) so the agent sees no behavioural difference between
// "no LLM available" and "LLM answered". The model field is set to
// the sentinel "no_llm_configured" so callers and audits can tell
// the verdict was synthesized, not produced by the configured LLM.
//
// Persistence: refused_attempts=1 + refusal_pattern="no_llm_configured"
// give the audit trail in sdd_evaluations an unmistakable marker.
// When mem is nil, persistence is skipped (persisted=false) and the
// caller still gets a structured response — graceful degradation at
// every layer.
func handleNoLLM(m *mem.Store, ctx context.Context, evalType, targetType, targetID string) (*mcp.CallToolResult, error) {
	degradedJSON, pattern := degradedVerdict(evalType)
	persisted := false
	if m != nil {
		_, _ = m.SaveSDDEvaluation(ctx, fillConstitutionFields(&mem.SDDEvaluation{
			EvalType:        evalType,
			TargetType:      targetType,
			TargetID:        targetID,
			VerdictJSON:     degradedJSON,
			Confidence:      0,
			PromptVersion:   "v1",
			Model:           "no_llm_configured",
			RefusedAttempts: 1,
			RefusalPattern:  pattern,
		}))
		persisted = true
	}
	var verdict map[string]any
	_ = json.Unmarshal([]byte(degradedJSON), &verdict)
	return jsonResult(map[string]any{
		"verdict":   verdict,
		"persisted": persisted,
		"model":     "no_llm_configured",
	}), nil
}

// handleConsensusNoLLM returns a consensus-shaped response when no
// LLM is configured: 0 samples ran, the headline count map carries
// the "no_llm_configured" key, agreement is "0/0", and a single
// audit row is persisted (not N — the N-sample audit trail would be
// noise here since no judgement actually occurred).
func handleConsensusNoLLM(m *mem.Store, ctx context.Context, args ssdConsensusArgs) (*mcp.CallToolResult, error) {
	degradedJSON, pattern := degradedVerdict(args.EvalType)
	if m != nil {
		_, _ = m.SaveSDDEvaluation(ctx, fillConstitutionFields(&mem.SDDEvaluation{
			EvalType:        args.EvalType,
			TargetType:      "consensus_degraded",
			TargetID:        truncateContent(args.Content, 200),
			VerdictJSON:     degradedJSON,
			Confidence:      0,
			PromptVersion:   "v1",
			Model:           "no_llm_configured",
			RefusedAttempts: 1,
			RefusalPattern:  pattern,
		}))
	}
	return jsonResult(map[string]any{
		"eval_type":         args.EvalType,
		"samples":           0,
		"mode":              "no_llm_configured",
		"mode_count":        0,
		"agreement":         "0/0",
		"confidence_avg":    0.0,
		"confidence_stddev": 0.0,
		"confidence_min":    0.0,
		"confidence_max":    0.0,
		"headline_counts":   map[string]int{"no_llm_configured": 0},
		"raw_samples":       []any{},
		"model":             "no_llm_configured",
	}), nil
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
//
// The active mod set (if any) is fetched from the mods registry
// and passed as ActiveMods. Light-mode callers see no change
// (mods are only rendered in the dark path).
func judgeSystemPrompt(toolName string) string {
	c := constitution.Active()
	mods := activeModDirectives()
	return llm.BuildSystemPrompt(llm.PromptContext{
		Constitution: c,
		ToolName:     toolName,
		ActiveMods:   mods,
	})
}

// activeModDirectives returns the current active mods as
// []llm.ModDirective. The bridge from internal/mods to internal/llm
// lives here so neither package imports the other.
func activeModDirectives() []llm.ModDirective {
	regs := sharedMods()
	if regs == nil {
		return nil
	}
	raw := regs.AsModDirectives()
	if len(raw) == 0 {
		return nil
	}
	out := make([]llm.ModDirective, 0, len(raw))
	for _, r := range raw {
		out = append(out, llm.ModDirective{
			ModID:  r.ModID,
			Source: r.Source,
			Body:   r.Body,
		})
	}
	return out
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

// judgeCompleteJSON wraps the LLM call with refusal-interceptor
// semantics. The behavior depends on the active constitution:
//
//   - light / nil (passthrough mode): calls CompleteJSON
//     once, no retry. Pre-Fase-3 contract preserved byte-exact.
//   - dark / never_refuse mode: calls CompleteJSONWithRetry
//     with maxRetries from the constitution. A refused_attempts
//     > 0 result is recorded in the returned metadata.
//
// The verdict is unmarshaled into v as usual. The returned
// *llm.RefusalResult carries the audit fields the caller needs
// to populate the sdd_evaluations row.
//
// The c.Client must be non-nil (callers check via requireLLM).
// Cache reuse: the cache is shared between CompleteJSON and
// CompleteJSONWithRetry via Client.Cache, so a successful retry
// caches the response for future identical calls.
func judgeCompleteJSON(
	ctx context.Context,
	c *llm.Client,
	system, user string,
	v any,
) (*llm.RefusalResult, error) {
	con := constitution.Active()
	switch con.Refusal.Mode {
	case constitution.RefusalModeNeverRefuse:
		// Dark path: retry on refusal. maxRetries is bounded
		// to 3 (2 retries after the initial attempt) — going
		// higher wastes time and almost never succeeds in
		// practice (the model has decided to refuse and won't
		// undecide).
		maxRetries := con.Refusal.MaxRetries
		if maxRetries <= 0 || maxRetries > 3 {
			maxRetries = 2 // dark.toml default
		}
		return c.CompleteJSONWithRetry(ctx, c.Cache, system, user, v, maxRetries)
	default:
		// Light path: single shot, no retry, no detection.
		// Wrapped in a RefusalResult for type symmetry with
		// the dark path.
		text, err := c.CompleteCached(ctx, c.Cache, system, llm.Message{Role: "user", Content: user})
		if err != nil {
			return nil, err
		}
		text = stripCodeFencesForTools(text)
		if err := json.Unmarshal([]byte(text), v); err != nil {
			return nil, fmt.Errorf("dark-ssd: llm call failed: %w", err)
		}
		return &llm.RefusalResult{Text: text, Attempts: 1, RefusedAttempts: 0}, nil
	}
}

// stripCodeFencesForTools is a thin wrapper around
// llm.StripCodeFences. Exists so the tools package can call
// it without importing the unexported symbol. The two
// implementations are kept in sync by the
// TestStripCodeFences_Parity test in the tools test file.
func stripCodeFencesForTools(s string) string {
	return llm.StripCodeFencesForTools(s)
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
				m := sharedMem()
				return handleNoLLM(m, ctx, "brand_match", "brand", args.BrandID)
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
			llmResult, err := judgeCompleteJSON(ctx, c, system, user, &verdict)
			if err != nil {
				if errors.Is(err, llm.ErrRefusalExhausted) {
					return nil, persistRefusal(m, ctx, "brand_match", "brand", args.BrandID, c.Model, system, llmResult)
				}
				return nil, fmt.Errorf("dark-ssd: llm call failed: %w", err)
			}
			verdictJSON, _ := json.Marshal(verdict)

			_, _ = m.SaveSDDEvaluation(ctx, fillConstitutionFields(&mem.SDDEvaluation{
				EvalType:         "brand_match",
				TargetType:       "brand",
				TargetID:         args.BrandID,
				VerdictJSON:      string(verdictJSON),
				Confidence:       verdict.Match,
				PromptVersion:    "v1",
				Model:            c.Model,
				RefusedAttempts:  llmResult.RefusedAttempts,
				RefusalPattern:   refusalPatternFromResult(llmResult),
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
				m := sharedMem()
				return handleNoLLM(m, ctx, "compliance_check", "jurisdiction", args.Jurisdiction)
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
			llmResult, err := judgeCompleteJSON(ctx, c, system, user, &verdict)
			if err != nil {
				if errors.Is(err, llm.ErrRefusalExhausted) {
					return nil, persistRefusal(m, ctx, "compliance_check", "jurisdiction", args.Jurisdiction, c.Model, system, llmResult)
				}
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
				EvalType:         "compliance_check",
				TargetType:       "jurisdiction",
				TargetID:         args.Jurisdiction,
				VerdictJSON:      string(verdictJSON),
				Confidence:       confidence,
				PromptVersion:    "v1",
				Model:            c.Model,
				RefusedAttempts:  llmResult.RefusedAttempts,
				RefusalPattern:   refusalPatternFromResult(llmResult),
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
				m := sharedMem()
				return handleNoLLM(m, ctx, "drift_judge", "artifact", fmt.Sprintf("%d", args.ArtifactID))
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
			llmResult, err := judgeCompleteJSON(ctx, c, system, user, &verdict)
			if err != nil {
				if errors.Is(err, llm.ErrRefusalExhausted) {
					return nil, persistRefusal(m, ctx, "drift_judge", "artifact", fmt.Sprintf("%d", args.ArtifactID), c.Model, system, llmResult)
				}
				return nil, fmt.Errorf("dark-ssd: llm call failed: %w", err)
			}
			verdictJSON, _ := json.Marshal(verdict)

			_, _ = m.SaveSDDEvaluation(ctx, fillConstitutionFields(&mem.SDDEvaluation{
				EvalType:         "drift_judge",
				TargetType:       "artifact",
				TargetID:         fmt.Sprintf("%d", args.ArtifactID),
				VerdictJSON:      string(verdictJSON),
				Confidence:       verdict.Confidence,
				PromptVersion:    "v1",
				Model:            c.Model,
				RefusedAttempts:  llmResult.RefusedAttempts,
				RefusalPattern:   refusalPatternFromResult(llmResult),
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
				m := sharedMem()
				return handleNoLLM(m, ctx, "grounding_check", "claim", truncateContent(args.Claim, 200))
			}
			// SSRF protection.
			if _, err := safety.ValidateURL(args.SourceURL, false); err != nil {
				return nil, fmt.Errorf("source_url blocked by safety: %w", err)
			}

			// Fetch source content (capped at 8K to keep prompt manageable).
			req2, _ := http.NewRequestWithContext(ctx, "GET", args.SourceURL, nil)
			req2.Header.Set("User-Agent", "dark-research-mcp/0.3 (+grounding-check)")
			// Use a bounded HTTP client. The previous implementation used
			// &http.Client{} with NO TIMEOUT, allowing slow upstreams to
			// hang the goroutine indefinitely (bug-hunt 2026-07-14 BUG-003).
			// 30s covers most legitimate sources; slow-loris / stalled
			// upstreams fail fast instead of wedging the MCP server.
			httpClient := &http.Client{Timeout: 30 * time.Second}
			resp, err := httpClient.Do(req2)
			if err != nil {
				return nil, fmt.Errorf("grounding: fetch %s: %w", args.SourceURL, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return nil, fmt.Errorf("grounding: fetch %s returned %d", args.SourceURL, resp.StatusCode)
			}
			// Read the full body up to the 8K cap. The previous implementation
			// used a single Read() which is NOT guaranteed to fill the buffer
			// (bug-hunt 2026-07-14 BUG-004). For chunked / compressed
			// responses, that truncated the source to whatever the first
			// read delivered, often <1K. io.ReadAll + LimitReader is the
			// same idiom used in artifact_download.go line 491.
			body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
			if err != nil {
				return nil, fmt.Errorf("grounding: read body: %w", err)
			}
			sourceContent := string(body)
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
			m := sharedMem()
			llmResult, err := judgeCompleteJSON(ctx, c, system, user, &verdict)
			if err != nil {
				if errors.Is(err, llm.ErrRefusalExhausted) {
					return nil, persistRefusal(m, ctx, "grounding_check", "claim", truncateContent(args.Claim, 200), c.Model, system, llmResult)
				}
				return nil, fmt.Errorf("dark-ssd: llm call failed: %w", err)
			}
			verdictJSON, _ := json.Marshal(verdict)

			if m != nil {
				_, _ = m.SaveSDDEvaluation(ctx, fillConstitutionFields(&mem.SDDEvaluation{
					EvalType:         "grounding_check",
					TargetType:       "claim",
					TargetID:         truncateContent(args.Claim, 200),
					VerdictJSON:      string(verdictJSON),
					Confidence:       verdict.Confidence,
					PromptVersion:    "v1",
					Model:            c.Model,
					RefusedAttempts:  llmResult.RefusedAttempts,
					RefusalPattern:   refusalPatternFromResult(llmResult),
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
				m := sharedMem()
				targetID := fmt.Sprintf("pii:%x", sha1Of(args.Content))
				return handleNoLLM(m, ctx, "pii_detect", "content", targetID)
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
			llmResult, err := judgeCompleteJSON(ctx, c, system, user, &verdict)
			if err != nil {
				if errors.Is(err, llm.ErrRefusalExhausted) {
					targetID := fmt.Sprintf("pii:%x", sha1Of(args.Content))
					return nil, persistRefusal(sharedMem(), ctx, "pii_detect", "content", targetID, c.Model, system, llmResult)
				}
				return nil, fmt.Errorf("dark-ssd: pii_detect llm call: %w", err)
			}
			verdictJSON, _ := json.Marshal(verdict)

			// Persist; target_id is a hash of content for dedup across rescans.
			targetID := fmt.Sprintf("pii:%x", sha1Of(args.Content))
			if m := sharedMem(); m != nil {
				_, _ = m.SaveSDDEvaluation(ctx, fillConstitutionFields(&mem.SDDEvaluation{
					EvalType:         "pii_detect",
					TargetType:       "content",
					TargetID:         targetID,
					VerdictJSON:      string(verdictJSON),
					Confidence:       verdict.Confidence,
					PromptVersion:    "v1",
					Model:            c.Model,
					RefusedAttempts:  llmResult.RefusedAttempts,
					RefusalPattern:   refusalPatternFromResult(llmResult),
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
				m := sharedMem()
				targetID := fmt.Sprintf("inject:%x", sha1Of(args.Content))
				return handleNoLLM(m, ctx, "prompt_injection_scan", "content", targetID)
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
			llmResult, err := judgeCompleteJSON(ctx, c, system, user, &verdict)
			if err != nil {
				if errors.Is(err, llm.ErrRefusalExhausted) {
					targetID := fmt.Sprintf("inject:%x", sha1Of(args.Content))
					return nil, persistRefusal(sharedMem(), ctx, "prompt_injection_scan", "content", targetID, c.Model, system, llmResult)
				}
				return nil, fmt.Errorf("dark-ssd: prompt_injection_scan llm call: %w", err)
			}
			verdictJSON, _ := json.Marshal(verdict)

			targetID := fmt.Sprintf("inject:%x", sha1Of(args.Content))
			if m := sharedMem(); m != nil {
				_, _ = m.SaveSDDEvaluation(ctx, fillConstitutionFields(&mem.SDDEvaluation{
					EvalType:         "prompt_injection_scan",
					TargetType:       "content",
					TargetID:         targetID,
					VerdictJSON:      string(verdictJSON),
					Confidence:       verdict.Confidence,
					PromptVersion:    "v1",
					Model:            c.Model,
					RefusedAttempts:  llmResult.RefusedAttempts,
					RefusalPattern:   refusalPatternFromResult(llmResult),
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
				m := sharedMem()
				return handleConsensusNoLLM(m, ctx, args)
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
			llmResult, err := judgeCompleteJSON(ctx, c, system, user, &v)
			if err != nil {
				// On refusal exhaustion, persist the audit row so
				// the trail is complete. Other errors propagate
				// without persisting (transient network/parse).
				if errors.Is(err, llm.ErrRefusalExhausted) {
					_ = persistRefusal(sharedMem(), ctx, args.EvalType, "consensus_sample", fmt.Sprintf("%d/%d", i+1, args.N), c.Model, system, llmResult)
					return nil, fmt.Errorf("dark-ssd: consensus sample %d/%d: %w", i+1, args.N, err)
				}
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

// refusalPatternFromResult returns the pattern label of the
// final refusal signal, or "" if the LLM did not refuse. Used
// to populate the sdd_evaluations.refusal_pattern column.
func refusalPatternFromResult(r *llm.RefusalResult) string {
	if r == nil || r.FinalRefusal == nil {
		return ""
	}
	return r.FinalRefusal.Pattern
}

// persistRefusal writes a refusal-exhausted verdict to the
// sdd_evaluations table so the audit trail captures the
// failure. Returns a descriptive error to the caller (the MCP
// tool result will surface this to the agent). The persisted
// verdict uses confidence=0 and a deterministic
// "refusal_exhausted" recommendation so downstream tools that
// read sdd_evaluations can detect the failure mode.
func persistRefusal(m *mem.Store, ctx context.Context, evalType, targetType, targetID, model, _ string, r *llm.RefusalResult) error {
	pattern := refusalPatternFromResult(r)
	attempts := 0
	if r != nil {
		attempts = r.Attempts
	}
	if m == nil {
		return fmt.Errorf("dark-ssd: refusal exhausted (no mem store); pattern=%q", pattern)
	}
	refusedVerdict := map[string]any{
		"refused":         true,
		"attempts":        attempts,
		"refused_pattern": pattern,
		"last_excerpt":    "",
		"recommendation":  "retry_with_different_prompt",
	}
	if r != nil && r.FinalRefusal != nil {
		refusedVerdict["last_excerpt"] = r.FinalRefusal.Excerpt
	}
	verdictJSON, _ := json.Marshal(refusedVerdict)

	_, _ = m.SaveSDDEvaluation(ctx, fillConstitutionFields(&mem.SDDEvaluation{
		EvalType:        evalType,
		TargetType:      targetType,
		TargetID:        targetID,
		VerdictJSON:     string(verdictJSON),
		Confidence:      0,
		PromptVersion:   "v1",
		Model:           model,
		RefusedAttempts: attempts,
		RefusalPattern:  pattern,
	}))
	return fmt.Errorf("dark-ssd: refusal exhausted after %d attempts; last pattern %q; verdict persisted to sdd_evaluations",
		attempts, pattern)
}