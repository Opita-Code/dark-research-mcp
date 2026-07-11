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
type SDDEvaluation struct {
	ID            int64  `json:"id"`
	EvalType      string `json:"eval_type"`            // brand_match | compliance_check | drift_judge | grounding_check
	TargetType    string `json:"target_type"`          // artifact | spec | claim
	TargetID      string `json:"target_id"`            // string for flexibility
	VerdictJSON   string `json:"verdict_json"`         // opaque JSON blob
	Confidence    float32 `json:"confidence"`
	PromptVersion string `json:"prompt_version,omitempty"`
	Model         string `json:"model,omitempty"`
	CreatedAt     string `json:"created_at"`
}