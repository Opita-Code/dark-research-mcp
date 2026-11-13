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
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestResultTookIsMilliseconds pins the wire contract for the route
// duration.
//
// Took used to be a time.Duration tagged `took_ms`, but encoding/json
// marshals a time.Duration as an integer count of NANOSECONDS — so a
// 350 ms call was published as 3473368300. The field name promises
// milliseconds and the research_runs.took_ms column stores
// milliseconds, so both sides must agree.
func TestResultTookIsMilliseconds(t *testing.T) {
	res := &Result{}
	res.Took = 350 * time.Millisecond
	res.TookMs = res.Took.Milliseconds()

	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := decoded["took_ms"].(float64)
	if !ok {
		t.Fatalf("took_ms missing or not a number: %#v", decoded["took_ms"])
	}
	if got != 350 {
		t.Errorf("took_ms = %v, want 350 (milliseconds, not nanoseconds)", got)
	}
	// The raw duration must not leak a second, conflicting key.
	if _, dup := decoded["Took"]; dup {
		t.Error("the raw time.Duration must not be serialised alongside took_ms")
	}
}

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

// TestParseAbuseCH uses the REAL URLhaus /v1/host/ shape documented at
// https://urlhaus-api.abuse.ch/ (2026-11-13). The host record sits at
// the top level and the timestamp field is `date_added` — the old
// parser looked for `dateadded`, which the API never emits.
func TestParseAbuseCH(t *testing.T) {
	body := []byte(`{
		"query_status": "ok",
		"urlhaus_reference": "https://urlhaus.abuse.ch/host/example.com/",
		"host": "example.com",
		"firstseen": "2026-01-02 03:04:05 UTC",
		"url_count": "2",
		"urls": [
			{
				"id": "123456",
				"url": "http://example.com/payload.exe",
				"url_status": "online",
				"date_added": "2026-02-03 04:05:06 UTC",
				"threat": "malware_download",
				"reporter": "someone",
				"tags": ["exe", "emotet"]
			},
			{
				"id": "123457",
				"url": "http://example.com/second.bin",
				"url_status": "offline",
				"date_added": "2026-02-04 04:05:06 UTC",
				"threat": "malware_download",
				"reporter": "someone",
				"tags": ["bin"]
			}
		]
	}`)
	items, err := parseAbuseCH(body)
	if err != nil {
		t.Fatalf("parseAbuseCH: %v", err)
	}
	// 1 host record + 2 URLs.
	if len(items) != 3 {
		t.Fatalf("want 3 items (host + 2 urls), got %d", len(items))
	}
	if items[0].Title != "example.com" {
		t.Errorf("host item title: %q", items[0].Title)
	}
	if items[1].Title != "http://example.com/payload.exe" {
		t.Errorf("url item title: %q", items[1].Title)
	}
	if !strings.Contains(items[1].URL, "123456") {
		t.Errorf("url item link: %q", items[1].URL)
	}
}

// TestParseAbuseCH_NoResults covers the "host not tracked" reply.
func TestParseAbuseCH_NoResults(t *testing.T) {
	items, err := parseAbuseCH([]byte(`{"query_status":"no_results"}`))
	if err != nil {
		t.Fatalf("parseAbuseCH: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("want 0 items, got %d", len(items))
	}
}

// TestParseAbuseCH_BadStatus surfaces a non-ok query_status as an
// error instead of a misleading empty result.
func TestParseAbuseCH_BadStatus(t *testing.T) {
	if _, err := parseAbuseCH([]byte(`{"query_status":"invalid_host"}`)); err == nil {
		t.Fatal("expected error for non-ok query_status")
	}
}

// web.archive.org on 2026-11-13. The API answers with a JSON ARRAY
// whose first row is the column header:
//
//	[["urlkey","timestamp","original",...],
//	 ["com,example)/","20020120142510","http://example.com:80/",...]]
//
// TestParseHackerNews uses the REAL HN Algolia Search API shape
// captured on 2026-11-13. The title carries HTML highlight markup
// that has to be stripped.
func TestParseHackerNews(t *testing.T) {
	body := []byte(`{
		"nbHits": 2,
		"hits": [
			{
				"title": "<em>Climate</em> change: IPCC report is 'code red'",
				"url": "https://www.bbc.com/news/science-environment-58130705",
				"author": "perfunctory",
				"created_at": "2026-01-02T03:04:05.000Z",
				"points": 42,
				"num_comments": 17,
				"objectID": "28113852"
			},
			{
				"title": "Ask HN: how do you monitor climate data?",
				"url": null,
				"author": "someone",
				"created_at": "2026-01-03T03:04:05.000Z",
				"points": 3,
				"num_comments": 1,
				"objectID": "28114999"
			}
		]
	}`)
	items, err := parseHackerNews(body)
	if err != nil {
		t.Fatalf("parseHackerNews: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	// The highlight markup must be stripped from the title.
	if items[0].Title != "Climate change: IPCC report is 'code red'" {
		t.Errorf("title: %q", items[0].Title)
	}
	if items[0].URL != "https://www.bbc.com/news/science-environment-58130705" {
		t.Errorf("url: %q", items[0].URL)
	}
	// A story without an external URL falls back to the HN discussion.
	if items[1].URL != "https://news.ycombinator.com/item?id=28114999" {
		t.Errorf("expected the HN discussion fallback, got %q", items[1].URL)
	}
	if items[0].FreshnessAt.IsZero() {
		t.Error("expected freshness from created_at to be parsed")
	}
}

// TestLooksLikeURLOrDomain covers the gate that keeps the Wayback
// archival backend from padding a topical query with unrelated
// captures.
func TestLooksLikeURLOrDomain(t *testing.T) {
	cases := map[string]bool{
		"https://example.com/path": true,
		"http://example.com":       true,
		"example.com":              true,
		"www.example.com/page":     true,
		"climate change":           false,
		"CVE-2024-3094":            false,
		"":                         false,
	}
	for q, want := range cases {
		if got := looksLikeURLOrDomain(q); got != want {
			t.Errorf("looksLikeURLOrDomain(%q) = %v, want %v", q, got, want)
		}
	}
}

// The previous fixture used JSON-lines, which the API never emits —
// that mismatch is exactly why the parser shipped broken.
func TestParseWayback(t *testing.T) {
	body := []byte(`[["urlkey","timestamp","original","mimetype","statuscode","digest","length"],
["com,example)/","20020120142510","http://example.com:80/","text/html","200","HT2DYGA5UKZCPBSFVCV3JOBXGW2G5UUA","1792"],
["com,example)/","20020328012821","http://www.example.com:80/","text/html","200","UY3I2DT2AMWAY6DECFCFYMT5ZOTFHUCH","481"]]`)
	items, err := parseWayback(body)
	if err != nil {
		t.Fatalf("parseWayback: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if !strings.Contains(items[0].URL, "20020120142510") {
		t.Errorf("url: %q", items[0].URL)
	}
	if items[0].Title != "http://example.com:80/" {
		t.Errorf("title: %q", items[0].Title)
	}
}

// TestParseWayback_EmptyArray covers the "no match" reply, which is
// a bare empty JSON array.
func TestParseWayback_EmptyArray(t *testing.T) {
	items, err := parseWayback([]byte(`[]`))
	if err != nil {
		t.Fatalf("parseWayback: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("want 0 items, got %d", len(items))
	}
}

// TestParseWayback_Malformed makes sure a non-JSON body surfaces as
// an error instead of silently returning zero results (which would
// look like "the host has no captures" to the caller).
func TestParseWayback_Malformed(t *testing.T) {
	if _, err := parseWayback([]byte(`not json at all`)); err == nil {
		t.Fatal("expected error for malformed body")
	}
}

// TestParseGDELT uses the REAL GDELT DOC 2.0 shape captured on
// 2026-11-13. format=json answers with an OBJECT holding an
// "articles" array, not a bare array.
func TestParseGDELT(t *testing.T) {
	body := []byte(`{"articles": [
		{
			"url": "http://world.people.com.cn/n1/2026/1002/c1002-40808849.html",
			"url_mobile": "",
			"title": "Climate report released",
			"seendate": "20261002T024500Z",
			"socialimage": "",
			"domain": "world.people.com.cn",
			"language": "English",
			"sourcecountry": "China"
		}
	]}`)
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
	if items[0].FreshnessAt.IsZero() {
		t.Error("expected freshness from seendate to be parsed")
	}
}

// TestParseGDELT_BareArrayIsRejected documents that the old
// (wrong) top-level-array shape is no longer silently accepted.
func TestParseGDELT_BareArrayIsRejected(t *testing.T) {
	if _, err := parseGDELT([]byte(`[{"title":"x","url":"y","seendate":"20240701T120000Z"}]`)); err == nil {
		t.Fatal("expected error: GDELT returns an object, not an array")
	}
}

func TestParseAhmia(t *testing.T) {
	// Ahmia is the most fragile backend (anti-bot); we test the
	// parser with a recorded body that has result items. The
	// parser's regex is non-greedy and does not span newlines, so
	// The test HTML follows ahmia-site's real tor_results.html
	// template: the results live in <ol class="searchResults">, each
	// entry is <li class="result"> and the title anchor is nested in
	// an <h4>. The href is an onion-redirect whose real target lives
	// in the `redirect_url` parameter.
	body := []byte(`<html><body><ol class="searchResults">
<li class="result">
  <h4>
    <a href="/search/redirect?search_term=bitcoin&amp;redirect_url=http%3A%2F%2Fexample.onion%2F">Example Hidden Service</a>
  </h4>
  <p>Short description here.</p>
  <cite>example.onion</cite>
  &mdash;
  <span class="lastSeen" data-timestamp="2026-01-02T03:04:05Z">2 months ago</span>
</li>
</ol></body></html>`)
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
	// The redirect wrapper must be unwrapped to the real onion URL.
	if items[0].URL != "http://example.onion/" {
		t.Errorf("url: %q (expected the redirect to be resolved)", items[0].URL)
	}
	if !strings.Contains(items[0].Snippet, "Short description") {
		t.Errorf("snippet: %q", items[0].Snippet)
	}
}

// TestParseAhmia_HomepageInsteadOfResults makes sure the parser
// reports an error when Ahmia answers with the homepage (which is
// what happens when the rolling token is missing or stale) instead of
// silently returning "0 results".
func TestParseAhmia_HomepageInsteadOfResults(t *testing.T) {
	body := []byte(`<html><body><form id="searchForm" action="/search/" method="get">
<input id="id_q" type="search" name="q" title="q">
<input type="hidden" name="940710" value="abd9eb">
</form></body></html>`)
	if _, err := parseAhmia(body); err == nil {
		t.Fatal("expected an error when the response is the homepage, not a result page")
	}
}

// TestParseAhmiaToken covers the rolling-token scrape: the homepage
// embeds the token as a hidden input whose NAME is the rotating
// parameter name.
func TestParseAhmiaToken(t *testing.T) {
	body := `<form id="searchForm" action="/search/" method="get">
<input id="id_q" type="search" name="q" title="q">
<input type="hidden" name="940710" value="abd9eb">
</form>`
	token, field, ok := parseAhmiaToken(body)
	if !ok {
		t.Fatal("expected to find the rolling token")
	}
	if token != "abd9eb" {
		t.Errorf("token: %q", token)
	}
	if field != "940710" {
		t.Errorf("field: %q", field)
	}
}

// TestParseRIPE uses the REAL RIPE Stat whois shape captured on
// 2026-11-13: "records" is an array of arrays of OBJECTS, each object
// being {"key":...,"value":...}. The old fixture used arrays of plain
// strings, which the API never returns.
func TestParseRIPE(t *testing.T) {
	body := []byte(`{
		"data": {
			"records": [[
				{"key":"NetRange","value":"8.8.8.0 - 8.8.8.255","comment":null,"details_link":null},
				{"key":"CIDR","value":"8.8.8.0/24","comment":null,"details_link":null},
				{"key":"NetName","value":"GOGL","comment":null,"details_link":null},
				{"key":"inetnum","value":"8.8.8.0 - 8.8.8.255","comment":null,"details_link":null},
				{"key":"netname","value":"GOGL","comment":null,"details_link":null},
				{"key":"country","value":"US","comment":null,"details_link":null}
			]]
		}
	}`)
	items, err := parseRIPE(body)
	if err != nil {
		t.Fatalf("parseRIPE: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Title != "8.8.8.0 - 8.8.8.255" {
		t.Errorf("title: %q", items[0].Title)
	}
	if !strings.Contains(items[0].Snippet, "GOGL") {
		t.Errorf("snippet: %q", items[0].Snippet)
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

// TestParseOTX uses the REAL OTX indicator shape captured from
// /api/v1/indicators/{type}/{value}/general on 2026-11-13. The old
// /search/limited endpoint (which returned {"results":[...]}) is no
// longer served by the API — it answers 404.
func TestParseOTX(t *testing.T) {
	body := []byte(`{
		"indicator": "8.8.8.8",
		"type": "IPv4",
		"type_title": "IPv4 Address",
		"reputation": 3,
		"description": "Google DNS",
		"tags": ["dns", "google"],
		"validation": [{"source": "AliDNS", "valid": true}],
		"malware": []
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
	if items[0].Snippet != "Google DNS" {
		t.Errorf("snippet: %q", items[0].Snippet)
	}
}

// TestParseOTX_MalwareFamilies covers the malware family entries that
// the indicator record can carry.
func TestParseOTX_MalwareFamilies(t *testing.T) {
	body := []byte(`{
		"indicator": "example.com",
		"type": "domain",
		"reputation": -3,
		"malware": [{"detection_ratio": "12/70", "family": "Emotet"}]
	}`)
	items, err := parseOTX(body)
	if err != nil {
		t.Fatalf("parseOTX: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items (indicator + malware family), got %d", len(items))
	}
	if items[1].Title != "Emotet" {
		t.Errorf("malware title: %q", items[1].Title)
	}
}

// TestParseOTX_NoIndicator makes sure a payload without an indicator
// is an error rather than a silent empty result.
func TestParseOTX_NoIndicator(t *testing.T) {
	if _, err := parseOTX([]byte(`{"results":[]}`)); err == nil {
		t.Fatal("expected error when no indicator is present")
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
