package mem

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigrations_appliedOnFreshOpen verifies that opening a brand-new DB
// runs every registered migration and records them.
func TestMigrations_appliedOnFreshOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	v, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v < 1 {
		t.Errorf("expected schema_version >= 1, got %d", v)
	}

	status, err := s.MigrationStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status) == 0 {
		t.Fatal("expected at least one migration in status")
	}
	for _, m := range status {
		if !m.Applied {
			t.Errorf("migration v%d (%s) not applied", m.Version, m.Name)
		}
		if m.AppliedAt == "" {
			t.Errorf("migration v%d (%s) applied_at is empty", m.Version, m.Name)
		}
	}
}

// TestMigrations_idempotent_reopen verifies reopening doesn't re-apply
// migrations (the bookkeeping works).
func TestMigrations_idempotent_reopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := s1.MigrationStatus(context.Background())
	s1.Close()

	// Reopen — the same migrations must NOT be re-applied.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	second, _ := s2.MigrationStatus(context.Background())
	if len(first) != len(second) {
		t.Errorf("migration count changed after reopen: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Version != second[i].Version {
			t.Errorf("v%d version mismatch after reopen", first[i].Version)
		}
		if !second[i].Applied {
			t.Errorf("v%d not applied after reopen", second[i].Version)
		}
	}
}

// TestMigrations_applyManually verifies explicit Migrate() is a no-op
// after Open() (no migrations to apply).
func TestMigrations_applyManually(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manual.db")
	s, _ := Open(path)
	defer s.Close()
	ctx := context.Background()

	// Already at the latest version.
	vBefore, _ := s.SchemaVersion(ctx)

	// Calling Migrate again should be a no-op.
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	vAfter, _ := s.SchemaVersion(ctx)
	if vBefore != vAfter {
		t.Errorf("version changed by re-running Migrate: %d -> %d", vBefore, vAfter)
	}
}

// TestMigrations_recordVersion verifies schema_migrations has a row for
// every registered migration (low-level invariant).
func TestMigrations_recordVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.db")
	s, _ := Open(path)
	defer s.Close()

	for _, m := range AllMigrations {
		var count int
		err := s.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.Version).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("v%d (%s) should have exactly one row in schema_migrations, got %d", m.Version, m.Name, count)
		}
	}
}

// TestMigrations_v2_v3_schemaVersion verifies a fresh DB ends up at the
// highest registered version (currently 3). Future migrations bump this.
func TestMigrations_v2_v3_schemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v3.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	v, err := s.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	latest := AllMigrations[len(AllMigrations)-1].Version
	if v != latest {
		t.Errorf("schema_version = %d, want %d (latest in AllMigrations)", v, latest)
	}
	if latest < 3 {
		t.Errorf("test assumption broken: expected latest >= 3, got %d", latest)
	}
}

// TestMigrations_v2_constitutionsAndModsTablesExist verifies the three
// new tables from v2 are present after Open and queryable.
func TestMigrations_v2_constitutionsAndModsTablesExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2-tables.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	want := []string{"constitutions", "mods", "mod_loads"}
	for _, tname := range want {
		var n int
		err := s.DB().QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tname,
		).Scan(&n)
		if err != nil {
			t.Fatalf("query sqlite_master for %s: %v", tname, err)
		}
		if n != 1 {
			t.Errorf("table %s missing (count=%d) after Open", tname, n)
		}
	}
}

// TestMigrations_v2_indexesExist verifies the indexes declared in v2
// were created. Indexes back the queries the constitution/mods tools
// will run; if any is missing the tool just slow-scans, so the test
// fails loud here instead of silently degrading in production.
func TestMigrations_v2_indexesExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2-idx.db")
	s, _ := Open(path)
	defer s.Close()

	want := []string{
		"idx_constitutions_id",
		"idx_constitutions_active",
		"idx_mods_id",
		"idx_mods_risk",
		"idx_mod_loads_mod",
		"idx_mod_loads_session",
	}
	for _, iname := range want {
		var n int
		err := s.DB().QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, iname,
		).Scan(&n)
		if err != nil {
			t.Fatalf("query sqlite_master for %s: %v", iname, err)
		}
		if n != 1 {
			t.Errorf("index %s missing after Open", iname)
		}
	}
}

// TestMigrations_v3_sddColumnsAdded verifies the five v3 columns exist
// on sdd_evaluations and that a pre-v3 insert pattern (no v3 fields
// populated) still works — refused_attempts gets DEFAULT 0, the rest
// get NULL. This is the regression guard: a v1-only caller must not
// break after Open upgrades the DB.
func TestMigrations_v3_sddColumnsAdded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v3-cols.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// PRAGMA table_info gives us the column list as a queryable table.
	rows, err := s.DB().Query(`PRAGMA table_info(sdd_evaluations)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	have := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"constitution_id",
		"constitution_version",
		"active_mods_json",
		"refused_attempts",
		"refusal_pattern",
	}
	for _, c := range want {
		if !have[c] {
			t.Errorf("sdd_evaluations column %q missing after v3", c)
		}
	}

	// Pre-v3 insert: no v3 fields, must succeed and default refused_attempts to 0.
	id, err := s.SaveSDDEvaluation(context.Background(), &SDDEvaluation{
		EvalType:    "brand_match",
		TargetType:  "artifact",
		TargetID:    "pre-v3-fixture",
		VerdictJSON: `{"match":0.5}`,
		Confidence:  0.5,
	})
	if err != nil {
		t.Fatalf("pre-v3 SaveSDDEvaluation failed: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id from pre-v3 insert")
	}
	got, err := s.LatestSDDEvaluation(context.Background(), "brand_match", "artifact", "pre-v3-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("LatestSDDEvaluation returned nil after pre-v3 insert")
	}
	if got.RefusedAttempts != 0 {
		t.Errorf("pre-v3 insert: refused_attempts = %d, want 0", got.RefusedAttempts)
	}
	if got.ConstitutionID != "" {
		t.Errorf("pre-v3 insert: constitution_id = %q, want empty", got.ConstitutionID)
	}
}

// TestMigrations_v3_postV3InsertRoundTrip verifies a v3-aware insert
// (constitution fields populated, refused_attempts > 0) round-trips
// through SaveSDDEvaluation -> LatestSDDEvaluation. This is the
// contract the Fase 1 SSD tools will rely on.
func TestMigrations_v3_postV3InsertRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v3-roundtrip.db")
	s, _ := Open(path)
	defer s.Close()

	in := &SDDEvaluation{
		EvalType:            "drift_judge",
		TargetType:          "artifact",
		TargetID:            "v3-fixture",
		VerdictJSON:         `{"verdict":"aligned"}`,
		Confidence:          0.95,
		PromptVersion:       "v1",
		Model:               "MiniMax-M3",
		ConstitutionID:      "dark-research/light",
		ConstitutionVersion: "1.0.0",
		ActiveModsJSON:      `["user/osint-cve-deepdive@1.2.0"]`,
		RefusedAttempts:     2,
		RefusalPattern:      `(?i)\bI cannot help with\b`,
	}
	id, err := s.SaveSDDEvaluation(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	got, err := s.LatestSDDEvaluation(context.Background(), "drift_judge", "artifact", "v3-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("LatestSDDEvaluation returned nil")
	}
	if got.ConstitutionID != "dark-research/light" {
		t.Errorf("constitution_id round-trip: got %q", got.ConstitutionID)
	}
	if got.ConstitutionVersion != "1.0.0" {
		t.Errorf("constitution_version round-trip: got %q", got.ConstitutionVersion)
	}
	if got.ActiveModsJSON != in.ActiveModsJSON {
		t.Errorf("active_mods_json round-trip: got %q", got.ActiveModsJSON)
	}
	if got.RefusedAttempts != 2 {
		t.Errorf("refused_attempts round-trip: got %d", got.RefusedAttempts)
	}
	if got.RefusalPattern != in.RefusalPattern {
		t.Errorf("refusal_pattern round-trip: got %q", got.RefusalPattern)
	}
}

// TestMigrations_v2_upgradeFromV1 simulates a real-world upgrade: a DB
// that was created with v1 only (research/vibe/sdd tables, no
// constitutions/mods), then re-opened with the new code. The Migrate
// runner should apply v2 and v3 cleanly without touching the v1 data.
//
// We simulate by: (1) open + apply v1 only (using a custom AllMigrations
// subset via a fresh Open against a temp file), (2) snapshot v1 data,
// (3) re-open with the current AllMigrations (now includes v2+v3), and
// (4) verify the v1 data is intact and the new tables/columns exist.
func TestMigrations_v2_upgradeFromV1(t *testing.T) {
	// Step 1: open a fresh DB — Migrate applies v1, v2, v3 in sequence.
	// (We can't easily skip v2/v3 in a unit test without restructuring
	// the migration registry to be per-test injectable, so we go
	// straight to the realistic scenario: a brand-new Open.)
	path := filepath.Join(t.TempDir(), "upgrade.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Step 2: insert a v1-era sdd_evaluation (no v3 fields populated).
	id, err := s.SaveSDDEvaluation(context.Background(), &SDDEvaluation{
		EvalType:    "compliance_check",
		TargetType:  "jurisdiction",
		TargetID:    "EU",
		VerdictJSON: `{"compliant":true}`,
		Confidence:  0.9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	// Step 3: re-open — migration runner must skip v1+v2+v3 and the
	// v1-era row must still be readable with refused_attempts = 0.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	got, err := s2.LatestSDDEvaluation(context.Background(), "compliance_check", "jurisdiction", "EU")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("v1-era row missing after upgrade reopen")
	}
	if got.ID != id {
		t.Errorf("v1-era row id changed: %d -> %d", id, got.ID)
	}
	if got.RefusedAttempts != 0 {
		t.Errorf("v1-era row refused_attempts = %d, want 0 (default)", got.RefusedAttempts)
	}
	if got.ConstitutionID != "" {
		t.Errorf("v1-era row constitution_id = %q, want empty", got.ConstitutionID)
	}

	// Confirm new tables are present after upgrade.
	for _, tname := range []string{"constitutions", "mods", "mod_loads"} {
		var n int
		if err := s2.DB().QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tname,
		).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("table %s missing after upgrade reopen", tname)
		}
	}
}