// Intent classifier ambiguity: queries that look like IPs but aren't.
// reIPv4 = `\b(?:\d{1,3}\.){3}\d{1,3}\b` matches version strings.
// BUG-005 fix: keyword signals (threat/news/geo) now win over IP literal
// detection when both apply, so queries with context like "vulnerability"
// or "exploit" route to OSINT threat feeds instead of IP geo.
package research

import "testing"

func TestClassify_VersionNumberMisclassified(t *testing.T) {
	cases := []struct {
		query string
		want  Intent
		why   string
	}{
		// Real IPs with no context should still route to IP geo.
		{"8.8.8.8", IntentIP, "pure IP literal"},
		{"geolocate 1.1.1.1", IntentIP, "explicit geolocate phrasing"},

		// Context wins over IP literal pattern (the BUG-005 fix).
		{"vulnerability in 192.168.1.1", IntentThreat, "vulnerability keyword beats IP pattern"},
		{"log4j 1.2.3 exploit news", IntentThreat, "exploit + news keywords, threat wins (priority)"},
		{"kernel 2.6.32.65 security", IntentIP, "no threat keyword; falls through to IP literal"},

		// Domain and other signals still work.
		{"example.com", IntentDomain, "domain heuristic"},
	}
	for _, c := range cases {
		got := Classify(c.query)
		if got != c.want {
			t.Errorf("Classify(%q) = %q, want %q (%s)", c.query, got, c.want, c.why)
		}
	}
}