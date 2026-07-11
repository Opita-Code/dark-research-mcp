package research

import "testing"

func TestClassify_cve(t *testing.T) {
	cases := []string{
		"CVE-2024-1234",
		"is CVE-2024-12345 patched?",
		"details on CVE-2023-999999",
	}
	for _, q := range cases {
		if got := Classify(q); got != IntentCVE {
			t.Errorf("Classify(%q) = %v, want %v", q, got, IntentCVE)
		}
	}
}

func TestClassify_academic(t *testing.T) {
	cases := []string{
		"arxiv.org/abs/2401.12345",
		"10.1038/nature12373",
		"arXiv:2401.12345",
		"doi:10.1145/123456",
	}
	for _, q := range cases {
		if got := Classify(q); got != IntentAcademic {
			t.Errorf("Classify(%q) = %v, want %v", q, got, IntentAcademic)
		}
	}
}

func TestClassify_email(t *testing.T) {
	cases := []string{
		"alice@example.com",
		"check breaches for john.doe@foo.io",
	}
	for _, q := range cases {
		if got := Classify(q); got != IntentEmail {
			t.Errorf("Classify(%q) = %v, want %v", q, got, IntentEmail)
		}
	}
}

func TestClassify_dark(t *testing.T) {
	// v2 onions are 16 chars; v3 are 56. The classifier's regex requires
	// 16+ to reduce false positives on short hostnames like "x.onion".
	cases := []string{
		"3g2upl4pq6kufc4m.onion",
		"http://3g2upl4pq6kufc4m.onion/path",
	}
	for _, q := range cases {
		if got := Classify(q); got != IntentDark {
			t.Errorf("Classify(%q) = %v, want %v", q, got, IntentDark)
		}
	}
}

func TestClassify_ip(t *testing.T) {
	cases := []string{
		"8.8.8.8",
		"geolocate 1.1.1.1",
		"2001:4860:4860::8888",
	}
	for _, q := range cases {
		if got := Classify(q); got != IntentIP {
			t.Errorf("Classify(%q) = %v, want %v", q, got, IntentIP)
		}
	}
}

func TestClassify_code(t *testing.T) {
	cases := []string{
		"github.com/charmbracelet/bubbletea",
		"crates.io/crates/serde",
		"npmjs.com/package/lodash",
	}
	for _, q := range cases {
		if got := Classify(q); got != IntentCode {
			t.Errorf("Classify(%q) = %v, want %v", q, got, IntentCode)
		}
	}
}

func TestClassify_domain(t *testing.T) {
	cases := []string{
		"example.com",
		"whois github.com",
	}
	for _, q := range cases {
		if got := Classify(q); got != IntentDomain {
			t.Errorf("Classify(%q) = %v, want %v", q, got, IntentDomain)
		}
	}
}

func TestClassify_news(t *testing.T) {
	if got := Classify("latest news about climate"); got != IntentNews {
		t.Errorf("Classify(news query) = %v, want %v", got, IntentNews)
	}
}

func TestClassify_threat(t *testing.T) {
	if got := Classify("exploit for Apache"); got != IntentThreat {
		t.Errorf("Classify(exploit query) = %v, want %v", got, IntentThreat)
	}
}

func TestClassify_geo(t *testing.T) {
	if got := Classify("where is Tokyo"); got != IntentGeo {
		t.Errorf("Classify(geo query) = %v, want %v", got, IntentGeo)
	}
}

func TestClassify_default(t *testing.T) {
	queries := []string{
		"how to bake bread",
		"best practices for go modules",
	}
	for _, q := range queries {
		if got := Classify(q); got != IntentWeb {
			t.Errorf("Classify(%q) = %v, want %v (default)", q, got, IntentWeb)
		}
	}
}

func TestParseIntent(t *testing.T) {
	cases := map[string]Intent{
		"web":       IntentWeb,
		"WEB":       IntentWeb,
		"papers":    IntentAcademic,
		"vuln":      IntentCVE,
		"unknown":   "",
		"":          "",
		"  code  ":  IntentCode,
	}
	for in, want := range cases {
		if got := ParseIntent(in); got != want {
			t.Errorf("ParseIntent(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestRegistry_has_all_intents(t *testing.T) {
	r := DefaultRegistry()
	for _, intent := range []Intent{
		IntentWeb, IntentAcademic, IntentCode, IntentCVE,
		IntentDomain, IntentDNS, IntentCert, IntentIP,
		IntentThreat, IntentEmail, IntentDark, IntentGeo, IntentNews,
	} {
		if len(r.For(intent)) == 0 {
			t.Errorf("registry missing backends for intent %q", intent)
		}
	}
}

func TestRegistry_orders_by_weight(t *testing.T) {
	r := DefaultRegistry()
	for _, intent := range []Intent{IntentWeb, IntentAcademic, IntentCode, IntentCVE, IntentIP} {
		bs := r.For(intent)
		for i := 1; i < len(bs); i++ {
			if bs[i].Weight < bs[i-1].Weight {
				t.Errorf("intent %q: weight not ascending at index %d: %d < %d",
					intent, i, bs[i].Weight, bs[i-1].Weight)
			}
		}
	}
}

func TestRegistry_primary_open_source(t *testing.T) {
	// The first backend for each intent must be Free+OpenSource.
	r := DefaultRegistry()
	for _, intent := range []Intent{
		IntentWeb, IntentAcademic, IntentCode, IntentCVE,
		IntentDomain, IntentDNS, IntentCert, IntentIP,
		IntentThreat, IntentDark, IntentGeo, IntentNews,
	} {
		bs := r.For(intent)
		if len(bs) == 0 {
			continue
		}
		primary := bs[0]
		if !(primary.Free && primary.OpenSource) {
			t.Errorf("intent %q: primary backend %q is not Free+OpenSource (free=%v oss=%v)",
				intent, primary.Name, primary.Free, primary.OpenSource)
		}
	}
}