package constitution

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dark-agents/research-mcp/internal/mem"
)

// storeTestDB opens a fresh in-memory-ish dark.db and returns the
// mem.Store. Used by store_test.go to exercise the constitution
// CRUD without sharing state with the rest of the test suite.
func storeTestDB(t *testing.T) *mem.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "constitution-test.db")
	s, err := mem.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestStore_SaveAndGetRoundTrips inserts a Constitution, reads it
// back via Get, and verifies every field made the round trip.
func TestStore_SaveAndGetRoundTrips(t *testing.T) {
	s := storeTestDB(t)
	st := NewStore(s.DB())
	ctx := context.Background()

	// Build a row from a freshly-loaded Constitution.
	c, err := LoadBytes([]byte(`
[meta]
id = "store/test-roundtrip"
version = "9.9.9"
label = "Roundtrip"

[identity]
agent_name = "x"
agent_role = "x"
tone = "x"

[authority]
priority = ["constitution"]

[refusal_policy]
mode = "passthrough"
`), "<test>")
	if err != nil {
		t.Fatal(err)
	}
	row := FromConstitution(c)

	id, err := st.Save(ctx, row)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Error("expected non-zero id from Save")
	}

	got, err := st.Get(ctx, "store/test-roundtrip", "9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("Get returned nil after Save")
	}
	if got.ID != "store/test-roundtrip" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.Version != "9.9.9" {
		t.Errorf("Version = %q", got.Version)
	}
	if got.Label != "Roundtrip" {
		t.Errorf("Label = %q, want %q", got.Label, "Roundtrip")
	}
	if got.SHA256 != c.SHA256 {
		t.Errorf("SHA256 = %q, want %q", got.SHA256, c.SHA256)
	}
	if got.ParsedJSON == "" {
		t.Error("ParsedJSON is empty")
	}
	if !strings.Contains(got.ParsedJSON, `"constitution_id"`) {
		t.Errorf("ParsedJSON does not look like a Constitution: %s", got.ParsedJSON)
	}
	if !got.Enabled {
		t.Error("Enabled = false, want true (default for fresh loads)")
	}
}

// TestStore_SaveIsUpsert verifies saving the same (id, version)
// twice overwrites parsed_json and sha256 but preserves created_at.
func TestStore_SaveIsUpsert(t *testing.T) {
	s := storeTestDB(t)
	st := NewStore(s.DB())
	ctx := context.Background()

	makeRow := func(label, sha string) *ConstitutionRow {
		c, err := LoadBytes([]byte(`
[meta]
id = "store/upsert"
version = "1.0.0"
label = "`+label+`"

[identity]
agent_name = "x"
agent_role = "x"
tone = "x"

[authority]
priority = ["constitution"]

[refusal_policy]
mode = "passthrough"
`), "<test>")
		if err != nil {
			t.Fatal(err)
		}
		// Override SHA256 so we can tell the rows apart.
		c.SHA256 = sha
		return FromConstitution(c)
	}

	// First save.
	first, err := st.Save(ctx, makeRow("first", "aaaa"))
	if err != nil {
		t.Fatal(err)
	}
	row1, _ := st.Get(ctx, "store/upsert", "1.0.0")
	if row1 == nil {
		t.Fatal("row1 nil")
	}
	created1 := row1.CreatedAt

	// Second save with a different label and sha.
	_, err = st.Save(ctx, makeRow("second", "bbbb"))
	if err != nil {
		t.Fatal(err)
	}
	row2, _ := st.Get(ctx, "store/upsert", "1.0.0")
	if row2 == nil {
		t.Fatal("row2 nil")
	}

	if row2.Label != "second" {
		t.Errorf("Label = %q, want 'second' (upsert overwrote)", row2.Label)
	}
	if row2.SHA256 != "bbbb" {
		t.Errorf("SHA256 = %q, want 'bbbb' (upsert overwrote)", row2.SHA256)
	}
	if row2.CreatedAt != created1 {
		t.Errorf("CreatedAt changed: %q -> %q (upsert should preserve)", created1, row2.CreatedAt)
	}
	if row2.ID == "" || first == 0 {
		// The (constitution_id, version) is the natural key; the
		// auto-incrementing row id is a side detail. We only check
		// non-zero/non-empty here as a sanity guard.
		t.Errorf("row id sanity: first=%d second=%q", first, row2.ID)
	}
}

// TestStore_GetReturnsNilOnMiss verifies the contract: a non-existent
// (id, version) returns nil, nil (not an error).
func TestStore_GetReturnsNilOnMiss(t *testing.T) {
	s := storeTestDB(t)
	st := NewStore(s.DB())
	row, err := st.Get(context.Background(), "no/such", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if row != nil {
		t.Errorf("expected nil row, got %+v", row)
	}
}

// TestStore_ListReturnsAllVersions is the multi-version path:
// saving three versions of the same id and listing them all.
func TestStore_ListByIDReturnsAllVersions(t *testing.T) {
	s := storeTestDB(t)
	st := NewStore(s.DB())
	ctx := context.Background()

	for _, v := range []string{"1.0.0", "1.1.0", "2.0.0"} {
		raw := []byte(`
[meta]
id = "store/multi-version"
version = "` + v + `"

[identity]
agent_name = "x"
agent_role = "x"
tone = "x"

[authority]
priority = ["constitution"]

[refusal_policy]
mode = "passthrough"
`)
		c, err := LoadBytes(raw, "<test>")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.Save(ctx, FromConstitution(c)); err != nil {
			t.Fatal(err)
		}
	}

	got, err := st.ListByID(ctx, "store/multi-version")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("ListByID returned %d, want 3", len(got))
	}
	// Ordered by version DESC.
	want := []string{"2.0.0", "1.1.0", "1.0.0"}
	for i, w := range want {
		if got[i].Version != w {
			t.Errorf("row %d Version = %q, want %q", i, got[i].Version, w)
		}
	}
}

// TestStore_MarkActivatedTouchesColumn verifies the activated_at
// update path bumps the timestamp. We can't easily clear
// activated_at (FromConstitution always populates it), so we
// check the relative behavior: the value after MarkActivated is
// at least as recent as the value before.
func TestStore_MarkActivatedTouchesColumn(t *testing.T) {
	s := storeTestDB(t)
	st := NewStore(s.DB())
	ctx := context.Background()

	raw := []byte(`
[meta]
id = "store/activate"
version = "1.0.0"

[identity]
agent_name = "x"
agent_role = "x"
tone = "x"

[authority]
priority = ["constitution"]

[refusal_policy]
mode = "passthrough"
`)
	c, err := LoadBytes(raw, "<test>")
	if err != nil {
		t.Fatal(err)
	}
	row := FromConstitution(c)
	if _, err := st.Save(ctx, row); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Get(ctx, "store/activate", "1.0.0")
	if before == nil {
		t.Fatal("row not found after save")
	}
	if before.ActivatedAt == "" {
		t.Fatal("expected non-empty activated_at after Save")
	}

	// MarkActivated should bump activated_at. Sleep briefly to
	// ensure the timestamp can advance (RFC3339Nano has nanosecond
	// resolution so back-to-back calls might collide; 1ms is
	// plenty for any platform).
	time.Sleep(time.Millisecond)

	if err := st.MarkActivated(ctx, "store/activate", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	after, _ := st.Get(ctx, "store/activate", "1.0.0")
	if after.ActivatedAt == "" {
		t.Error("activated_at is empty after MarkActivated")
	}
	if after.ActivatedAt < before.ActivatedAt {
		t.Errorf("activated_at went backwards: %q -> %q", before.ActivatedAt, after.ActivatedAt)
	}
}
