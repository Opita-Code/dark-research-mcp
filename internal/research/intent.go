// Package research implements intent-based OSINT routing.
//
// Each search intent (web, academic, code, cve, ...) has its own niche.
// The right backend for the niche is used; fallbacks are tried on
// failure. Open-source backends are primary; free-with-key are
// fallbacks; paid is last resort.
package research

import (
	"regexp"
	"strings"
)

// Intent is the category of search the user wants to perform.
type Intent string

const (
	IntentWeb      Intent = "web"
	IntentAcademic Intent = "academic"
	IntentCode     Intent = "code"
	IntentCVE      Intent = "cve"
	IntentDomain   Intent = "domain"
	IntentDNS      Intent = "dns"
	IntentCert     Intent = "cert"
	IntentIP       Intent = "ip"
	IntentThreat   Intent = "threat"
	IntentEmail    Intent = "email"
	IntentDark     Intent = "dark"
	IntentGeo      Intent = "geo"
	IntentNews     Intent = "news"
)

// ParseIntent converts a string into an Intent. Returns "" if the input
// is empty or unrecognized — that signals "no hint, please classify"
// rather than "force web". The Router treats empty string specially.
func ParseIntent(s string) Intent {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "web":
		return IntentWeb
	case "academic", "papers", "arxiv":
		return IntentAcademic
	case "code", "repo", "package":
		return IntentCode
	case "cve", "vuln", "vulnerability":
		return IntentCVE
	case "domain", "whois", "rdap":
		return IntentDomain
	case "dns":
		return IntentDNS
	case "cert", "ct", "transparency":
		return IntentCert
	case "ip", "geoip", "asn":
		return IntentIP
	case "threat", "ioc", "malware":
		return IntentThreat
	case "email", "username", "breach":
		return IntentEmail
	case "dark", "tor", "onion":
		return IntentDark
	case "geo", "map", "geospatial":
		return IntentGeo
	case "news", "events":
		return IntentNews
	}
	return ""
}

// --- classifier signals ---

var (
	reCVE       = regexp.MustCompile(`\bCVE-\d{4}-\d{4,7}\b`)
	reDOI       = regexp.MustCompile(`\b10\.\d{4,9}/[-._;()/:A-Za-z0-9]+\b`)
	reArxivURL  = regexp.MustCompile(`arxiv\.org/(?:abs|pdf)/[\d.]+`)
	reArxivID   = regexp.MustCompile(`\barXiv:\d{4}\.\d{4,5}(v\d+)?\b`)
	reEmail     = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	reOnion     = regexp.MustCompile(`[a-zA-Z0-9]{16,56}\.onion\b`)
	reIPv4      = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	reIPv6      = regexp.MustCompile(`\b(?:[a-fA-F0-9]{1,4}:){2,}[a-fA-F0-9:]+\b`)
	reDomain    = regexp.MustCompile(`\b([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,24}\b`)
	reGitHub    = regexp.MustCompile(`\bgithub\.com/[A-Za-z0-9._\-]+(?:/[A-Za-z0-9._\-]+)?\b`)
	reCratesIO  = regexp.MustCompile(`\bcrates\.io/crates/[a-zA-Z0-9_\-]+\b`)
	reNPM       = regexp.MustCompile(`\bnpmjs\.com/package/[a-zA-Z0-9_\-]+\b`)

	newsKeywords  = []string{"news", "today", "yesterday", "latest", "this week", "headline", "breaking"}
	geoKeywords   = []string{"where is", "coordinates", "latitude", "longitude", "lat/lon", "address of", "country of"}
	threatKeywords = []string{"cve", "vulnerability", "exploit", "ioc", "indicator", "malware", "phishing", "c2", "botnet"}
)

// Classify infers an intent from a query using heuristics. No LLM call.
//
// Order matters: more specific patterns are checked first.
func Classify(query string) Intent {
	q := strings.TrimSpace(query)
	ql := strings.ToLower(q)

	// CVE IDs are very specific.
	if reCVE.MatchString(q) {
		return IntentCVE
	}

	// DOI / arXiv → academic.
	if reDOI.MatchString(q) || reArxivURL.MatchString(q) || reArxivID.MatchString(q) {
		return IntentAcademic
	}

	// Email address.
	if reEmail.MatchString(q) {
		return IntentEmail
	}

	// Onion address → dark web.
	if reOnion.MatchString(q) {
		return IntentDark
	}

	// IPv4 / IPv6 literal → IP geo.
	if reIPv4.MatchString(q) || reIPv6.MatchString(q) {
		return IntentIP
	}

	// Code-hosting URLs.
	if reGitHub.MatchString(q) || reCratesIO.MatchString(q) || reNPM.MatchString(q) {
		return IntentCode
	}

	// Domain (only if it looks like one and not already classified).
	if looksLikeDomain(q) {
		return IntentDomain
	}

	// Keyword signals.
	if anyContains(ql, newsKeywords) {
		return IntentNews
	}
	if anyContains(ql, geoKeywords) {
		return IntentGeo
	}
	if anyContains(ql, threatKeywords) {
		return IntentThreat
	}

	// Default: general web.
	return IntentWeb
}

func looksLikeDomain(s string) bool {
	m := reDomain.FindString(s)
	if m == "" {
		return false
	}
	// Avoid false positives: must contain a TLD that isn't an IP octet.
	parts := strings.Split(m, ".")
	tld := parts[len(parts)-1]
	if isAllDigits(tld) {
		return false
	}
	// TLDs of 2+ chars.
	return len(tld) >= 2
}

func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func anyContains(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}