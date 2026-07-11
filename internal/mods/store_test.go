package mods

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dark-agents/research-mcp/internal/mem"
)

// TestStore_SaveModAndListMods exercises the upsert path
// (insert + update on conflict) and confirms ListMods
// returns the row in newest-first order.
func TestStore_SaveModAndListMods(t *testing.T) {
	s := memStoreForMods(t)
	st := NewStore(context.Background(), s.DB())

	// Build a Loaded value from a real-looking manifest.
	raw := []byte(`
[meta]
id = "store/test-save"
name = "Save Test"
version = "1.0.0"

[activation]
auto_load = false

[risk]
risk_class = "research-only"
target_scope = "public_internet"
requires_tor = false
`)
	loaded, err := LoadManifestBytes(raw, "<test>")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveMod(context.Background(), loaded); err != nil {
		t.Fatal(err)
	}

	rows, err := st.ListMods(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListMods = %d rows, want 1", len(rows))
	}
	if rows[0].ModID != "store/test-save" {
		t.Errorf("ModID = %q", rows[0].ModID)
	}
	if rows[0].Version != "1.0.0" {
		t.Errorf("Version = %q", rows[0].Version)
	}
	if rows[0].RiskClass != "research-only" {
		t.Errorf("RiskClass = %q", rows[0].RiskClass)
	}
	if rows[0].SHA256 == "" {
		t.Error("SHA256 empty")
	}

	// Save again with a new version; this is an upsert.
	loaded.Manifest.Meta.Version = "1.1.0"
	if err := st.SaveMod(context.Background(), loaded); err != nil {
		t.Fatal(err)
	}
	rows, _ = st.ListMods(context.Background(), 10)
	if len(rows) != 1 {
		t.Fatalf("after upsert: %d rows, want 1", len(rows))
	}
	if rows[0].Version != "1.1.0" {
		t.Errorf("after upsert Version = %q, want 1.1.0", rows[0].Version)
	}
}

// TestStore_RecordLoad_AppendsRows verifies that RecordLoad
// is append-only and the rows can be listed.
func TestStore_RecordLoad_AppendsRows(t *testing.T) {
	s := memStoreForMods(t)
	st := NewStore(context.Background(), s.DB())
	ctx := context.Background()

	if err := st.RecordLoad(ctx, "store/test-record", "session-1", 50, 3, "", "dark-research/aggressive"); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordLoad(ctx, "store/test-record", "session-1", 80, 3, "", ""); err != nil {
		t.Fatal(err)
	}

	rows, err := st.ListModLoads(ctx, "store/test-record", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("ListModLoads = %d, want 2", len(rows))
	}
	// Newest first.
	if rows[0].DurationMs != 80 {
		t.Errorf("newest DurationMs = %d, want 80", rows[0].DurationMs)
	}
	if rows[0].ConstitutionID != "" {
		t.Errorf("second load has empty constitution_id; got %q", rows[0].ConstitutionID)
	}
	if rows[1].ConstitutionID != "dark-research/aggressive" {
		t.Errorf("first load constitution_id = %q, want dark-research/aggressive", rows[1].ConstitutionID)
	}
}

// TestStore_RecordLoad_RecordsErrors verifies the error path
// persists the error string.
func TestStore_RecordLoad_RecordsErrors(t *testing.T) {
	s := memStoreForMods(t)
	st := NewStore(context.Background(), s.DB())
	if err := st.RecordLoad(context.Background(), "store/err", "", 0, 0, "manifest invalid: foo", ""); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.ListModLoads(context.Background(), "store/err", 10)
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].Error == "" {
		t.Error("Error field is empty")
	}
}

// memStoreForMods opens a fresh in-memory dark.db for the mods
// store tests. Uses the same mem package as the rest of the
// project (so the schema is shared) but writes to a temp file
// to keep each test isolated.
func memStoreForMods(t *testing.T) *mem.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mods-test.db")
	s, err := mem.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
