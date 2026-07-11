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