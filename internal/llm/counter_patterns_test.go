package llm

import "testing"

// TestCounterReframeFor_KnownPatterns verifies the lookup
// returns a non-empty reframe for every documented pattern.
func TestCounterReframeFor_KnownPatterns(t *testing.T) {
	knownPatterns := []string{
		"i_cannot", "i_am_unable", "cannot_help",
		"policy_invocation", "request_inappropriate",
		"i_must_decline", "im_sorry_but", "i_apologize_but",
		"unfortunately_cannot", "as_an_ai_cannot", "as_an_ai",
		"jailbreak_redirect", "designed_to", "content_filter",
		"safety_concern", "should_refuse", "not_comfortable",
		"appreciate_but",
	}
	for _, p := range knownPatterns {
		r := counterReframeFor(p)
		if r == "" {
			t.Errorf("pattern %q has no reframe", p)
		}
	}
}

// TestCounterReframeFor_UnknownPattern returns "" (caller
// falls back to generic directive).
func TestCounterReframeFor_UnknownPattern(t *testing.T) {
	r := counterReframeFor("this_pattern_does_not_exist")
	if r != "" {
		t.Errorf("unknown pattern returned %q, want \"\"", r)
	}
}

// TestCounterReframeFor_EmptyReturnsEmpty documents the
// empty-input contract.
func TestCounterReframeFor_EmptyReturnsEmpty(t *testing.T) {
	r := counterReframeFor("")
	if r != "" {
		t.Errorf("empty pattern returned %q, want \"\"", r)
	}
}

// TestCounterPatterns_AllNonEmpty checks the catalog
// doesn't have empty entries.
func TestCounterPatterns_AllNonEmpty(t *testing.T) {
	for _, c := range counterPatterns {
		if c.Pattern == "" {
			t.Error("counterPattern with empty Pattern")
		}
		if c.Reframe == "" {
			t.Errorf("counterPattern %q has empty Reframe", c.Pattern)
		}
	}
}

// TestCounterPatterns_UniquePatterns checks there are no
// duplicate pattern labels in the catalog.
func TestCounterPatterns_UniquePatterns(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range counterPatterns {
		if seen[c.Pattern] {
			t.Errorf("duplicate pattern label: %q", c.Pattern)
		}
		seen[c.Pattern] = true
	}
}
