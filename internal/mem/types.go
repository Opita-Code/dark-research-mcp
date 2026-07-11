package mem

// ResearchRun is one query executed by the router. Saved along with
// its Items in a single transaction.
type ResearchRun struct {
	ID            int64          `json:"id"`
	SessionID     string         `json:"session_id,omitempty"`
	Query         string         `json:"query"`
	Intent        string         `json:"intent"`
	BackendUsed   string         `json:"backend_used,omitempty"`
	BackendsTried []string       `json:"backends_tried,omitempty"`
	TookMs        int64          `json:"took_ms"`
	ConfidenceAvg float32        `json:"confidence_avg"`
	Items         []Item         `json:"items,omitempty"`
	Errors        []BackendError `json:"errors,omitempty"`
	CreatedAt     string         `json:"created_at"`
}

// Item is one search result, persisted as part of a ResearchRun.
type Item struct {
	ID          int64   `json:"id"`
	RunID       int64   `json:"run_id"`
	Title       string  `json:"title"`
	URL         string  `json:"url,omitempty"`
	Snippet     string  `json:"snippet,omitempty"`
	Source      string  `json:"source"`
	Confidence  float32 `json:"confidence"`
	FreshnessAt string  `json:"freshness_at,omitempty"` // RFC 3339; empty if unknown
	Lang        string  `json:"lang,omitempty"`
	Raw         string  `json:"raw,omitempty"` // JSON blob
	CreatedAt   string  `json:"created_at"`
}

// BackendError mirrors research.BackendError but lives in this package
// to avoid an import cycle (research imports mem, not the other way).
type BackendError struct {
	Backend string `json:"backend"`
	Err     string `json:"err"`
}

// Status is the aggregate stats returned by Store.Status.
type Status struct {
	RunsTotal       int            `json:"runs_total"`
	ItemsTotal      int            `json:"items_total"`
	LinksTotal      int            `json:"links_total"`
	IntentHistogram map[string]int `json:"intent_histogram"`
	SourceHistogram map[string]int `json:"source_histogram"`
	OldestRun       string         `json:"oldest_run,omitempty"`
	NewestRun       string         `json:"newest_run,omitempty"`
}

// ---------------------------------------------------------------------------
// vibe-flow types. All JSON fields are passed through as opaque strings so
// the schema can evolve without DB migrations. The dark-research-mcp tools
// are the typed boundary; everything else reads/writes raw JSON.
//
// Each field has an explicit `json:"snake_case"` tag so MCP tool responses
// use a stable contract for the agent (independent of Go field names).
// ---------------------------------------------------------------------------

// Spec is a declarative intent: constitution (rules) + spec (what) +
// tasks (ordered work units). Persisted before generation; reconciled with
// artifacts via drift_reports. Maps to DB table vibe_specs.
type Spec struct {
	ID           int64  `json:"id"`
	VibeCase     string `json:"vibe_case"`
	SessionID    string `json:"session_id,omitempty"`
	Constitution string `json:"constitution,omitempty"`
	Spec         string `json:"spec,omitempty"`
	Tasks        string `json:"tasks,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// BrandGuide captures voice + visual + narrative + compliance for one
// brand. Single-row per brand_id (upsert). Maps to DB table vibe_brands.
type BrandGuide struct {
	BrandID    string `json:"brand_id"`
	Voice      string `json:"voice,omitempty"`
	Visual     string `json:"visual,omitempty"`
	Narrative  string `json:"narrative,omitempty"`
	Compliance string `json:"compliance,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

// ComplianceRule captures jurisdiction-specific rules (EU AI Act
// 2026-08-02 disclosure, FTC, Meta/Google ad labels, etc). Maps to DB
// table vibe_compliance.
type ComplianceRule struct {
	Jurisdiction string `json:"jurisdiction"`
	Rules        string `json:"rules"`
	EffectiveAt  string `json:"effective_at,omitempty"`
	SourceURL    string `json:"source_url,omitempty"`
	CreatedAt    string `json:"created_at"`
}

// Artifact is one generated output (code, text, image, video, audio,
// multi-modal). Linked optionally to a Spec via spec_id. Maps to DB
// table vibe_artifacts.
type Artifact struct {
	ID               int64  `json:"id"`
	SessionID        string `json:"session_id,omitempty"`
	VibeCase         string `json:"vibe_case"`
	SpecID           int64  `json:"spec_id,omitempty"`
	ArtifactURL      string `json:"artifact_url,omitempty"`
	ArtifactType     string `json:"artifact_type"`
	BrandID          string `json:"brand_id,omitempty"`
	Jurisdiction     string `json:"jurisdiction,omitempty"`
	HasDisclosure    bool   `json:"has_disclosure"`
	ValidationStatus string `json:"validation_status"`
	CreatedAt        string `json:"created_at"`
}

// DriftReport records a comparison between a Spec and a generated
// Artifact. Verdict aligned means the artifact matches the spec;
// drift_detected means the artifact diverged (reconcile needed);
// needs_human means the LLM-as-judge was not confident. Maps to DB
// table vibe_drift_reports.
type DriftReport struct {
	ID             int64  `json:"id"`
	ArtifactID     int64  `json:"artifact_id"`
	SpecID         int64  `json:"spec_id,omitempty"`
	Verdict        string `json:"verdict"`
	SpecDiff       string `json:"spec_diff,omitempty"`
	JudgeReasoning string `json:"judge_reasoning,omitempty"`
	ReconciledAt   string `json:"reconciled_at,omitempty"`
	CreatedAt      string `json:"created_at"`
}

// SDDEvaluation is one LLM-as-judge verdict. Stored in sdd_evaluations.
// Use this to audit the agent's reasoning ("why did we brand_match
// with 0.4?") and to feed a calibration loop later.
//
// v3 added five columns for constitution-aware audit: ConstitutionID,
// ConstitutionVersion, ActiveModsJSON, RefusedAttempts, RefusalPattern.
// Pre-v3 rows have these as zero values; post-v3 writes populate them
// from the constitution loader context. The JSON tags use omitempty so
// the existing tool contract is unchanged for callers that don't
// supply these.
type SDDEvaluation struct {
	ID                  int64   `json:"id"`
	EvalType            string  `json:"eval_type"`            // brand_match | compliance_check | drift_judge | grounding_check
	TargetType          string  `json:"target_type"`          // artifact | spec | claim
	TargetID            string  `json:"target_id"`            // string for flexibility
	VerdictJSON         string  `json:"verdict_json"`         // opaque JSON blob
	Confidence          float32 `json:"confidence"`
	PromptVersion       string  `json:"prompt_version,omitempty"`
	Model               string  `json:"model,omitempty"`
	ConstitutionID      string  `json:"constitution_id,omitempty"`
	ConstitutionVersion string  `json:"constitution_version,omitempty"`
	ActiveModsJSON      string  `json:"active_mods_json,omitempty"`
	RefusedAttempts     int     `json:"refused_attempts"`
	RefusalPattern      string  `json:"refusal_pattern,omitempty"`
	CreatedAt           string  `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Constitution system types (migration v2).
//
// A Constitution is a declarative manifest that defines the agent's
// identity, authority hierarchy, refusal policy, scope, tone, and the
// ordered layers of the system prompt. The constitution loader (Fase 1)
// parses TOML files and persists them here so the same constitution is
// applied consistently across restarts and across agents that share the
// dark.db file.
//
// A Mod is a drop-in package of knowledge (text/datasets) and/or
// capabilities (tools, parsers, backends). The mod loader (Fase 2)
// discovers mod.toml manifests under the user mod path and registers
// them here. The web-of-mods (Fase 7) will install mods from a remote
// registry; the `source` column already differentiates user-local from
// registry-sourced.
//
// A ModLoad is one audit row per (mod, session) load event. It lets
// the agent answer "which mods were active when this judge verdict
// was emitted?" — the chain-of-custody question that's also
// answerable for research_runs and vibe_specs.
// ---------------------------------------------------------------------------

// Constitution is one (constitution_id, version) manifest. The
// UNIQUE(constitution_id, version) constraint at the DB layer means the
// same constitution can have multiple versions over time, and
// sdd_evaluations.constitution_id + constitution_version together
// reproduce the exact manifest that was in effect.
type Constitution struct {
	ID             int64  `json:"id"`
	ConstitutionID string `json:"constitution_id"`           // e.g. "dark-research/light"
	Version        string `json:"version"`                  // semver, e.g. "1.0.0"
	Label          string `json:"label,omitempty"`          // human-readable
	Source         string `json:"source"`                   // builtin:light | builtin:dark | user:<path>
	FilePath       string `json:"file_path"`                // absolute path or "<builtin>"
	ParsedJSON     string `json:"parsed_json"`              // full TOML dump
	SHA256         string `json:"sha256"`                   // hash of the source file
	Enabled        bool   `json:"enabled"`                  // 0 = disabled, 1 = active
	CreatedAt      string `json:"created_at"`
	ActivatedAt    string `json:"activated_at,omitempty"`
}

// Mod is one installed mod manifest. ModID is the immutable
// "namespace/name" handle (e.g. "user/osint-cve-deepdive"); Version is
// semver. Source tracks provenance for the future registry.
// ManifestJSON is the parsed mod.toml so a downgrade of the loader
// can still read older manifests. SHA256 catches tampering of the
// source files between activations.
//
// RiskClass and TargetScope are surfaced in the future web-of-mods UI
// so users can filter / warn before installing.
type Mod struct {
	ID           int64  `json:"id"`
	ModID        string `json:"mod_id"`               // e.g. "user/osint-cve-deepdive"
	Name         string `json:"name"`
	Version      string `json:"version"`              // semver
	Source       string `json:"source"`               // user:<path> | registry:<url>
	ManifestJSON string `json:"manifest_json"`        // parsed mod.toml
	SHA256       string `json:"sha256"`
	RiskClass    string `json:"risk_class,omitempty"`          // research-only | active-probing | exploit-development
	TargetScope  string `json:"target_scope,omitempty"`        // public_internet | private_infrastructure | darkweb
	RequiresTor  bool   `json:"requires_tor"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// ModLoad is one load event: "mod X was loaded at time T under
// constitution Y, took D milliseconds, contributed C capabilities".
// Error is non-empty when the load failed (e.g. invalid manifest,
// missing file); the row is still written so the failure is auditable
// instead of silently dropped.
//
// This table is the join point between the constitution system and
// the existing audit trail (sdd_evaluations, vibe_artifacts,
// research_runs). Once sdd_evaluations references both
// constitution_id and active_mods_json, you can ask: "under what
// constitution + which mods was this judge verdict produced?" — and
// "what was loaded at the time this artifact was generated?".
type ModLoad struct {
	ID                int64  `json:"id"`
	ModID             string `json:"mod_id"`
	SessionID         string `json:"session_id,omitempty"`
	LoadedAt          string `json:"loaded_at"`
	DurationMs        int64  `json:"duration_ms"`
	CapabilitiesCount int    `json:"capabilities_count"`
	Error             string `json:"error,omitempty"`
	ConstitutionID    string `json:"constitution_id,omitempty"`
}