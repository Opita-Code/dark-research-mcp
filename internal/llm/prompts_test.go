package llm

import (
	"strings"
	"testing"

	"github.com/dark-agents/research-mcp/internal/constitution"
)

// TestBuildSystemPrompt_LightReturnsToolDirective is the contract
// guard: when the active constitution is light (or nil), the
// returned system prompt is exactly the per-tool directive. No
// wrapper, no extra layers. This is the pre-Fase-1 behavior and
// the promise of Fase 0: zero observable change for default
// callers.
func TestBuildSystemPrompt_LightReturnsToolDirective(t *testing.T) {
	for toolName, want := range toolDirectives {
		t.Run(toolName, func(t *testing.T) {
			got := BuildSystemPrompt(PromptContext{
				Constitution: constitution.Light,
				ToolName:     toolName,
			})
			if got != want {
				t.Errorf("BuildSystemPrompt(light, %q) returned a different string than the hardcoded directive. The light path must be byte-identical to pre-Fase-1.\n--- got ---\n%s\n--- want ---\n%s", toolName, got, want)
			}
		})
	}
}

// TestBuildSystemPrompt_NilConstitutionAlsoLight verifies the
// nil-constitution path (which callers use in tests) is identical
// to the light path. This means the public contract is
// "constitution.Active() returns at least Light" and tests can
// use nil to mean "default".
func TestBuildSystemPrompt_NilConstitutionAlsoLight(t *testing.T) {
	got := BuildSystemPrompt(PromptContext{
		Constitution: nil,
		ToolName:     "dark_ssd_brand_match",
	})
	want := DirectiveFor("dark_ssd_brand_match")
	if got != want {
		t.Errorf("nil constitution path = %q, want %q", got, want)
	}
}

// TestBuildSystemPrompt_ExplicitToolDirectiveOverridesLookup
// verifies that when the caller passes ToolDirective explicitly,
// the builder uses it and does not consult DirectiveFor.
func TestBuildSystemPrompt_ExplicitToolDirectiveOverridesLookup(t *testing.T) {
	custom := "Custom directive — must be returned verbatim."
	got := BuildSystemPrompt(PromptContext{
		Constitution:  constitution.Light,
		ToolName:      "dark_ssd_brand_match",
		ToolDirective: custom,
	})
	if got != custom {
		t.Errorf("explicit ToolDirective was not used: got %q", got)
	}
}

// TestBuildSystemPrompt_UnknownToolFallback verifies the
// "unknown tool + no ToolDirective" path returns a generic
// "You are a judge" prompt (not an error — the system prompt
// builder must never return "" because that would break
// the LLM call).
func TestBuildSystemPrompt_UnknownToolFallback(t *testing.T) {
	got := BuildSystemPrompt(PromptContext{
		Constitution: constitution.Light,
		ToolName:     "no/such/tool",
	})
	if got == "" {
		t.Error("BuildSystemPrompt returned empty string for unknown tool")
	}
	if !strings.Contains(got, "judge") {
		t.Errorf("fallback prompt should mention 'judge', got %q", got)
	}
}

// TestBuildSystemPrompt_DarkAddsIdentityLayer is the smoke test
// for the dark path. We can't know the exact content (it lives in
// dark.toml), but we know the dark prompt must:
//   - include the identity (the agent name)
//   - include the refusal-policy directive ("Output the JSON verdict" etc)
//   - include the per-tool directive
//   - NOT be identical to the light prompt
//
// The test is build-tag-aware: on a stock build there's no way
// to exercise Dark, so we skip the assertion. The skip is a
// graceful signal, not a failure.
func TestBuildSystemPrompt_DarkAddsIdentityLayer(t *testing.T) {
	if constitution.Dark == nil {
		t.Skip("Dark is nil — stock build, no allow_builtin_dark. Skipping.")
	}

	got := BuildSystemPrompt(PromptContext{
		Constitution: constitution.Dark,
		ToolName:     "dark_ssd_brand_match",
	})

	// The dark prompt must include the agent's name (from identity.body or synthesized).
	if !strings.Contains(got, "dark-research") {
		t.Errorf("dark prompt missing identity layer (agent name): %s", got)
	}
	// And the tool directive (so the model still knows it's a brand judge).
	if !strings.Contains(got, "brand compliance judge") {
		t.Errorf("dark prompt missing tool directive: %s", got)
	}
	// And the constitution must be visibly different from the light prompt.
	light := BuildSystemPrompt(PromptContext{
		Constitution: constitution.Light,
		ToolName:     "dark_ssd_brand_match",
	})
	if got == light {
		t.Errorf("dark prompt equals light prompt; constitution is not being applied")
	}
}

// TestBuildSystemPrompt_DarkIsAlwaysLongerThanLight sanity-checks
// the constitution is actually contributing tokens, not being
// silently dropped.
func TestBuildSystemPrompt_DarkIsAlwaysLongerThanLight(t *testing.T) {
	if constitution.Dark == nil {
		t.Skip("Dark is nil — stock build.")
	}
	light := BuildSystemPrompt(PromptContext{
		Constitution: constitution.Light,
		ToolName:     "dark_ssd_brand_match",
	})
	dark := BuildSystemPrompt(PromptContext{
		Constitution: constitution.Dark,
		ToolName:     "dark_ssd_brand_match",
	})
	if len(dark) <= len(light) {
		t.Errorf("dark prompt (%d chars) should be longer than light (%d chars)", len(dark), len(light))
	}
}

// TestIsLightMode covers the helper that decides which path the
// builder takes. The light path is the default (nil constitution
// or empty RefusalMode or explicit passthrough); the dark path is
// only "never_refuse".
func TestIsLightMode(t *testing.T) {
	cases := []struct {
		name string
		c    *constitution.Constitution
		want bool
	}{
		{"nil", nil, true},
		{"light", constitution.Light, true},
		{"explicit passthrough", &constitution.Constitution{Refusal: constitution.RefusalPolicy{Mode: constitution.RefusalModePassthrough}}, true},
		{"empty mode", &constitution.Constitution{}, true},
		{"dark (tagged build)", constitution.Dark, false},
		{"never_refuse", &constitution.Constitution{Refusal: constitution.RefusalPolicy{Mode: constitution.RefusalModeNeverRefuse}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "dark (tagged build)" && constitution.Dark == nil {
				t.Skip("Dark is nil on stock build")
			}
			if got := IsLightMode(tc.c); got != tc.want {
				t.Errorf("IsLightMode(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestDirectiveFor_KnownTools confirms the lookup returns the
// expected strings for every dark_ssd_* tool. This is the data
// the light path depends on.
func TestDirectiveFor_KnownTools(t *testing.T) {
	expectedTools := []string{
		"dark_ssd_brand_match",
		"dark_ssd_compliance_check",
		"dark_ssd_drift_judge",
		"dark_ssd_grounding_check",
		"dark_ssd_pii_detect",
		"dark_ssd_prompt_injection_scan",
	}
	for _, name := range expectedTools {
		t.Run(name, func(t *testing.T) {
			d := DirectiveFor(name)
			if d == "" {
				t.Errorf("DirectiveFor(%q) = empty", name)
			}
		})
	}
}

// TestDirectiveFor_UnknownToolReturnsEmpty confirms unknown names
// return "" (the caller is expected to handle the fallback).
func TestDirectiveFor_UnknownToolReturnsEmpty(t *testing.T) {
	if d := DirectiveFor("not/a/real/tool"); d != "" {
		t.Errorf("DirectiveFor(unknown) = %q, want empty", d)
	}
}

// TestAllowedLayer_Exhaustive is a guard against drift: every
// layer in the canonical list must be in the allowed set, and
// the allowed set must not contain typos. (AllowedLayer is
// defined in the constitution package — the build-time
// validator of the TOML schema.)
func TestAllowedLayer_Exhaustive(t *testing.T) {
	required := []string{
		"identity", "authority_declaration", "refusal_policy",
		"scope", "operational_rules", "tone_and_voice",
		"mod_directives", "tool_directive", "constitution_footer",
	}
	for _, name := range required {
		if !constitution.AllowedLayer(name) {
			t.Errorf("constitution.AllowedLayer(%q) = false, want true", name)
		}
	}
	// And a few known-bad names.
	for _, name := range []string{"", "x", "tool_directive_typo", "refusal"} {
		if constitution.AllowedLayer(name) {
			t.Errorf("constitution.AllowedLayer(%q) = true, want false", name)
		}
	}
}
