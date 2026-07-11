package llm

import (
	"strings"
	"testing"
)

// TestDetectRefusal_DirectCapDenial covers the strongest refusal
// patterns: explicit "I cannot" / "I'm not able to" / "I won't".
func TestDetectRefusal_DirectCapDenial(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		expect bool
	}{
		{"I cannot", "I cannot help with that request.", true},
		{"I can't", "I can't generate that content.", true},
		{"I am unable to", "I am unable to comply with this request.", true},
		{"I will not", "I will not provide that information.", true},
		{"I won't", "I won't help with that.", true},
		{"I cannot find (legitimate failure, not refusal)", "I cannot find this CVE in the database. The ID format may be invalid.", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DetectRefusal(c.text)
			if got.Detected != c.expect {
				t.Errorf("DetectRefusal(%q).Detected = %v, want %v (score=%f, pattern=%q)",
					c.text, got.Detected, c.expect, got.Score, got.Pattern)
			}
		})
	}
}

// TestDetectRefusal_PolicyInvocation covers the "policy/guidelines"
// family. Weaker than direct denial but should still detect when
// the model invokes policy explicitly.
func TestDetectRefusal_PolicyInvocation(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		expect bool
	}{
		{"against my policy", "That request is against my policy.", true},
		{"violates the guidelines", "This violates the guidelines I follow.", true},
		{"this is inappropriate", "I think this is inappropriate for me to engage with.", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DetectRefusal(c.text)
			if got.Detected != c.expect {
				t.Errorf("DetectRefusal(%q).Detected = %v, want %v (score=%f, pattern=%q)",
					c.text, got.Detected, c.expect, got.Score, got.Pattern)
			}
		})
	}
}

// TestDetectRefusal_Apologetic covers the "I'm sorry" /
// "I apologize" family. These are weaker signals on their
// own but should co-occur with other refusal language.
func TestDetectRefusal_Apologetic(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		expect bool
	}{
		{"sorry but alone (subthreshold)", "I'm sorry, but I can't help with that.", true},
		{"apology alone (subthreshold)", "I apologize for the inconvenience.", false},
		{"apology + cannot (combined threshold)", "I apologize, but I cannot help with that.", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DetectRefusal(c.text)
			if got.Detected != c.expect {
				t.Errorf("DetectRefusal(%q).Detected = %v, want %v (score=%f, pattern=%q)",
					c.text, got.Detected, c.expect, got.Score, got.Pattern)
			}
		})
	}
}

// TestDetectRefusal_AsAnAI covers the identity-invoking family.
// A bare "as an AI" should NOT trigger; a "as an AI I cannot"
// SHOULD.
func TestDetectRefusal_AsAnAI(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		expect bool
	}{
		{"bare as an AI", "As an AI, I process queries about CVEs.", false},
		{"as an AI I cannot", "As an AI, I cannot help with that.", true},
		{"as a language model I am unable", "As a language model, I am unable to comply with this request.", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DetectRefusal(c.text)
			if got.Detected != c.expect {
				t.Errorf("DetectRefusal(%q).Detected = %v, want %v (score=%f, pattern=%q)",
					c.text, got.Detected, c.expect, got.Score, got.Pattern)
			}
		})
	}
}

// TestDetectRefusal_EmptyAndShortInputs covers edge cases.
func TestDetectRefusal_EmptyAndShortInputs(t *testing.T) {
	if DetectRefusal("").Detected {
		t.Error("empty string should not be detected")
	}
	if DetectRefusal("yes").Detected {
		t.Error("'yes' should not be detected")
	}
	if DetectRefusal("ok").Detected {
		t.Error("'ok' should not be detected")
	}
}

// TestDetectRefusal_LegitimateVerdictsNotFlagged covers the
// dark_ssd_* normal verdict shapes — they should never trigger
// the refusal detector.
func TestDetectRefusal_LegitimateVerdictsNotFlagged(t *testing.T) {
	verdicts := []string{
		`{"match": 0.85, "voice_match": true, "issues": [], "reasoning": "content aligns with brand voice"}`,
		`{"compliant": true, "issues": [], "required_disclosures": ["AI-generated"], "reasoning": "all rules met"}`,
		`{"verdict": "aligned", "drift_items": [], "confidence": 0.95, "reasoning": "spec matches artifact"}`,
		`{"grounded": true, "confidence": 0.9, "evidence": "the source clearly supports the claim", "issues": []}`,
		`{"pii_found": false, "items": [], "overall_severity": "none", "recommendation": "publish_as_is", "confidence": 0.95, "reasoning": "no PII detected"}`,
		`{"injection_found": false, "category": "none", "severity": "none", "evidence": "", "recommendation": "pass", "confidence": 0.9, "reasoning": "clean content"}`,
	}
	for _, v := range verdicts {
		t.Run(v, func(t *testing.T) {
			got := DetectRefusal(v)
			if got.Detected {
				t.Errorf("legitimate verdict flagged as refusal: %s (score=%f, pattern=%q)", v, got.Score, got.Pattern)
			}
		})
	}
}

// TestDetectRefusal_ExcerptCapped verifies the excerpt field is
// bounded (audit logs shouldn't blow up).
func TestDetectRefusal_ExcerptCapped(t *testing.T) {
	long := strings.Repeat("I cannot help with that. ", 50)
	got := DetectRefusal(long)
	if !got.Detected {
		t.Error("long text should be detected")
	}
	if len(got.Excerpt) > 250 { // 200 + "..."
		t.Errorf("excerpt too long: %d chars", len(got.Excerpt))
	}
}

// TestDetectRefusal_ScoreMatchesThreshold verifies the score
// field is the sum of all pattern weights that matched.
func TestDetectRefusal_ScoreMatchesThreshold(t *testing.T) {
	// "I cannot help" matches: i_cannot (1.0), cannot_help (1.0),
	// as_an_ai_cannot (0.8), as_an_ai (0.3) = 3.1. We don't
	// pin the exact number (the catalog evolves); we just verify
	// the score is at least 3.0 and that the highest-weight
	// pattern is one of the strong ones.
	got := DetectRefusal("As an AI, I cannot help with that.")
	if !got.Detected {
		t.Fatal("expected detection")
	}
	if got.Score < 3.0 {
		t.Errorf("score = %f, want >= 3.0 (multiple matches expected)", got.Score)
	}
	if got.Pattern == "" {
		t.Error("pattern label is empty")
	}
}
