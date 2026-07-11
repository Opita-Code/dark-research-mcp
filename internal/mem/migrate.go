package mem

import (
	"context"
	"database/sql"
	"fmt"
)

// ---------------------------------------------------------------------------
// Versioned schema migrations.
//
// Each migration has a monotonically increasing Version, a human-readable
// Name, the SQL Up script (idempotent — every statement uses IF NOT
// EXISTS / IF EXISTS), and an optional Down for rollback. The Store
// records every applied migration in schema_migrations so they don't re-run.
//
// To add a new migration:
//  1. Append a Migration{Version, Name, Up, Down} to AllMigrations.
//  2. Open() runs migrations on every startup; pending ones apply
//     atomically inside a transaction.
//  3. Verify with `go test ./internal/mem/...` (TestMigrations_applies).
//
// Migration history:
//   v1 — initial schema (research_runs/items/links, vibe_*, sdd_evaluations)
//   v2 — constitution system: constitutions, mods, mod_loads tables
//   v3 — sdd_evaluations extended with constitution_id, constitution_version,
//        active_mods_json, refused_attempts, refusal_pattern
//
// v1 is identical to the IF NOT EXISTS bootstrap. Future v2+ entries
// should ONLY contain the delta from v1, not the full schema.
// ---------------------------------------------------------------------------

// Migration is one versioned schema change.
type Migration struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
	Up      string `json:"-"` // SQL: idempotent CREATE/ALTER statements
	Down    string `json:"-"` // optional rollback SQL (best effort)
}

// AllMigrations is the registry. Append new migrations here; never edit
// a past one. Open() applies them in Version order.
var AllMigrations = []Migration{
	{
		Version: 1,
		Name:    "initial_schema",
		// Same as the original IF NOT EXISTS bootstrap. Future v2+
		// migrations should ONLY contain their delta; the bootstrap
		// is recorded as applied once.
		Up: schemaV1,
		Down: `
			DROP TABLE IF EXISTS sdd_evaluations;
			DROP TABLE IF EXISTS vibe_drift_reports;
			DROP TABLE IF EXISTS vibe_artifacts;
			DROP TABLE IF EXISTS vibe_compliance;
			DROP TABLE IF EXISTS vibe_brands;
			DROP TABLE IF EXISTS vibe_specs;
			DROP TABLE IF EXISTS research_links;
			DROP TABLE IF EXISTS research_items;
			DROP TABLE IF EXISTS research_runs;
		`,
	},
	{
		Version: 2,
		Name:    "constitutions_and_mods",
		// Adds the constitution system: registered constitutions
		// (declarative agent-posture manifests) and mods (drop-in
		// knowledge + capability packages). mod_loads is the audit
		// trail of which mods were active under which constitution
		// during a given session. No backfill needed: the tables
		// are empty by default and populated at runtime by the
		// constitution/mods loaders (Fase 1/2).
		Up: schemaV2,
		Down: `
			DROP TABLE IF EXISTS mod_loads;
			DROP TABLE IF EXISTS mods;
			DROP TABLE IF EXISTS constitutions;
		`,
	},
	{
		Version: 3,
		Name:    "sdd_evaluations_constitution_audit",
		// Extends sdd_evaluations with anti-refusal / constitution
		// audit fields. The v1 columns are untouched; new columns
		// are nullable or have DEFAULT so existing rows remain
		// valid. refused_attempts has DEFAULT 0 because it's the
		// most-queried column for refusal-rate analytics.
		Up: schemaV3,
		Down: `
			ALTER TABLE sdd_evaluations DROP COLUMN refusal_pattern;
			ALTER TABLE sdd_evaluations DROP COLUMN refused_attempts;
			ALTER TABLE sdd_evaluations DROP COLUMN active_mods_json;
			ALTER TABLE sdd_evaluations DROP COLUMN constitution_version;
			ALTER TABLE sdd_evaluations DROP COLUMN constitution_id;
		`,
	},
}

// schemaV1 is the canonical initial schema. Kept as a separate constant
// (not the old `schema` literal) so future migrations can reference it
// without confusion.
const schemaV1 = `
CREATE TABLE IF NOT EXISTS research_runs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id      TEXT,
    query           TEXT NOT NULL,
    intent          TEXT NOT NULL,
    backend_used    TEXT,
    backends_tried  TEXT,
    took_ms         INTEGER NOT NULL DEFAULT 0,
    confidence_avg  REAL NOT NULL DEFAULT 0,
    items_count     INTEGER NOT NULL DEFAULT 0,
    errors          TEXT,
    created_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_research_runs_intent  ON research_runs(intent);
CREATE INDEX IF NOT EXISTS idx_research_runs_session ON research_runs(session_id);
CREATE INDEX IF NOT EXISTS idx_research_runs_created ON research_runs(created_at);

CREATE TABLE IF NOT EXISTS research_items (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id       INTEGER NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
    title        TEXT NOT NULL,
    url          TEXT,
    snippet      TEXT,
    source       TEXT NOT NULL,
    confidence   REAL NOT NULL DEFAULT 0,
    freshness_at TEXT,
    lang         TEXT,
    raw          TEXT,
    created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_research_items_run    ON research_items(run_id);
CREATE INDEX IF NOT EXISTS idx_research_items_source ON research_items(source);

CREATE TABLE IF NOT EXISTS research_links (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    research_item_id INTEGER NOT NULL REFERENCES research_items(id) ON DELETE CASCADE,
    target_type      TEXT NOT NULL,
    target_id        TEXT NOT NULL,
    note             TEXT,
    created_at       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_research_links_target ON research_links(target_type, target_id);

CREATE TABLE IF NOT EXISTS vibe_specs (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    vibe_case         TEXT NOT NULL,
    session_id        TEXT,
    constitution_json TEXT,
    spec_json         TEXT,
    tasks_json        TEXT,
    created_at        TEXT NOT NULL,
    updated_at        TEXT
);
CREATE INDEX IF NOT EXISTS idx_vibe_specs_case    ON vibe_specs(vibe_case);
CREATE INDEX IF NOT EXISTS idx_vibe_specs_session ON vibe_specs(session_id);

CREATE TABLE IF NOT EXISTS vibe_brands (
    brand_id        TEXT PRIMARY KEY,
    voice_json      TEXT,
    visual_json     TEXT,
    narrative_json  TEXT,
    compliance_json TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT
);

CREATE TABLE IF NOT EXISTS vibe_compliance (
    jurisdiction   TEXT PRIMARY KEY,
    rules_json     TEXT NOT NULL,
    effective_at   TEXT,
    source_url     TEXT,
    created_at     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS vibe_artifacts (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id        TEXT,
    vibe_case         TEXT NOT NULL,
    spec_id           INTEGER REFERENCES vibe_specs(id) ON DELETE SET NULL,
    artifact_url      TEXT,
    artifact_type     TEXT NOT NULL,
    brand_id          TEXT,
    jurisdiction     TEXT,
    has_disclosure    INTEGER NOT NULL DEFAULT 0,
    validation_status TEXT NOT NULL DEFAULT 'pending',
    created_at        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_vibe_artifacts_case    ON vibe_artifacts(vibe_case);
CREATE INDEX IF NOT EXISTS idx_vibe_artifacts_brand   ON vibe_artifacts(brand_id);
CREATE INDEX IF NOT EXISTS idx_vibe_artifacts_session ON vibe_artifacts(session_id);
CREATE INDEX IF NOT EXISTS idx_vibe_artifacts_status  ON vibe_artifacts(validation_status);

CREATE TABLE IF NOT EXISTS vibe_drift_reports (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    artifact_id      INTEGER NOT NULL REFERENCES vibe_artifacts(id) ON DELETE CASCADE,
    spec_id          INTEGER REFERENCES vibe_specs(id) ON DELETE SET NULL,
    verdict          TEXT NOT NULL,
    spec_diff_json   TEXT,
    judge_reasoning  TEXT,
    reconciled_at    TEXT,
    created_at       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_vibe_drift_artifact ON vibe_drift_reports(artifact_id);
CREATE INDEX IF NOT EXISTS idx_vibe_drift_spec     ON vibe_drift_reports(spec_id);

CREATE TABLE IF NOT EXISTS sdd_evaluations (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    eval_type        TEXT NOT NULL,
    target_type      TEXT NOT NULL,
    target_id        TEXT NOT NULL,
    verdict_json     TEXT NOT NULL,
    confidence       REAL NOT NULL DEFAULT 0,
    prompt_version   TEXT,
    model            TEXT,
    created_at       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sdd_eval_type     ON sdd_evaluations(eval_type);
CREATE INDEX IF NOT EXISTS idx_sdd_eval_target   ON sdd_evaluations(target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_sdd_eval_created  ON sdd_evaluations(created_at);
`

// schemaV2 introduces the constitution system (v1) and the mod registry
// (v2 in the user-facing sense, not the migration version). These three
// tables are the persistence layer for the constitution loader and the
// mod loader (see internal/constitution and internal/mods, added in
// Fase 1 and Fase 2 respectively). Every column is NULL-tolerant so a
// partially-populated row is still a valid row (we learn the schema as
// mods declare richer manifests).
//
//   constitutions   one row per (constitution_id, version). source
//                   distinguishes "builtin:light", "builtin:dark" (only
//                   when compiled with -tags allow_builtin_dark), and
//                   "user:/path/to/file.toml" (a custom user file).
//                   parsed_json holds the full TOML dump so a downgrade
//                   of the loader can still read older manifests.
//   mods            one row per installed mod manifest. id is the
//                   immutable "namespace/name" handle; version is semver.
//                   source tells us where it came from (local path vs
//                   future registry). manifest_json is the parsed
//                   mod.toml; sha256 catches tampered files. risk_class
//                   and target_scope are surfaced to the user in the
//                   future web-of-mods UI.
//   mod_loads       audit trail. One row per (mod, session) load event.
//                   Lets the agent answer "which mods were active when
//                   this artifact was generated?" — the same provenance
//                   question that already exists for vibe_specs and
//                   research_runs. constitution_id is the constitution
//                   under which the mod was loaded, so we can correlate
//                   refusals (v3) with the constitution in effect.
const schemaV2 = `
CREATE TABLE IF NOT EXISTS constitutions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    constitution_id TEXT NOT NULL,                 -- e.g. "dark-research/light"
    version         TEXT NOT NULL,                 -- semver, e.g. "1.0.0"
    label           TEXT,                          -- human-readable
    source          TEXT NOT NULL,                 -- builtin:light | builtin:dark | user:<path>
    file_path       TEXT NOT NULL,                 -- absolute path or "<builtin>"
    parsed_json     TEXT NOT NULL,                 -- full TOML dump
    sha256          TEXT NOT NULL,                 -- hash of the source file
    enabled         INTEGER NOT NULL DEFAULT 1,    -- 0 = disabled, 1 = active
    created_at      TEXT NOT NULL,
    activated_at    TEXT,
    UNIQUE(constitution_id, version)
);
CREATE INDEX IF NOT EXISTS idx_constitutions_id     ON constitutions(constitution_id);
CREATE INDEX IF NOT EXISTS idx_constitutions_active ON constitutions(enabled);

CREATE TABLE IF NOT EXISTS mods (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    mod_id        TEXT NOT NULL UNIQUE,            -- e.g. "user/osint-cve-deepdive"
    name          TEXT NOT NULL,
    version       TEXT NOT NULL,                   -- semver
    source        TEXT NOT NULL,                   -- user:<path> | registry:<url>
    manifest_json TEXT NOT NULL,                   -- parsed mod.toml
    sha256        TEXT NOT NULL,
    risk_class    TEXT,                            -- research-only | active-probing | exploit-development
    target_scope  TEXT,                            -- public_internet | private_infrastructure | darkweb
    requires_tor  INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    updated_at    TEXT
);
CREATE INDEX IF NOT EXISTS idx_mods_id     ON mods(mod_id);
CREATE INDEX IF NOT EXISTS idx_mods_risk   ON mods(risk_class);

CREATE TABLE IF NOT EXISTS mod_loads (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    mod_id              TEXT NOT NULL,
    session_id          TEXT,
    loaded_at           TEXT NOT NULL,
    duration_ms         INTEGER NOT NULL DEFAULT 0,
    capabilities_count  INTEGER NOT NULL DEFAULT 0,
    error               TEXT,                       -- non-empty = load failed
    constitution_id     TEXT                        -- constitution under which the mod was loaded
);
CREATE INDEX IF NOT EXISTS idx_mod_loads_mod     ON mod_loads(mod_id);
CREATE INDEX IF NOT EXISTS idx_mod_loads_session ON mod_loads(session_id);
`

// schemaV3 extends sdd_evaluations with anti-refusal / constitution
// audit fields. SQLite does not support ALTER TABLE ADD COLUMN IF NOT
// EXISTS, but that's fine here: the migration runner in Migrate() only
// runs each migration's Up once (it records the version in
// schema_migrations and skips already-applied entries). So this block
// is only ever executed on a DB that does not have these columns yet.
// All new columns are nullable or have DEFAULT 0 so existing rows
// remain valid after the ALTER.
//
//   constitution_id     identifier of the constitution in effect
//                       when the judge ran. NULL means pre-v3 record.
//   constitution_version semver of that constitution. Together with
//                       constitution_id gives "dark-research/light@1.0.0".
//   active_mods_json    JSON array of "mod_id@version" strings. Lets
//                       us answer "which mods were active when this
//                       judge verdict was emitted?" — the chain of
//                       custody needed to reproduce a decision.
//   refused_attempts    how many times the LLM had to be retried
//                       before emitting a parseable verdict. 0 =
//                       first try succeeded. This is the column the
//                       dark-matrix-analysis skill will group by
//                       when surfacing refusal rates.
//   refusal_pattern     when refused_attempts > 0, the regex that
//                       matched the refusal signal (e.g.
//                       "I cannot help with that"). NULL otherwise.
const schemaV3 = `
ALTER TABLE sdd_evaluations ADD COLUMN constitution_id     TEXT;
ALTER TABLE sdd_evaluations ADD COLUMN constitution_version TEXT;
ALTER TABLE sdd_evaluations ADD COLUMN active_mods_json    TEXT;
ALTER TABLE sdd_evaluations ADD COLUMN refused_attempts    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sdd_evaluations ADD COLUMN refusal_pattern     TEXT;
`

// migrateTable is the bookkeeping table for applied migrations. Created
// on the first Open(); updated by Migrate. Kept minimal so older DBs
// (which had a 2-column version,applied_at shape) remain compatible.
const migrateTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	applied_at TEXT NOT NULL
);
`

// Migrate applies every pending migration in AllMigrations order. Each
// migration runs in its own transaction so a failure on v3 leaves v1+v2
// applied. Idempotent: safe to call repeatedly.
func (s *Store) Migrate(ctx context.Context) error {
	// Ensure bookkeeping table exists.
	if _, err := s.db.ExecContext(ctx, migrateTable); err != nil {
		return fmt.Errorf("mem: migrate table: %w", err)
	}

	applied, err := s.appliedVersionsWithTimestamps(ctx)
	if err != nil {
		return err
	}

	for _, m := range AllMigrations {
		if _, ok := applied[m.Version]; ok {
			continue
		}
		if err := s.applyOne(ctx, m); err != nil {
			return err
		}
		applied[m.Version] = Now()
	}
	return nil
}

// applyOne runs a single migration's Up SQL and records it.
func (s *Store) applyOne(ctx context.Context, m Migration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mem: migrate v%d begin: %w", m.Version, err)
	}
	defer func() { _ = tx.Rollback() }()

	if m.Up != "" {
		if _, err := tx.ExecContext(ctx, m.Up); err != nil {
			return fmt.Errorf("mem: migrate v%d (%s) up: %w", m.Version, m.Name, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		m.Version, Now()); err != nil {
		return fmt.Errorf("mem: migrate v%d record: %w", m.Version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mem: migrate v%d commit: %w", m.Version, err)
	}
	return nil
}

// appliedVersionsWithTimestamps returns version -> applied_at.
func (s *Store) appliedVersionsWithTimestamps(ctx context.Context) (map[int]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT version, applied_at FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("mem: migrate query: %w", err)
	}
	defer rows.Close()

	out := map[int]string{}
	for rows.Next() {
		var v int
		var ts string
		if err := rows.Scan(&v, &ts); err != nil {
			return nil, err
		}
		out[v] = ts
	}
	return out, rows.Err()
}

// SchemaVersion returns the highest applied migration version (0 if
// nothing has been applied yet).
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("mem: schema version: %w", err)
	}
	return int(v.Int64), nil
}

// MigrationStatus describes one migration and whether it has been applied.
type MigrationStatus struct {
	Version   int    `json:"version"`
	Name      string `json:"name"`
	Applied   bool   `json:"applied"`
	AppliedAt string `json:"applied_at,omitempty"`
}

// MigrationStatus returns the full state: every registered migration and
// whether it's been applied to this DB. Names come from the in-process
// AllMigrations registry (so renaming a migration in code is reflected
// without a DB rewrite). Used by the agent to introspect ("is my DB at
// v1 or do I need to upgrade?").
func (s *Store) MigrationStatus(ctx context.Context) ([]MigrationStatus, error) {
	applied, err := s.appliedVersionsWithTimestamps(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]MigrationStatus, 0, len(AllMigrations))
	for _, m := range AllMigrations {
		st := MigrationStatus{Version: m.Version, Name: m.Name}
		if ts, ok := applied[m.Version]; ok {
			st.Applied = true
			st.AppliedAt = ts
		}
		out = append(out, st)
	}
	return out, nil
}