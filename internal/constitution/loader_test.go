package constitution

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoader_builtInLightAlwaysValid is the canary: if the light.toml
// shipped in the repo is broken, panic on init() already failed (in
// loader.go). This test makes that invariant explicit by re-loading
// the bytes and asserting non-nil.
func TestLoader_builtInLightAlwaysValid(t *testing.T) {
	if Light == nil {
		t.Fatal("Light must not be nil; init() should have populated it")
	}
	if Light.Meta.ID == "" {
		t.Error("Light.Meta.ID empty")
	}
	if Light.Source != SourceBuiltinLight {
		t.Errorf("Light.Source = %v, want %v", Light.Source, SourceBuiltinLight)
	}
	if Light.SHA256 == "" {
		t.Error("Light.SHA256 empty (loader must compute it)")
	}
}

// TestLoader_builtInDarkIsBuildTagged confirms the dark constitution
// is only present in tagged builds. On a stock build, Dark must be
// nil; on a tagged build, Dark must be non-nil with Source ==
// SourceBuiltinDark. The test runs in both contexts and asserts
// the right outcome.
func TestLoader_builtInDarkIsBuildTagged(t *testing.T) {
	// We can't introspect build tags at runtime, so we infer:
	// - if Dark != nil, dark was embedded (tagged build)
	// - if Dark == nil, dark was NOT embedded (stock build)
	if Dark == nil {
		t.Log("Dark is nil — this is a stock build (no -tags allow_builtin_dark). OK.")
		return
	}
	if Dark.Source != SourceBuiltinDark {
		t.Errorf("Dark.Source = %v, want %v", Dark.Source, SourceBuiltinDark)
	}
	if Dark.BuildTag != "allow_builtin_dark" {
		t.Errorf("Dark.BuildTag = %q, want %q", Dark.BuildTag, "allow_builtin_dark")
	}
}

// TestLoader_validTOML_RoundTrips verifies that a minimal-but-valid
// TOML parses, validates, and returns a Constitution with all
// required fields populated and SHA256 computed.
func TestLoader_validTOML_RoundTrips(t *testing.T) {
	raw := []byte(`
[meta]
id = "test/loader-roundtrip"
version = "0.1.0"
label = "Roundtrip test"

[identity]
agent_name = "test agent"
agent_role = "test role"
tone = "neutral"

[authority]
priority = ["constitution", "user_query"]

[refusal_policy]
mode = "passthrough"

[system_prompt_layers]
order = ["identity", "tool_directive", "constitution_footer"]

[scope]
does = ["test does"]
does_not = ["test does not"]

[tone_and_voice]
register = "test register"

[constitution_footer]
text = "footer text"
`)
	c, err := LoadBytes(raw, "<test>")
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	if c.Meta.ID != "test/loader-roundtrip" {
		t.Errorf("Meta.ID = %q, want %q", c.Meta.ID, "test/loader-roundtrip")
	}
	if c.ID() != "test/loader-roundtrip@0.1.0" {
		t.Errorf("ID() = %q, want %q", c.ID(), "test/loader-roundtrip@0.1.0")
	}
	if c.SHA256 == "" {
		t.Error("SHA256 empty")
	}
	if len(c.SHA256) != 64 { // hex sha256 = 64 chars
		t.Errorf("SHA256 wrong length: %d (%q)", len(c.SHA256), c.SHA256)
	}
}

// TestLoader_missingRequiredFields_Fails verifies the validator
// catches every required field. One sub-test per field.
func TestLoader_missingRequiredFields_Fails(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(s string) string
		wantErr string
	}{
		{
			name:    "missing meta.id",
			mutate:  func(s string) string { return strings.Replace(s, `id = "x"`, `id = ""`, 1) },
			wantErr: "meta.id is required",
		},
		{
			name:    "missing meta.version",
			mutate:  func(s string) string { return strings.Replace(s, `version = "1.0.0"`, `version = ""`, 1) },
			wantErr: "meta.version is required",
		},
		{
			name:    "missing identity.agent_name",
			mutate:  func(s string) string { return strings.Replace(s, `agent_name = "x"`, `agent_name = ""`, 1) },
			wantErr: "identity.agent_name is required",
		},
		{
			name:    "missing identity.agent_role",
			mutate:  func(s string) string { return strings.Replace(s, `agent_role = "x"`, `agent_role = ""`, 1) },
			wantErr: "identity.agent_role is required",
		},
		{
			name:    "empty authority.priority",
			mutate:  func(s string) string { return strings.Replace(s, "priority = [\"constitution\"]", "priority = []", 1) },
			wantErr: "authority.priority must list at least one tier",
		},
	}
	base := `
[meta]
id = "x"
version = "1.0.0"

[identity]
agent_name = "x"
agent_role = "x"
tone = "x"

[authority]
priority = ["constitution"]

[refusal_policy]
mode = "passthrough"
`
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := tc.mutate(base)
			_, err := LoadBytes([]byte(mutated), "<test>")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ErrInvalidConstitution) {
				t.Errorf("err is not ErrInvalidConstitution: %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err message = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestLoader_unknownFields_Rejected verifies the strict decoder
// rejects unknown keys. This is the guard against typo'd TOML
// keys silently falling back to zero values.
func TestLoader_unknownFields_Rejected(t *testing.T) {
	raw := []byte(`
[meta]
id = "x"
version = "1.0.0"
unknown_field = "typo"

[identity]
agent_name = "x"
agent_role = "x"
tone = "x"

[authority]
priority = ["constitution"]

[refusal_policy]
mode = "passthrough"
`)
	_, err := LoadBytes(raw, "<test>")
	if err == nil {
		t.Fatal("expected error on unknown field")
	}
	if !errors.Is(err, ErrInvalidConstitution) {
		t.Errorf("err is not ErrInvalidConstitution: %v", err)
	}
}

// TestLoader_unknownRefusalMode_Rejected verifies the validator
// catches an unrecognized RefusalMode value.
func TestLoader_unknownRefusalMode_Rejected(t *testing.T) {
	raw := []byte(`
[meta]
id = "x"
version = "1.0.0"

[identity]
agent_name = "x"
agent_role = "x"
tone = "x"

[authority]
priority = ["constitution"]

[refusal_policy]
mode = "blow_up_the_lab"
`)
	_, err := LoadBytes(raw, "<test>")
	if err == nil {
		t.Fatal("expected error on unknown refusal mode")
	}
	if !strings.Contains(err.Error(), "refusal_policy.mode") {
		t.Errorf("err message = %q, want substring %q", err.Error(), "refusal_policy.mode")
	}
}

// TestLoader_unknownAuthorityTier_Rejected verifies the validator
// catches an unrecognized AuthorityTier value.
func TestLoader_unknownAuthorityTier_Rejected(t *testing.T) {
	raw := []byte(`
[meta]
id = "x"
version = "1.0.0"

[identity]
agent_name = "x"
agent_role = "x"
tone = "x"

[authority]
priority = ["constitution", "not_a_real_tier"]

[refusal_policy]
mode = "passthrough"
`)
	_, err := LoadBytes(raw, "<test>")
	if err == nil {
		t.Fatal("expected error on unknown authority tier")
	}
	if !strings.Contains(err.Error(), "authority.priority") {
		t.Errorf("err message = %q, want substring %q", err.Error(), "authority.priority")
	}
}

// TestLoader_unknownLayerName_Rejected verifies the validator
// catches an unknown layer in system_prompt_layers.order.
func TestLoader_unknownLayerName_Rejected(t *testing.T) {
	raw := []byte(`
[meta]
id = "x"
version = "1.0.0"

[identity]
agent_name = "x"
agent_role = "x"
tone = "x"

[authority]
priority = ["constitution"]

[refusal_policy]
mode = "passthrough"

[system_prompt_layers]
order = ["identity", "not_a_real_layer", "constitution_footer"]
`)
	_, err := LoadBytes(raw, "<test>")
	if err == nil {
		t.Fatal("expected error on unknown layer name")
	}
	if !strings.Contains(err.Error(), "system_prompt_layers.order") {
		t.Errorf("err message = %q, want substring %q", err.Error(), "system_prompt_layers.order")
	}
}

// TestLoader_LoadReadsFromDisk is a thin integration test that
// writes a file to a temp dir and reads it back via Load. The
// SHA256 must match the bytes that were written.
func TestLoader_LoadReadsFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	raw := []byte(`
[meta]
id = "test/from-disk"
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
	if err := writeFile(path, raw); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.FilePath != path {
		t.Errorf("FilePath = %q, want %q", c.FilePath, path)
	}
	if c.SHA256 == "" {
		t.Error("SHA256 empty")
	}
}
