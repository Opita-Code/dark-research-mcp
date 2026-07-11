package mods

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoad_ValidTOML_RoundTrips is the basic canary: a valid
// mod.toml parses, the manifest is populated, and SHA256 is
// computed.
func TestLoad_ValidTOML_RoundTrips(t *testing.T) {
	raw := []byte(`
[meta]
id = "test/loader-roundtrip"
name = "Loader Roundtrip"
version = "0.1.0"

[activation]
auto_load = false

[risk]
risk_class = "research-only"
target_scope = "public_internet"
requires_tor = false
`)
	c, err := LoadManifestBytes(raw, "<test>")
	if err != nil {
		t.Fatalf("LoadManifestBytes: %v", err)
	}
	if c.Manifest.Meta.ID != "test/loader-roundtrip" {
		t.Errorf("Meta.ID = %q", c.Manifest.Meta.ID)
	}
	if c.SHA256 == "" {
		t.Error("SHA256 empty")
	}
	if len(c.SHA256) != 64 {
		t.Errorf("SHA256 wrong length: %d", len(c.SHA256))
	}
}

// TestLoad_MissingMetaID_Fails verifies the validator catches
// the missing required field.
func TestLoad_MissingMetaID_Fails(t *testing.T) {
	raw := []byte(`
[meta]
name = "x"
version = "1.0.0"
`)
	_, err := LoadManifestBytes(raw, "<test>")
	if err == nil {
		t.Fatal("expected error on missing meta.id")
	}
	if !errors.Is(err, ErrInvalidManifest) {
		t.Errorf("err is not ErrInvalidManifest: %v", err)
	}
}

// TestLoad_MalformedModID_Fails verifies the namespace/name
// format check.
func TestLoad_MalformedModID_Fails(t *testing.T) {
	raw := []byte(`
[meta]
id = "no-namespace"
name = "x"
version = "1.0.0"
`)
	_, err := LoadManifestBytes(raw, "<test>")
	if err == nil {
		t.Fatal("expected error on malformed mod id")
	}
	if !strings.Contains(err.Error(), "namespace/name") {
		t.Errorf("err message = %q, want 'namespace/name'", err.Error())
	}
}

// TestLoad_UnknownRiskClass_Fails verifies the validator
// catches an unrecognized risk class.
func TestLoad_UnknownRiskClass_Fails(t *testing.T) {
	raw := []byte(`
[meta]
id = "test/x"
name = "x"
version = "1.0.0"

[risk]
risk_class = "destroy-the-lab"
target_scope = "public_internet"
requires_tor = false
`)
	_, err := LoadManifestBytes(raw, "<test>")
	if err == nil {
		t.Fatal("expected error on unknown risk class")
	}
	if !strings.Contains(err.Error(), "risk_class") {
		t.Errorf("err message = %q, want 'risk_class'", err.Error())
	}
}

// TestLoad_UnknownFieldRejected verifies the strict decoder
// rejects unknown keys (catches typos in the schema).
func TestLoad_UnknownFieldRejected(t *testing.T) {
	raw := []byte(`
[meta]
id = "test/x"
name = "x"
version = "1.0.0"
unknown_typo_field = "x"

[risk]
risk_class = "research-only"
target_scope = "public_internet"
requires_tor = false
`)
	_, err := LoadManifestBytes(raw, "<test>")
	if err == nil {
		t.Fatal("expected error on unknown field")
	}
}

// TestLoad_PathTraversal_Rejected verifies that mod paths
// can't escape the mod root (the validateModPath check).
func TestLoad_PathTraversal_Rejected(t *testing.T) {
	raw := []byte(`
[meta]
id = "test/x"
name = "x"
version = "1.0.0"

[knowledge]
prompt_injections = ["../../../etc/passwd"]
data_sources = []

[activation]
auto_load = false

[risk]
risk_class = "research-only"
target_scope = "public_internet"
requires_tor = false
`)
	_, err := LoadManifestBytes(raw, "<test>")
	if err == nil {
		t.Fatal("expected error on path traversal")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("err message = %q, want 'escapes'", err.Error())
	}
}

// TestLoad_AbsolutePath_Rejected verifies absolute paths
// in knowledge/directives are rejected. Uses t.TempDir() to
// build a guaranteed-absolute path on every platform.
func TestLoad_AbsolutePath_Rejected(t *testing.T) {
	absPath := t.TempDir() // absolute on Linux, macOS, and Windows

	raw := []byte(`
[meta]
id = "test/x"
name = "x"
version = "1.0.0"

[knowledge]
prompt_injections = ["` + filepath.ToSlash(absPath) + `/foo.md"]
data_sources = []

[activation]
auto_load = false

[risk]
risk_class = "research-only"
target_scope = "public_internet"
requires_tor = false
`)
	_, err := LoadManifestBytes(raw, "<test>")
	if err == nil {
		t.Fatal("expected error on absolute path")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("err = %q, want 'absolute'", err.Error())
	}
}

// TestLoad_NoManifestInDir verifies the ErrNoManifest error
// when the directory has no mod.toml.
func TestLoad_NoManifestInDir(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for missing mod.toml")
	}
	if !errors.Is(err, ErrNoManifest) {
		t.Errorf("err is not ErrNoManifest: %v", err)
	}
}

// TestLoad_ReadsKnowledgeAndDirectiveFiles is an integration
// test: write a temp dir with mod.toml + knowledge/foo.md +
// directives/bar.md and verify the loader reads the content.
func TestLoad_ReadsKnowledgeAndDirectiveFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.toml"), []byte(`
[meta]
id = "test/load-files"
name = "Load Files"
version = "1.0.0"

[knowledge]
prompt_injections = ["knowledge/foo.md"]
data_sources = ["knowledge/data.toml"]

[directives]
prompt_fragments = ["directives/bar.md"]

[activation]
auto_load = false

[risk]
risk_class = "research-only"
target_scope = "public_internet"
requires_tor = false
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "knowledge", "foo.md"), []byte("# Foo\nbody of foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "knowledge", "data.toml"), []byte("key = \"value\""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "directives"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "directives", "bar.md"), []byte("Follow this rule."), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.Knowledge) != 2 {
		t.Fatalf("expected 2 knowledge items, got %d", len(loaded.Knowledge))
	}
	if loaded.Knowledge[0].Path != "knowledge/foo.md" {
		t.Errorf("Knowledge[0].Path = %q", loaded.Knowledge[0].Path)
	}
	if loaded.Knowledge[0].Body != "# Foo\nbody of foo" {
		t.Errorf("Knowledge[0].Body = %q", loaded.Knowledge[0].Body)
	}
	if loaded.Knowledge[0].Kind != "prompt_injection" {
		t.Errorf("Knowledge[0].Kind = %q", loaded.Knowledge[0].Kind)
	}
	if loaded.Knowledge[1].Kind != "data_source" {
		t.Errorf("Knowledge[1].Kind = %q, want 'data_source'", loaded.Knowledge[1].Kind)
	}

	if len(loaded.Directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(loaded.Directives))
	}
	if loaded.Directives[0].Body != "Follow this rule." {
		t.Errorf("Directives[0].Body = %q", loaded.Directives[0].Body)
	}
}
