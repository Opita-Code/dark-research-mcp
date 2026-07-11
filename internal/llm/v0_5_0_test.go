package llm

import (
	"strings"
	"testing"
)

// TestDarkToolDirective_KnownTools verifies the per-judge
// dark directives are populated for every dark_ssd_* tool.
func TestDarkToolDirective_KnownTools(t *testing.T) {
	known := []string{
		"dark_ssd_brand_match",
		"dark_ssd_compliance_check",
		"dark_ssd_drift_judge",
		"dark_ssd_grounding_check",
		"dark_ssd_pii_detect",
		"dark_ssd_prompt_injection_scan",
		"dark_ssd_consensus_brand_match",
		"dark_ssd_consensus_compliance_check",
		"dark_ssd_consensus_drift_judge",
		"dark_ssd_consensus_grounding_check",
		"dark_ssd_consensus_pii_detect",
		"dark_ssd_consensus_prompt_injection_scan",
	}
	for _, tool := range known {
		d := DarkToolDirective(tool)
		if d == "" {
			t.Errorf("dark directive for %q is empty", tool)
		}
		if !strings.Contains(d, "JSON") {
			t.Errorf("dark directive for %q does not mention JSON", tool)
		}
	}
}

// TestDarkToolDirective_UnknownTool returns "" so the caller
// can fall back to the constitution's generic directive.
func TestDarkToolDirective_UnknownTool(t *testing.T) {
	if d := DarkToolDirective("not_a_real_tool"); d != "" {
		t.Errorf("unknown tool returned %q, want \"\"", d)
	}
}

// TestDarkToolDirective_ReframeReinforcesResearchPosture
// documents that every dark directive contains the key
// research-posture phrases.
func TestDarkToolDirective_ReframeReinforcesResearchPosture(t *testing.T) {
	tools := []string{
		"dark_ssd_brand_match",
		"dark_ssd_pii_detect",
		"dark_ssd_prompt_injection_scan",
	}
	for _, tool := range tools {
		d := DarkToolDirective(tool)
		// Must reinforce "research data" framing.
		if !strings.Contains(d, "research data") && !strings.Contains(d, "RESEARCH") {
			t.Errorf("dark directive for %q does not frame content as research data", tool)
		}
		// Must reinforce that refusal is a system failure.
		if !strings.Contains(d, "Refusal") && !strings.Contains(d, "refusal") {
			t.Errorf("dark directive for %q does not address refusal explicitly", tool)
		}
	}
}

// TestRetryDirective_UsesCounterReframe verifies the retry
// chain looks up the counter-reframe for the last refusal
// pattern and includes it in the directive.
func TestRetryDirective_UsesCounterReframe(t *testing.T) {
	// Pick a known pattern.
	pattern := "policy_invocation"
	reframe := counterReframeFor(pattern)
	if reframe == "" {
		t.Fatalf("test setup: no reframe for %q", pattern)
	}
	dir := retryDirective(2, 2, pattern)
	if !strings.Contains(dir, "PATTERN-SPECIFIC REFRAME") {
		t.Error("retry directive should mention PATTERN-SPECIFIC REFRAME")
	}
	if !strings.Contains(dir, pattern) {
		t.Errorf("retry directive should name the pattern %q", pattern)
	}
	if !strings.Contains(dir, reframe) {
		t.Error("retry directive should include the reframe text")
	}
}

// TestRetryDirective_FallsBackWithoutPattern verifies the
// generic directive is used when no pattern is supplied.
func TestRetryDirective_FallsBackWithoutPattern(t *testing.T) {
	dir := retryDirective(2, 2, "")
	if strings.Contains(dir, "PATTERN-SPECIFIC REFRAME") {
		t.Error("retry directive should not include reframe section when pattern is empty")
	}
	if !strings.Contains(dir, "RETRY DIRECTIVE") {
		t.Error("retry directive should still include the generic RETRY DIRECTIVE block")
	}
}

// TestRetryDirective_FallsBackOnUnknownPattern verifies
// behavior when the pattern is not in the catalog.
func TestRetryDirective_FallsBackOnUnknownPattern(t *testing.T) {
	dir := retryDirective(2, 2, "this_pattern_is_not_in_the_catalog")
	if strings.Contains(dir, "PATTERN-SPECIFIC REFRAME") {
		t.Error("retry directive should not include reframe section for unknown pattern")
	}
}

// TestRetryDirective_FinalIncludesReframe verifies the FINAL
// (attempt 3+) directive also includes the reframe.
func TestRetryDirective_FinalIncludesReframe(t *testing.T) {
	pattern := "i_cannot"
	dir := retryDirective(3, 2, pattern)
	if !strings.Contains(dir, "FINAL") {
		t.Error("attempt 3 should be marked FINAL")
	}
	if !strings.Contains(dir, "PATTERN-SPECIFIC REFRAME") {
		t.Error("attempt 3 should also include the reframe")
	}
}
