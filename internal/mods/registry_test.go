package mods

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// makeTestMod writes a minimal mod directory under parent and
// returns its absolute path. The mod has no knowledge/directive
// files; tests that need them add them explicitly.
func makeTestMod(t *testing.T, parent, shortName string) string {
	t.Helper()
	dir := filepath.Join(parent, shortName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	modTOML := `
[meta]
id = "test/` + shortName + `"
name = "Test ` + shortName + `"
version = "1.0.0"

[activation]
auto_load = false

[risk]
risk_class = "research-only"
target_scope = "public_internet"
requires_tor = false
`
	if err := os.WriteFile(filepath.Join(dir, "mod.toml"), []byte(modTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestRegistry_Discover_FindsModsUnderSearchPath verifies the
// search-path scan returns every mod in the configured dir.
// The exact namespace prefix depends on the path (the user
// home dir yields "user", arbitrary paths use the basename);
// here we just assert the count and the short names.
func TestRegistry_Discover_FindsModsUnderSearchPath(t *testing.T) {
	dir := t.TempDir()
	makeTestMod(t, dir, "alpha")
	makeTestMod(t, dir, "beta")
	t.Setenv("DARK_MODS_PATH", dir)

	r := NewRegistry(nil)
	got, err := r.Discover()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 entries", got)
	}
	// Check that each id ends in the right short name.
	for _, id := range got {
		if id != "alpha" && id != "beta" {
			// Could be prefixed; just check suffix.
			if !endsWithShort(id, "alpha") && !endsWithShort(id, "beta") {
				t.Errorf("unexpected id %q", id)
			}
		}
	}
}

func endsWithShort(id, short string) bool {
	if len(id) <= len(short)+1 {
		return false
	}
	return id[len(id)-len(short)-1] == '/' && id[len(id)-len(short):] == short
}

// TestRegistry_Activate_LoadsAndIsActive is the happy path:
// Activate reads the mod, marks it active, and IsActive
// returns true.
func TestRegistry_Activate_LoadsAndIsActive(t *testing.T) {
	dir := t.TempDir()
	makeTestMod(t, dir, "foo")
	t.Setenv("DARK_MODS_PATH", dir)

	r := NewRegistry(nil)
	loaded, err := r.Activate("test/foo")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Manifest.Meta.ID != "test/foo" {
		t.Errorf("loaded mod id = %q", loaded.Manifest.Meta.ID)
	}
	if !r.IsActive("test/foo") {
		t.Error("IsActive(test/foo) = false after Activate")
	}
}

// TestRegistry_Activate_NotFound verifies the resolver path
// for an unknown mod_id.
func TestRegistry_Activate_NotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DARK_MODS_PATH", dir)
	r := NewRegistry(nil)
	_, err := r.Activate("test/does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing mod")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %q, want 'not found'", err.Error())
	}
}

// TestRegistry_Activate_InvalidModID verifies the resolver
// rejects "namespace/name"-less ids.
func TestRegistry_Activate_InvalidModID(t *testing.T) {
	r := NewRegistry(nil)
	_, err := r.Activate("no-namespace")
	if err == nil {
		t.Fatal("expected error for missing namespace")
	}
}

// TestRegistry_Deactivate_RemovesFromActiveSet verifies the
// round trip.
func TestRegistry_Deactivate_RemovesFromActiveSet(t *testing.T) {
	dir := t.TempDir()
	makeTestMod(t, dir, "foo")
	t.Setenv("DARK_MODS_PATH", dir)

	r := NewRegistry(nil)
	if _, err := r.Activate("test/foo"); err != nil {
		t.Fatal(err)
	}
	if err := r.Deactivate("test/foo"); err != nil {
		t.Fatal(err)
	}
	if r.IsActive("test/foo") {
		t.Error("IsActive = true after Deactivate")
	}
}

// TestRegistry_Deactivate_NotActive verifies the error path.
func TestRegistry_Deactivate_NotActive(t *testing.T) {
	r := NewRegistry(nil)
	err := r.Deactivate("test/never-activated")
	if !errors.Is(err, ErrNotActive) {
		t.Errorf("err = %v, want ErrNotActive", err)
	}
}

// TestRegistry_List_SortedByID verifies the active mod set
// is returned in stable order.
func TestRegistry_List_SortedByID(t *testing.T) {
	dir := t.TempDir()
	makeTestMod(t, dir, "zeta")
	makeTestMod(t, dir, "alpha")
	t.Setenv("DARK_MODS_PATH", dir)

	r := NewRegistry(nil)
	if _, err := r.Activate("test/zeta"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Activate("test/alpha"); err != nil {
		t.Fatal(err)
	}
	got := r.List()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Manifest.Meta.ID != "test/alpha" {
		t.Errorf("List[0] = %q, want test/alpha (sorted)", got[0].Manifest.Meta.ID)
	}
	if got[1].Manifest.Meta.ID != "test/zeta" {
		t.Errorf("List[1] = %q, want test/zeta (sorted)", got[1].Manifest.Meta.ID)
	}
}

// TestRegistry_AsModDirectives_FlattensKnowledgeAndDirectives
// verifies the cross-package bridge: every knowledge and
// directive file becomes one ModDirectiveOut in stable order.
func TestRegistry_AsModDirectives_FlattensKnowledgeAndDirectives(t *testing.T) {
	dir := t.TempDir()
	modPath := makeTestMod(t, dir, "foo")
	if err := os.MkdirAll(filepath.Join(modPath, "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modPath, "knowledge", "k1.md"), []byte("knowledge-one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(modPath, "directives"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modPath, "directives", "d1.md"), []byte("directive-one"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Rewrite the mod.toml to reference the new files.
	if err := os.WriteFile(filepath.Join(modPath, "mod.toml"), []byte(`
[meta]
id = "test/foo"
name = "Test"
version = "1.0.0"

[knowledge]
prompt_injections = ["knowledge/k1.md"]
data_sources = []

[directives]
prompt_fragments = ["directives/d1.md"]

[activation]
auto_load = false

[risk]
risk_class = "research-only"
target_scope = "public_internet"
requires_tor = false
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DARK_MODS_PATH", dir)

	r := NewRegistry(nil)
	if _, err := r.Activate("test/foo"); err != nil {
		t.Fatal(err)
	}

	dirs := r.AsModDirectives()
	if len(dirs) != 2 {
		t.Fatalf("AsModDirectives = %d, want 2", len(dirs))
	}
	// Find each by body to avoid order coupling.
	var sawKnowledge, sawDirective bool
	for _, d := range dirs {
		if d.ModID != "test/foo" {
			t.Errorf("ModID = %q, want test/foo", d.ModID)
		}
		switch d.Body {
		case "knowledge-one":
			if d.Source != "prompt_injection" {
				t.Errorf("knowledge Source = %q, want 'prompt_injection'", d.Source)
			}
			sawKnowledge = true
		case "directive-one":
			if d.Source != "directive" {
				t.Errorf("directive Source = %q, want 'directive'", d.Source)
			}
			sawDirective = true
		}
	}
	if !sawKnowledge || !sawDirective {
		t.Errorf("missing one of knowledge/directive: k=%v d=%v", sawKnowledge, sawDirective)
	}
}

// TestRegistry_RecordLoad_NoStoreIsNoOp verifies the best-effort
// store contract: a nil store means recordLoad is a silent
// no-op.
func TestRegistry_RecordLoad_NoStoreIsNoOp(t *testing.T) {
	r := NewRegistry(nil)
	// Should not panic.
	r.recordLoad("test/foo", "abc", 100, 5, nil)
}
