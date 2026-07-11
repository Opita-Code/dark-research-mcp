package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dark-agents/research-mcp/internal/mem"
	"github.com/dark-agents/research-mcp/internal/safety"
	"github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// vibe-flow MCP tools. CRUD wrappers around the mem package methods. The
// agent does the actual LLM-as-judge reasoning (brand match, compliance
// check, drift compare); these tools persist and retrieve state.
// ---------------------------------------------------------------------------

// --- specs ---

type specCreateArgs struct {
	VibeCase     string `json:"vibe_case" jsonschema:"Vibe-flow case: C1 (code), C2 (text), C3 (image), C4 (video), C5 (audio), C6 (multi-modal), C7 (mixed)"`
	SessionID    string `json:"session_id,omitempty" jsonschema:"Optional session id"`
	Constitution string `json:"constitution,omitempty" jsonschema:"JSON: hard rules, brand voice, legal constraints"`
	Spec         string `json:"spec,omitempty" jsonschema:"JSON: declarative intent (what + why)"`
	Tasks        string `json:"tasks,omitempty" jsonschema:"JSON array: [{id, description, depends_on}]"`
}

func specCreateTool() Tool {
	def := mcp.NewTool("dark_research_spec_create",
		mcp.WithDescription("Persist a vibe-flow spec (constitution + intent + tasks) before generation. Returns the spec_id for linking with artifacts and drift reports. The spec is the source of truth; if the generated artifact diverges, drift detection reconciles back to the spec."),
		mcp.WithString("case_kind", mcp.Required(), mcp.Description("C1..C7")),
		mcp.WithString("session_id", mcp.Description("Optional session id")),
		mcp.WithString("constitution", mcp.Description("JSON: hard rules, brand voice, legal constraints")),
		mcp.WithString("spec", mcp.Description("JSON: declarative intent (what + why)")),
		mcp.WithString("tasks", mcp.Description("JSON array of ordered work units")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args specCreateArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			id, err := m.SaveSpec(ctx, &mem.Spec{
				VibeCase:     args.VibeCase,
				SessionID:    args.SessionID,
				Constitution: args.Constitution,
				Spec:         args.Spec,
				Tasks:        args.Tasks,
			})
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"spec_id": id}), nil
		},
	}
}

type specGetArgs struct {
	SpecID int64 `json:"spec_id" jsonschema:"Spec id returned by dark_research_spec_create"`
}

func specGetTool() Tool {
	def := mcp.NewTool("dark_research_spec_get",
		mcp.WithDescription("Load a persisted spec by id. Use before drift detection to compare against the artifact."),
		mcp.WithNumber("spec_id", mcp.Required(), mcp.Description("Spec id")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args specGetArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			sp, err := m.GetSpec(ctx, args.SpecID)
			if err != nil {
				return nil, err
			}
			if sp == nil {
				return jsonResult(map[string]any{"found": false}), nil
			}
			return jsonResult(sp), nil
		},
	}
}

// --- brand guides ---

type brandRegisterArgs struct {
	BrandID    string `json:"brand_id" jsonschema:"Unique brand identifier (e.g. 'acme-2026')"`
	Voice      string `json:"voice,omitempty" jsonschema:"JSON: {tone, forbidden_words, preferred_words}"`
	Visual     string `json:"visual,omitempty" jsonschema:"JSON: {palette, logo_url, forbidden_fonts}"`
	Narrative  string `json:"narrative,omitempty" jsonschema:"JSON: {hero_archetype, story_structure}"`
	Compliance string `json:"compliance,omitempty" jsonschema:"JSON: {restricted_claims, required_disclaimers}"`
}

func brandRegisterTool() Tool {
	def := mcp.NewTool("dark_research_brand_register",
		mcp.WithDescription("Upsert a brand guide (voice + visual + narrative + compliance). One per brand_id. Use dark_research_brand_get to retrieve for brand-match validation. The agent does the matching — this tool only persists state."),
		mcp.WithString("brand_id", mcp.Required(), mcp.Description("Brand id")),
		mcp.WithString("voice", mcp.Description("JSON voice profile")),
		mcp.WithString("visual", mcp.Description("JSON visual profile")),
		mcp.WithString("narrative", mcp.Description("JSON narrative profile")),
		mcp.WithString("compliance", mcp.Description("JSON compliance rules per brand")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args brandRegisterArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			if err := m.SaveBrandGuide(ctx, &mem.BrandGuide{
				BrandID:    args.BrandID,
				Voice:      args.Voice,
				Visual:     args.Visual,
				Narrative:  args.Narrative,
				Compliance: args.Compliance,
			}); err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"brand_id": args.BrandID, "saved": true}), nil
		},
	}
}

type brandGetArgs struct {
	BrandID string `json:"brand_id" jsonschema:"Brand id to look up"`
}

func brandGetTool() Tool {
	def := mcp.NewTool("dark_research_brand_get",
		mcp.WithDescription("Load a brand guide by id. Returns the voice/visual/narrative/compliance profile for brand-match validation."),
		mcp.WithString("brand_id", mcp.Required(), mcp.Description("Brand id")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args brandGetArgs
			if err := bindArgs(req, &args); err != nil {
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
				return jsonResult(map[string]any{"found": false}), nil
			}
			return jsonResult(b), nil
		},
	}
}

// --- compliance rules ---

type complianceRegisterArgs struct {
	Jurisdiction string `json:"jurisdiction" jsonschema:"Jurisdiction code: 'EU', 'US-CA', 'UK', 'BR', etc"`
	Rules        string `json:"rules" jsonschema:"JSON: {disclosure_required_for:[...], watermark_required:bool, penalty_per_violation_usd:int}"`
	EffectiveAt  string `json:"effective_at,omitempty" jsonschema:"ISO date when rule takes effect (e.g. '2026-08-02')"`
	SourceURL    string `json:"source_url,omitempty" jsonschema:"URL of the source regulation"`
}

func complianceRegisterTool() Tool {
	def := mcp.NewTool("dark_research_compliance_register",
		mcp.WithDescription("Upsert compliance rules for a jurisdiction. Pre-loaded examples: EU AI Act (effective 2026-08-02, disclosure + watermark required, $51,744/violation), US-CA SB-1001, UK AISI, BR PL 21/2020. The agent does the compliance check — this tool only persists state."),
		mcp.WithString("jurisdiction", mcp.Required(), mcp.Description("Jurisdiction code")),
		mcp.WithString("rules", mcp.Required(), mcp.Description("JSON rules")),
		mcp.WithString("effective_at", mcp.Description("ISO date when rule takes effect")),
		mcp.WithString("source_url", mcp.Description("Source URL")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args complianceRegisterArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			if err := m.SaveComplianceRule(ctx, &mem.ComplianceRule{
				Jurisdiction: args.Jurisdiction,
				Rules:        args.Rules,
				EffectiveAt:  args.EffectiveAt,
				SourceURL:    args.SourceURL,
			}); err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"jurisdiction": args.Jurisdiction, "saved": true}), nil
		},
	}
}

type complianceGetArgs struct {
	Jurisdiction string `json:"jurisdiction" jsonschema:"Jurisdiction code to look up"`
}

func complianceGetTool() Tool {
	def := mcp.NewTool("dark_research_compliance_get",
		mcp.WithDescription("Load compliance rules for a jurisdiction. Use before any artifact that targets a regulated jurisdiction (EU AI Act effective 2026-08-02, US state laws, etc)."),
		mcp.WithString("jurisdiction", mcp.Required(), mcp.Description("Jurisdiction code")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args complianceGetArgs
			if err := bindArgs(req, &args); err != nil {
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
				return jsonResult(map[string]any{"found": false}), nil
			}
			return jsonResult(r), nil
		},
	}
}

// --- artifacts ---

type artifactLogArgs struct {
	VibeCase     string `json:"vibe_case" jsonschema:"Vibe-flow case: C1..C7"`
	ArtifactType string `json:"artifact_type" jsonschema:"code|text|image|video|audio|multi"`
	ArtifactURL  string `json:"artifact_url,omitempty" jsonschema:"URL where artifact is hosted (repo, doc, image, video, etc)"`
	SpecID       int64  `json:"spec_id,omitempty" jsonschema:"Optional spec id for provenance linkage"`
	BrandID      string `json:"brand_id,omitempty" jsonschema:"Brand id if applicable"`
	Jurisdiction string `json:"jurisdiction,omitempty" jsonschema:"Jurisdiction for compliance check"`
	HasDisclosure bool  `json:"has_disclosure,omitempty" jsonschema:"True if artifact has required AI-generation disclosure"`
	SessionID    string `json:"session_id,omitempty" jsonschema:"Session id"`
}

func artifactLogTool() Tool {
	def := mcp.NewTool("dark_research_artifact_log",
		mcp.WithDescription("Log a generated artifact (code, text, image, video, audio) for provenance + drift tracking. Returns artifact_id for linking with drift reports. Always log BEFORE publishing so the audit trail is complete."),
		mcp.WithString("case_kind", mcp.Required(), mcp.Description("C1..C7")),
		mcp.WithString("artifact_type", mcp.Required(), mcp.Description("code|text|image|video|audio|multi")),
		mcp.WithString("artifact_url", mcp.Description("URL where the artifact lives")),
		mcp.WithNumber("spec_id", mcp.Description("Optional spec id")),
		mcp.WithString("brand_id", mcp.Description("Optional brand id")),
		mcp.WithString("jurisdiction", mcp.Description("Optional jurisdiction code")),
		mcp.WithBoolean("has_disclosure", mcp.Description("True if disclosure is present")),
		mcp.WithString("session_id", mcp.Description("Optional session id")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args artifactLogArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			id, err := m.SaveArtifact(ctx, &mem.Artifact{
				VibeCase:      args.VibeCase,
				ArtifactType:  args.ArtifactType,
				ArtifactURL:   args.ArtifactURL,
				SpecID:        args.SpecID,
				BrandID:       args.BrandID,
				Jurisdiction:  args.Jurisdiction,
				HasDisclosure: args.HasDisclosure,
				SessionID:     args.SessionID,
			})
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"artifact_id": id}), nil
		},
	}
}

type artifactGetArgs struct {
	ArtifactID int64 `json:"artifact_id" jsonschema:"Artifact id from dark_research_artifact_log"`
}

func artifactGetTool() Tool {
	def := mcp.NewTool("dark_research_artifact_get",
		mcp.WithDescription("Load a logged artifact. Use before drift detection to compare against its spec."),
		mcp.WithNumber("artifact_id", mcp.Required(), mcp.Description("Artifact id")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args artifactGetArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			a, err := m.GetArtifact(ctx, args.ArtifactID)
			if err != nil {
				return nil, err
			}
			if a == nil {
				return jsonResult(map[string]any{"found": false}), nil
			}
			return jsonResult(a), nil
		},
	}
}

// artifactUpdateArgs uses *bool / *int64 sentinels via the MCP layer's
// JSON pointer convention: only present keys are updated.
type artifactUpdateArgs struct {
	ArtifactID      int64   `json:"artifact_id" jsonschema:"Artifact id from dark_research_artifact_log"`
	SessionID       *string `json:"session_id,omitempty" jsonschema:"New session id. Omit to leave unchanged."`
	SpecID          *int64  `json:"spec_id,omitempty" jsonschema:"New spec id (or 0 to unlink). Omit to leave unchanged."`
	ArtifactURL     *string `json:"artifact_url,omitempty" jsonschema:"New artifact URL. Omit to leave unchanged."`
	BrandID         *string `json:"brand_id,omitempty" jsonschema:"New brand id. Omit to leave unchanged."`
	Jurisdiction    *string `json:"jurisdiction,omitempty" jsonschema:"New jurisdiction (e.g. 'EU'). Omit to leave unchanged."`
	HasDisclosure   *bool   `json:"has_disclosure,omitempty" jsonschema:"New has_disclosure value. CRITICAL for EU AI Act compliance: explicitly set true after publishing a C4 video."`
	ValidationStatus *string `json:"validation_status,omitempty" jsonschema:"New status (pending|passed|failed|drift_detected). Omit to leave unchanged."`
}

func artifactUpdateTool() Tool {
	def := mcp.NewTool("dark_research_artifact_update",
		mcp.WithDescription("Partial update of an artifact by id. Only fields you pass are updated; missing fields are left unchanged. Use to fix a typo'd URL, attach a missing jurisdiction, flip has_disclosure after publication, or update validation_status after a manual review."),
		mcp.WithNumber("artifact_id", mcp.Required(), mcp.Description("Artifact id")),
		mcp.WithString("session_id", mcp.Description("New session id")),
		mcp.WithNumber("spec_id", mcp.Description("New spec id (0 = unlink)")),
		mcp.WithString("artifact_url", mcp.Description("New artifact URL")),
		mcp.WithString("brand_id", mcp.Description("New brand id")),
		mcp.WithString("jurisdiction", mcp.Description("New jurisdiction")),
		mcp.WithBoolean("has_disclosure", mcp.Description("New has_disclosure value")),
		mcp.WithString("validation_status", mcp.Description("New validation status")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args artifactUpdateArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			err := m.UpdateArtifact(ctx, args.ArtifactID, &mem.ArtifactUpdate{
				SessionID:        args.SessionID,
				SpecID:           args.SpecID,
				ArtifactURL:      args.ArtifactURL,
				BrandID:          args.BrandID,
				Jurisdiction:     args.Jurisdiction,
				HasDisclosure:    args.HasDisclosure,
				ValidationStatus: args.ValidationStatus,
			})
			if err == sql.ErrNoRows {
				return jsonResult(map[string]any{"updated": false, "reason": "artifact not found"}), nil
			}
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"artifact_id": args.ArtifactID, "updated": true}), nil
		},
	}
}

type artifactDeleteArgs struct {
	ArtifactID int64 `json:"artifact_id" jsonschema:"Artifact id to delete"`
}

func artifactDeleteTool() Tool {
	def := mcp.NewTool("dark_research_artifact_delete",
		mcp.WithDescription("Permanently delete an artifact by id. Drift reports referencing this artifact are removed via ON DELETE CASCADE. Use to clean up failed experiments or replace an artifact."),
		mcp.WithNumber("artifact_id", mcp.Required(), mcp.Description("Artifact id to delete")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args artifactDeleteArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			err := m.DeleteArtifact(ctx, args.ArtifactID)
			if err == sql.ErrNoRows {
				return jsonResult(map[string]any{"deleted": false, "reason": "artifact not found"}), nil
			}
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"artifact_id": args.ArtifactID, "deleted": true}), nil
		},
	}
}

type artifactDownloadArgs struct {
	ArtifactID int64 `json:"artifact_id" jsonschema:"Artifact id from dark_research_artifact_log"`
	MaxLength  int   `json:"max_length,omitempty" jsonschema:"Max characters of returned content (default 20000; capped at Safety.MaxOutputChars)"`
}

func artifactDownloadTool(c *clients) Tool {
	def := mcp.NewTool("dark_research_artifact_download",
		mcp.WithDescription("Fetch the content of an artifact's URL for dark_ssd_drift_judge or dark_ssd_grounding_check. Looks up the artifact in dark.db, SSRF-guards the URL (no private IPs unless safety.allow_loopback is true), downloads via the shared clearnet client with a byte cap, and returns the text content. Use BEFORE drift_judge so the judge can compare actual content vs spec without you copy-pasting."),
		mcp.WithNumber("artifact_id", mcp.Required(), mcp.Description("Artifact id")),
		mcp.WithNumber("max_length", mcp.Description("Max characters of returned content (default 20000; capped at Safety.MaxOutputChars)")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args artifactDownloadArgs
			if err := bindArgs(req, &args); err != nil {
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
				return jsonResult(map[string]any{"found": false, "artifact_id": args.ArtifactID}), nil
			}
			if art.ArtifactURL == "" {
				return jsonResult(map[string]any{
					"artifact_id": args.ArtifactID,
					"downloaded":  false,
					"reason":      "artifact has no artifact_url (was logged without a URL)",
				}), nil
			}
			if _, err := safety.ValidateURL(art.ArtifactURL, c.Cfg.Safety.AllowLoopback); err != nil {
				return jsonResult(map[string]any{
					"artifact_id": args.ArtifactID,
					"url":         art.ArtifactURL,
					"downloaded":  false,
					"reason":      "ssrf guard rejected URL: " + err.Error(),
				}), nil
			}
			maxLen := args.MaxLength
			if maxLen <= 0 {
				maxLen = 20000
			}
			if maxLen > c.Cfg.Safety.MaxOutputChars {
				maxLen = c.Cfg.Safety.MaxOutputChars
			}

			httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, art.ArtifactURL, nil)
			if err != nil {
				return nil, err
			}
			httpReq.Header.Set("User-Agent", "dark-research-mcp/0.3 (+https://github.com/dark-agents/research-mcp) artifact-download")
			httpReq.Header.Set("Accept", "text/plain,text/html,text/markdown,application/json;q=0.5")

			resp, err := c.clearnet.DoContext(ctx, httpReq)
			if err != nil {
				return nil, fmt.Errorf("fetch %s: %w", art.ArtifactURL, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return nil, fmt.Errorf("fetch %s returned %d", art.ArtifactURL, resp.StatusCode)
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, int64(c.Cfg.Safety.MaxResponseBytes)))
			if err != nil {
				return nil, fmt.Errorf("read body: %w", err)
			}
			content := string(body)
			truncated := false
			if len(content) > maxLen {
				content = content[:maxLen]
				truncated = true
			}
			out := map[string]any{
				"artifact_id":   args.ArtifactID,
				"url":           art.ArtifactURL,
				"downloaded":    true,
				"bytes":         len(body),
				"fetched_at":    time.Now().UTC(),
				"http_status":   resp.StatusCode,
				"content":       content,
				"content_type":  resp.Header.Get("Content-Type"),
				"truncated":     truncated,
				"spec_id":       art.SpecID,
				"vibe_case":     art.VibeCase,
				"brand_id":      art.BrandID,
				"jurisdiction":  art.Jurisdiction,
			}
			return jsonResult(out), nil
		},
	}
}

type brandDeleteArgs struct {
	BrandID string `json:"brand_id" jsonschema:"Brand id to remove (e.g. 'acme-2026')"`
}

func brandDeleteTool() Tool {
	def := mcp.NewTool("dark_research_brand_delete",
		mcp.WithDescription("Remove a brand guide by id. Idempotent: deleting an absent brand returns success. Use to clean up old test brands or reset a corrupted registration."),
		mcp.WithString("brand_id", mcp.Required(), mcp.Description("Brand id to remove")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args brandDeleteArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			if err := m.DeleteBrandGuide(ctx, args.BrandID); err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"brand_id": args.BrandID, "deleted": true}), nil
		},
	}
}

// --- drift reports ---

type driftLogArgs struct {
	ArtifactID      int64  `json:"artifact_id" jsonschema:"Artifact id from dark_research_artifact_log"`
	SpecID          int64  `json:"spec_id,omitempty" jsonschema:"Spec id from dark_research_spec_create"`
	Verdict         string `json:"verdict" jsonschema:"aligned | drift_detected | needs_human"`
	SpecDiff        string `json:"spec_diff,omitempty" jsonschema:"JSON structured diff: {changed:[...], why:'...'}"`
	JudgeReasoning  string `json:"judge_reasoning,omitempty" jsonschema:"Free-form LLM-as-judge reasoning"`
	ReconciledAt    string `json:"reconciled_at,omitempty" jsonschema:"ISO timestamp if drift was accepted; auto-marks artifact validation_status='passed'"`
}

func driftLogTool() Tool {
	def := mcp.NewTool("dark_research_drift_log",
		mcp.WithDescription("Record a drift report comparing a spec against its artifact. The LLM-as-judge reasoning lives here. If reconciled_at is set, the artifact's validation_status auto-updates to 'passed'. Use this after every generation to close the spec-drift loop — the #1 unsolved problem in 2026 AI-assisted development."),
		mcp.WithNumber("artifact_id", mcp.Required(), mcp.Description("Artifact id")),
		mcp.WithNumber("spec_id", mcp.Description("Spec id")),
		mcp.WithString("verdict", mcp.Required(), mcp.Description("aligned | drift_detected | needs_human")),
		mcp.WithString("spec_diff", mcp.Description("JSON structured diff")),
		mcp.WithString("judge_reasoning", mcp.Description("LLM-as-judge reasoning")),
		mcp.WithString("reconciled_at", mcp.Description("Set when drift was accepted (auto-marks artifact as passed)")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args driftLogArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			id, err := m.SaveDriftReport(ctx, &mem.DriftReport{
				ArtifactID:     args.ArtifactID,
				SpecID:         args.SpecID,
				Verdict:        args.Verdict,
				SpecDiff:       args.SpecDiff,
				JudgeReasoning: args.JudgeReasoning,
				ReconciledAt:   args.ReconciledAt,
			})
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"drift_id": id}), nil
		},
	}
}

type driftGetArgs struct {
	ArtifactID int64 `json:"artifact_id" jsonschema:"Artifact id to fetch latest drift for"`
}

func driftGetTool() Tool {
	def := mcp.NewTool("dark_research_drift_get",
		mcp.WithDescription("Fetch the most recent drift report for an artifact. Returns nil drift_id if none exists yet."),
		mcp.WithNumber("artifact_id", mcp.Required(), mcp.Description("Artifact id")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args driftGetArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			d, err := m.LatestDriftForArtifact(ctx, args.ArtifactID)
			if err != nil {
				return nil, err
			}
			if d == nil {
				return jsonResult(map[string]any{"found": false}), nil
			}
			return jsonResult(d), nil
		},
	}
}

// ensure encoding/json is referenced (used by other files in this package).
var _ = json.Marshal

// --- list endpoints (newest-first, optional filters) ---

type specUpdateArgs struct {
	SpecID       int64  `json:"spec_id" jsonschema:"Spec id returned by dark_research_spec_create"`
	VibeCase     string `json:"vibe_case,omitempty" jsonschema:"New case (C1..C7). Empty = unchanged."`
	SessionID    string `json:"session_id,omitempty" jsonschema:"New session id. Empty = unchanged."`
	Constitution string `json:"constitution,omitempty" jsonschema:"New constitution JSON. Empty = unchanged."`
	Spec         string `json:"spec,omitempty" jsonschema:"New spec JSON. Empty = unchanged."`
	Tasks        string `json:"tasks,omitempty" jsonschema:"New tasks JSON. Empty = unchanged."`
}

func specUpdateTool() Tool {
	def := mcp.NewTool("dark_research_spec_update",
		mcp.WithDescription("Partial update of a spec by id. Any field left empty is left unchanged. updated_at is bumped automatically. Use to fix typos, refine the constitution, or extend the tasks list without re-creating the spec (which would invalidate artifact linkage)."),
		mcp.WithNumber("spec_id", mcp.Required(), mcp.Description("Spec id")),
		mcp.WithString("vibe_case", mcp.Description("New case (C1..C7)")),
		mcp.WithString("session_id", mcp.Description("New session id")),
		mcp.WithString("constitution", mcp.Description("New constitution JSON")),
		mcp.WithString("spec", mcp.Description("New spec JSON")),
		mcp.WithString("tasks", mcp.Description("New tasks JSON")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args specUpdateArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			err := m.UpdateSpec(ctx, args.SpecID, &mem.Spec{
				VibeCase:     args.VibeCase,
				SessionID:    args.SessionID,
				Constitution: args.Constitution,
				Spec:         args.Spec,
				Tasks:        args.Tasks,
			})
			if err == sql.ErrNoRows {
				return jsonResult(map[string]any{"updated": false, "reason": "spec not found"}), nil
			}
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"spec_id": args.SpecID, "updated": true}), nil
		},
	}
}

type specDeleteArgs struct {
	SpecID int64 `json:"spec_id" jsonschema:"Spec id to delete"`
}

func specDeleteTool() Tool {
	def := mcp.NewTool("dark_research_spec_delete",
		mcp.WithDescription("Permanently delete a spec by id. Drift reports referencing this spec have their spec_id set to NULL (the spec_diff stays). Artifacts referencing this spec are NOT deleted (their spec_id becomes stale; use artifact_update to re-link or artifact_delete to remove)."),
		mcp.WithNumber("spec_id", mcp.Required(), mcp.Description("Spec id to delete")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args specDeleteArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			err := m.DeleteSpec(ctx, args.SpecID)
			if err == sql.ErrNoRows {
				return jsonResult(map[string]any{"deleted": false, "reason": "spec not found"}), nil
			}
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"spec_id": args.SpecID, "deleted": true}), nil
		},
	}
}

type specRenderArgs struct {
	SpecID int64 `json:"spec_id" jsonschema:"Spec id returned by dark_research_spec_create"`
}

func specRenderTool() Tool {
	def := mcp.NewTool("dark_research_spec_render",
		mcp.WithDescription("Render a spec as a human-readable markdown document. Useful for handoff to humans, code review, archival, and debugging the drift loop. Returns the markdown body (not the raw JSON)."),
		mcp.WithNumber("spec_id", mcp.Required(), mcp.Description("Spec id")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args specRenderArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			sp, err := m.GetSpec(ctx, args.SpecID)
			if err != nil {
				return nil, err
			}
			if sp == nil {
				return jsonResult(map[string]any{"found": false}), nil
			}
			md := renderSpecMarkdown(sp)
			return jsonResult(map[string]any{
				"spec_id":  sp.ID,
				"vibe_case": sp.VibeCase,
				"markdown": md,
			}), nil
		},
	}
}

type specListArgs struct {
	VibeCase  string `json:"vibe_case,omitempty" jsonschema:"Optional: only specs for this case (C1..C7)"`
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional: only specs in this session"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Max specs to return (default 50)"`
}

func specListTool() Tool {
	def := mcp.NewTool("dark_research_spec_list",
		mcp.WithDescription("List specs newest-first with optional filters by vibe_case and session_id. Use to audit prior specs (e.g. 'show me every C4 spec from session abc')."),
		mcp.WithString("vibe_case", mcp.Description("Optional vibe_case filter (C1..C7)")),
		mcp.WithString("session_id", mcp.Description("Optional session id filter")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50)")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args specListArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			out, err := m.ListSpecs(ctx, args.VibeCase, args.SessionID, args.Limit)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{
				"count": len(out),
				"specs": out,
			}), nil
		},
	}
}

type brandListArgs struct {
	Limit int `json:"limit,omitempty" jsonschema:"Max brand guides to return (default 100)"`
}

func brandListTool() Tool {
	def := mcp.NewTool("dark_research_brand_list",
		mcp.WithDescription("List every registered brand guide (voice + visual + narrative + compliance). Used to audit which brands exist before registering a new one."),
		mcp.WithNumber("limit", mcp.Description("Max results (default 100)")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args brandListArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			out, err := m.ListBrandGuides(ctx, args.Limit)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{
				"count":          len(out),
				"brand_guides":   out,
			}), nil
		},
	}
}

type complianceListArgs struct {
	Limit int `json:"limit,omitempty" jsonschema:"Max rules to return (default 50)"`
}

func complianceListTool() Tool {
	def := mcp.NewTool("dark_research_compliance_list",
		mcp.WithDescription("List every registered compliance rule (EU AI Act, US-CA SB-1001, UK AISI, BR PL 21/2020, etc). Used to know which jurisdictions we have rules for before validating an artifact."),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50)")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args complianceListArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			out, err := m.ListComplianceRules(ctx, args.Limit)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{
				"count":           len(out),
				"compliance_rules": out,
			}), nil
		},
	}
}

type artifactListArgs struct {
	VibeCase     string `json:"vibe_case,omitempty" jsonschema:"Optional: filter by vibe_case (C1..C7)"`
	BrandID      string `json:"brand_id,omitempty" jsonschema:"Optional: filter by brand id"`
	Jurisdiction string `json:"jurisdiction,omitempty" jsonschema:"Optional: filter by jurisdiction (EU, US-CA, etc)"`
	SessionID    string `json:"session_id,omitempty" jsonschema:"Optional: filter by session id"`
	Status       string `json:"status,omitempty" jsonschema:"Optional: filter by validation_status (pending|passed|failed|drift_detected)"`
	Limit        int    `json:"limit,omitempty" jsonschema:"Max artifacts to return (default 50)"`
}

func artifactListTool() Tool {
	def := mcp.NewTool("dark_research_artifact_list",
		mcp.WithDescription("List artifacts newest-first with optional filters. Common patterns: 'every pending C2 image', 'every passed C4 video for EU jurisdiction', 'every artifact for brand acme-2026'."),
		mcp.WithString("vibe_case", mcp.Description("Optional vibe_case filter")),
		mcp.WithString("brand_id", mcp.Description("Optional brand id filter")),
		mcp.WithString("jurisdiction", mcp.Description("Optional jurisdiction filter")),
		mcp.WithString("session_id", mcp.Description("Optional session id filter")),
		mcp.WithString("status", mcp.Description("Optional validation_status filter")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50)")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args artifactListArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			out, err := m.ListArtifacts(ctx, args.VibeCase, args.BrandID, args.Jurisdiction, args.SessionID, args.Status, args.Limit)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{
				"count":     len(out),
				"artifacts": out,
			}), nil
		},
	}
}

type driftListArgs struct {
	ArtifactID int64  `json:"artifact_id,omitempty" jsonschema:"Optional: only reports for this artifact"`
	Verdict    string `json:"verdict,omitempty" jsonschema:"Optional: filter by verdict (aligned|drift_detected|needs_human)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Max reports (default 50)"`
}

func driftListTool() Tool {
	def := mcp.NewTool("dark_research_drift_list",
		mcp.WithDescription("List drift reports newest-first with optional filters. Use to audit every drift detection (e.g. 'every drift_detected verdict this session') or to walk back through the spec-drift loop."),
		mcp.WithNumber("artifact_id", mcp.Description("Optional artifact id filter")),
		mcp.WithString("verdict", mcp.Description("Optional verdict filter")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50)")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args driftListArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			out, err := m.ListDriftReports(ctx, args.ArtifactID, args.Verdict, args.Limit)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{
				"count":          len(out),
				"drift_reports":  out,
			}), nil
		},
	}
}

// ---------------------------------------------------------------------------
// renderSpecMarkdown converts a Spec struct into a human-readable markdown
// document. JSON fields are pretty-printed; tasks render as a checklist.
// Used by dark_research_spec_render.
// ---------------------------------------------------------------------------

func renderSpecMarkdown(sp *mem.Spec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Spec #%d — `%s`\n\n", sp.ID, sp.VibeCase)
	if sp.SessionID != "" {
		fmt.Fprintf(&b, "**Session:** `%s`  \n", sp.SessionID)
	}
	fmt.Fprintf(&b, "**Created:** %s  \n", sp.CreatedAt)
	if sp.UpdatedAt != "" {
		fmt.Fprintf(&b, "**Updated:** %s  \n", sp.UpdatedAt)
	}
	b.WriteString("\n")

	if sp.Constitution != "" {
		b.WriteString("## Constitution (hard rules)\n\n")
		b.WriteString(prettyJSON(sp.Constitution))
		b.WriteString("\n\n")
	}
	if sp.Spec != "" {
		b.WriteString("## Intent (what + why)\n\n")
		b.WriteString(prettyJSON(sp.Spec))
		b.WriteString("\n\n")
	}
	if sp.Tasks != "" {
		b.WriteString("## Tasks\n\n")
		// Try to render as checklist if the JSON is an array of
		// {id, description, depends_on} objects.
		var tasks []map[string]any
		if err := json.Unmarshal([]byte(sp.Tasks), &tasks); err == nil && len(tasks) > 0 {
			for _, t := range tasks {
				id, _ := t["id"].(float64)
				desc, _ := t["description"].(string)
				deps, _ := t["depends_on"].([]any)
				fmt.Fprintf(&b, "- [ ] **#%.0f** %s", id, desc)
				if len(deps) > 0 {
					fmt.Fprintf(&b, "  _depends on: %v_", deps)
				}
				b.WriteString("\n")
			}
		} else {
			// Fallback: pretty-print raw JSON
			b.WriteString("```json\n")
			b.WriteString(prettyJSON(sp.Tasks))
			b.WriteString("\n```\n")
		}
	}

	b.WriteString("\n---\n_Generated by `dark_research_spec_render` from `dark-research-mcp`._\n")
	return b.String()
}

// prettyJSON reformats a JSON string with 2-space indent. If the input
// isn't valid JSON, returns it verbatim.
func prettyJSON(s string) string {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s
	}
	return string(out)
}