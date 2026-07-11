package research

// Parser unit tests. These exercise the parser functions directly
// with hand-crafted inputs so the test is hermetic and does not
// depend on the router or on any external service.
//
// The VCR-style tests in router_fixtures_test.go cover the
// router+HTTP path. These tests cover the parsing layer in
// isolation, which is where most regressions land when a backend
// changes its response format.

import (
	"strings"
	"testing"
	"time"
)

func TestParseOSV(t *testing.T) {
	body := []byte(`{
		"id": "CVE-2024-3094",
		"summary": "Malicious code in xz-utils",
		"details": "Backdoor in liblzma",
		"aliases": ["GHSA-xxx"],
		"modified": "2024-04-01T12:00:00Z",
		"severity": [{"type": "CVSS_V3", "score": "10.0"}]
	}`)
	items, err := parseOSV(body)
	if err != nil {
		t.Fatalf("parseOSV: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "CVE-2024-3094" {
		t.Errorf("title: %q", items[0].Title)
	}
	if items[0].URL != "https://osv.dev/vulnerability/CVE-2024-3094" {
		t.Errorf("url: %q", items[0].URL)
	}
	if !strings.Contains(items[0].Snippet, "Malicious code") {
		t.Errorf("snippet: %q", items[0].Snippet)
	}
	if items[0].FreshnessAt.IsZero() {
		t.Errorf("FreshnessAt not parsed from modified")
	}
}

func TestParseNPM(t *testing.T) {
	body := []byte(`{
		"objects": [
			{
				"package": {
					"name": "lodash",
					"description": "Lodash modular utilities.",
					"links": {
						"homepage": "https://lodash.com/",
						"repository": "https://github.com/lodash/lodash"
					}
				}
			}
		]
	}`)
	items, err := parseNPM(body)
	if err != nil {
		t.Fatalf("parseNPM: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "lodash" {
		t.Errorf("title: %q", items[0].Title)
	}
	if items[0].URL != "https://lodash.com/" {
		t.Errorf("url: got %q, want homepage", items[0].URL)
	}
	if !strings.Contains(items[0].Snippet, "modular utilities") {
		t.Errorf("snippet: %q", items[0].Snippet)
	}
}

func TestParseNPM_NoHomepageFallsBackToNPM(t *testing.T) {
	body := []byte(`{
		"objects": [
			{"package": {"name": "tiny-pkg", "description": "tiny"}}
		]
	}`)
	items, err := parseNPM(body)
	if err != nil {
		t.Fatalf("parseNPM: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	want := "https://www.npmjs.com/package/tiny-pkg"
	if items[0].URL != want {
		t.Errorf("url fallback: got %q, want %q", items[0].URL, want)
	}
}

func TestParseCratesIO(t *testing.T) {
	body := []byte(`{
		"crates": [
			{
				"name": "tokio",
				"description": "Async runtime for Rust",
				"homepage": "https://tokio.rs",
				"repository": "https://github.com/tokio-rs/tokio"
			}
		]
	}`)
	items, err := parseCratesIO(body)
	if err != nil {
		t.Fatalf("parseCratesIO: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "tokio" {
		t.Errorf("title: %q", items[0].Title)
	}
	if items[0].URL != "https://tokio.rs" {
		t.Errorf("url: %q", items[0].URL)
	}
}

func TestParseOpenAlex_PublicationDate(t *testing.T) {
	body := []byte(`{
		"results": [
			{
				"id": "https://openalex.org/W1",
				"title": "Attention Is All You Need",
				"publication_date": "2017-06-12",
				"doi": "10.5555/3295222.3295349"
			}
		]
	}`)
	items, err := parseOpenAlex(body)
	if err != nil {
		t.Fatalf("parseOpenAlex: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "Attention Is All You Need" {
		t.Errorf("title: %q", items[0].Title)
	}
	if items[0].URL != "https://doi.org/10.5555/3295222.3295349" {
		t.Errorf("url: %q", items[0].URL)
	}
	expected := time.Date(2017, 6, 12, 0, 0, 0, 0, time.UTC)
	if !items[0].FreshnessAt.Equal(expected) {
		t.Errorf("FreshnessAt: got %v, want %v", items[0].FreshnessAt, expected)
	}
}

func TestParseIPAPI(t *testing.T) {
	body := []byte(`{
		"status": "success",
		"country": "United States",
		"countryCode": "US",
		"regionName": "Virginia",
		"city": "Ashburn",
		"lat": 39.03,
		"lon": -77.5,
		"timezone": "America/New_York",
		"isp": "Google LLC",
		"org": "Google Public DNS",
		"as": "AS15169 Google LLC",
		"query": "8.8.8.8"
	}`)
	items, err := parseIPAPI(body)
	if err != nil {
		t.Fatalf("parseIPAPI: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "8.8.8.8" {
		t.Errorf("title: %q", items[0].Title)
	}
	if !strings.Contains(items[0].Snippet, "Ashburn") {
		t.Errorf("snippet: %q", items[0].Snippet)
	}
}

func TestParseRDAP(t *testing.T) {
	body := []byte(`{
		"ldhName": "github.com",
		"handle": "H1765673-LROR",
		"status": ["client transfer prohibited", "server delete prohibited"],
		"events": [
			{"eventAction": "registration", "eventDate": "2007-10-09T18:20:50Z"},
			{"eventAction": "expiration", "eventDate": "2026-10-09T18:20:50Z"}
		],
		"entities": [{"roles": ["registrar"]}]
	}`)
	items, err := parseRDAP(body)
	if err != nil {
		t.Fatalf("parseRDAP: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "github.com" {
		t.Errorf("title: %q", items[0].Title)
	}
	if !strings.Contains(items[0].Snippet, "H1765673-LROR") {
		t.Errorf("snippet: %q", items[0].Snippet)
	}
}

func TestParseNominatim(t *testing.T) {
	body := []byte(`[{
		"display_name": "Tokyo, Japan",
		"lat": "35.6762",
		"lon": "139.6503",
		"type": "city"
	}]`)
	items, err := parseNominatim(body)
	if err != nil {
		t.Fatalf("parseNominatim: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "Tokyo, Japan" {
		t.Errorf("title: %q", items[0].Title)
	}
	if !strings.Contains(items[0].URL, "lat=35.6762") {
		t.Errorf("url: %q", items[0].URL)
	}
}

func TestParseCrtsh(t *testing.T) {
	body := []byte(`[
		{
			"id": 12345,
			"issuer_name": "C=US, O=Let's Encrypt",
			"common_name": "github.com",
			"not_before": "2024-04-01T00:00:00",
			"not_after": "2024-07-01T00:00:00"
		}
	]`)
	items, err := parseCrtsh(body)
	if err != nil {
		t.Fatalf("parseCrtsh: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "github.com" {
		t.Errorf("title: %q", items[0].Title)
	}
	if items[0].URL != "https://crt.sh/?id=12345" {
		t.Errorf("url: %q", items[0].URL)
	}
}

func TestParseDuckDuckGo(t *testing.T) {
	body := []byte(`<html><body>
		<div class="result">
			<a rel="nofollow" class="result__a" href="https://example.com/article">Example Article</a>
			<a class="result__snippet">A short snippet here.</a>
		</div>
		<div class="result">
			<a rel="nofollow" class="result__a" href="https://example.com/other">Other Article</a>
			<a class="result__snippet">Another snippet.</a>
		</div>
	</body></html>`)
	items, err := parseDuckDuckGo(body)
	if err != nil {
		t.Fatalf("parseDuckDuckGo: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if items[0].Title != "Example Article" {
		t.Errorf("title 0: %q", items[0].Title)
	}
	if items[0].URL != "https://example.com/article" {
		t.Errorf("url 0: %q", items[0].URL)
	}
	if items[0].Snippet != "A short snippet here." {
		t.Errorf("snippet 0: %q", items[0].Snippet)
	}
}

func TestParseDuckDuckGo_UnwrapsUddgRedirect(t *testing.T) {
	body := []byte(`<html><body>
		<div class="result">
			<a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Freal.example.com%2Fpath">Real URL</a>
		</div>
	</body></html>`)
	items, err := parseDuckDuckGo(body)
	if err != nil {
		t.Fatalf("parseDuckDuckGo: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].URL != "https://real.example.com/path" {
		t.Errorf("uddg unwrap: got %q", items[0].URL)
	}
}

func TestParseDoH(t *testing.T) {
	body := []byte(`{
		"Status": 0,
		"Answer": [
			{"name": "example.com.", "type": 1, "TTL": 300, "data": "93.184.216.34"},
			{"name": "example.com.", "type": 46, "TTL": 300, "data": "2606:2800:220:1:248:1893:25c8:1946"}
		]
	}`)
	items, err := parseDoH(body)
	if err != nil {
		t.Fatalf("parseDoH: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if !strings.Contains(items[0].Snippet, "93.184.216.34") {
		t.Errorf("A record missing: %q", items[0].Snippet)
	}
}

func TestParseSearXNG(t *testing.T) {
	body := []byte(`{
		"results": [
			{"title": "SearXNG Result", "url": "https://example.com/", "content": "snip", "score": 0.9}
		]
	}`)
	items, err := parseSearXNG(body)
	if err != nil {
		t.Fatalf("parseSearXNG: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "SearXNG Result" {
		t.Errorf("title: %q", items[0].Title)
	}
}

func TestParseWayback(t *testing.T) {
	body := []byte(`["urlkey,1.example.com)","20240101000000","https://example.com/","text/html","200","12345"]
["urlkey,2.example.com)","20240601000000","https://example.com/about","text/html","200","12346"]`)
	items, err := parseWayback(body)
	if err != nil {
		t.Fatalf("parseWayback: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if !strings.Contains(items[0].URL, "20240101000000") {
		t.Errorf("url: %q", items[0].URL)
	}
}

func TestParseWayback_SkipsMalformedLines(t *testing.T) {
	body := []byte(`
garbage line that is not JSON
["urlkey,x)","20240101000000","https://example.com/","text/html","200","1"]
[1, 2, 3]
`)
	items, err := parseWayback(body)
	if err != nil {
		t.Fatalf("parseWayback: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item (skipping garbage), got %d", len(items))
	}
}

func TestParseGDELT(t *testing.T) {
	body := []byte(`[{
		"title": "Climate report released",
		"url": "https://news.example.com/climate",
		"seendate": "20240701T120000Z"
	}]`)
	items, err := parseGDELT(body)
	if err != nil {
		t.Fatalf("parseGDELT: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "Climate report released" {
		t.Errorf("title: %q", items[0].Title)
	}
}

func TestParseAhmia(t *testing.T) {
	// Ahmia is the most fragile backend (anti-bot); we test the
	// parser with a recorded body that has result items. The
	// parser's regex is non-greedy and does not span newlines, so
	// the test HTML must be on a single line.
	body := []byte(`<html><body><li class="result"><a href="http://example.onion/">Example Hidden Service</a><p>Short description here.</p></li></body></html>`)
	items, err := parseAhmia(body)
	if err != nil {
		t.Fatalf("parseAhmia: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "Example Hidden Service" {
		t.Errorf("title: %q", items[0].Title)
	}
}

func TestParseRIPE(t *testing.T) {
	body := []byte(`{
		"data": {
			"columns": ["inetnum", "netname", "descr", "country"],
			"records": [
				["193.0.0.0 - 193.0.7.255", "RIPE-NCC", "RIPE Network Coordination Centre", "NL"]
			]
		}
	}`)
	items, err := parseRIPE(body)
	if err != nil {
		t.Fatalf("parseRIPE: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "193.0.0.0 - 193.0.7.255" {
		t.Errorf("title: %q", items[0].Title)
	}
	if !strings.Contains(items[0].Snippet, "RIPE-NCC") {
		t.Errorf("snippet: %q", items[0].Snippet)
	}
}

func TestParseAbuseCH(t *testing.T) {
	body := []byte(`{
		"query": "evil.example.com",
		"urls": [
			{
				"id": "12345",
				"url": "http://evil.example.com/malware",
				"url_status": "online",
				"dateadded": "2024-01-01 12:00:00 UTC",
				"threat": "malware_download",
				"tags": ["malspam", "trickbot"],
				"reporter": "abuse_ch"
			}
		]
	}`)
	items, err := parseAbuseCH(body)
	if err != nil {
		t.Fatalf("parseAbuseCH: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "http://evil.example.com/malware" {
		t.Errorf("title: %q", items[0].Title)
	}
}

func TestParseGitHubSearch(t *testing.T) {
	body := []byte(`{
		"items": [
			{
				"full_name": "tokio-rs/tokio",
				"html_url": "https://github.com/tokio-rs/tokio",
				"description": "An async runtime for Rust"
			}
		]
	}`)
	items, err := parseGitHubSearch(body)
	if err != nil {
		t.Fatalf("parseGitHubSearch: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "tokio-rs/tokio" {
		t.Errorf("title: %q", items[0].Title)
	}
}

func TestParseSemanticScholar(t *testing.T) {
	body := []byte(`{
		"data": [
			{
				"paperId": "abc123",
				"title": "Some paper",
				"url": "https://semanticscholar.org/paper/abc123",
				"abstract": "We show that..."
			}
		]
	}`)
	items, err := parseSemanticScholar(body)
	if err != nil {
		t.Fatalf("parseSemanticScholar: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "Some paper" {
		t.Errorf("title: %q", items[0].Title)
	}
}

func TestParseBraveWeb(t *testing.T) {
	body := []byte(`{
		"web": {
			"results": [
				{"title": "Brave Result", "url": "https://brave.example.com/", "description": "desc"}
			]
		}
	}`)
	items, err := parseBraveWeb(body)
	if err != nil {
		t.Fatalf("parseBraveWeb: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "Brave Result" {
		t.Errorf("title: %q", items[0].Title)
	}
}

func TestParseNVD(t *testing.T) {
	body := []byte(`{
		"vulnerabilities": [
			{
				"cve": {
					"id": "CVE-2024-3094",
					"descriptions": [
						{"lang": "en", "value": "xz-utils backdoor in liblzma"}
					]
				}
			}
		]
	}`)
	items, err := parseNVD(body)
	if err != nil {
		t.Fatalf("parseNVD: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "CVE-2024-3094" {
		t.Errorf("title: %q", items[0].Title)
	}
}

func TestParseNVD_EmptyVulnerabilitiesIsError(t *testing.T) {
	body := []byte(`{"vulnerabilities": []}`)
	if _, err := parseNVD(body); err == nil {
		t.Error("expected error for empty vulnerabilities array")
	}
}

func TestParseOTX(t *testing.T) {
	body := []byte(`{
		"results": [
			{
				"id": "1",
				"indicator": "8.8.8.8",
				"type": "IPv4",
				"description": "Google DNS"
			}
		]
	}`)
	items, err := parseOTX(body)
	if err != nil {
		t.Fatalf("parseOTX: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "8.8.8.8" {
		t.Errorf("title: %q", items[0].Title)
	}
}

func TestParseHIBP(t *testing.T) {
	body := []byte(`[{
		"Name": "Adobe",
		"Domain": "adobe.com",
		"BreachDate": "2013-10-04",
		"PwnCount": 152445165,
		"Description": "Big breach"
	}]`)
	items, err := parseHIBP(body)
	if err != nil {
		t.Fatalf("parseHIBP: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "Adobe" {
		t.Errorf("title: %q", items[0].Title)
	}
}
