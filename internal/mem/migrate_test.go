package mem

import (
	"context"
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