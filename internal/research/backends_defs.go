package research

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// webBackends: DuckDuckGo HTML (primary, no auth, scraping) → SearXNG
// public instance → Brave (free tier w/ key).
func webBackends() []Backend {
	return []Backend{
		{
			Name: "duckduckgo", BaseURL: "https://html.duckduckgo.com/html/",
			Method: "GET", Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.7, LangHint: "en",
			RateLimitMs: 500,
			Parse:       parseDuckDuckGo,
		},
		{
			// SearXNG instances answer HTML by default. The JSON
			// output format has to be requested explicitly with
			// `format=json`; without it the instance returns its
			// HTML results page and parseSearXNG — which expects a
			// JSON object with a "results" array — can never
			// succeed.
			//
			// Public instances are frequently behind anti-bot walls
			// that answer 200 with an HTML challenge page. The
			// instance list is overridable via the SEARXNG_INSTANCES
			// env var (comma-separated) so an operator can point at
			// a self-hosted instance; the parser below detects the
			// challenge page and reports it as a clear error instead
			// of a confusing JSON parse failure.
			Name: "searxng", BaseURL: searxngBaseURL(),
			Method: "GET", Free: true, OpenSource: true, Weight: 2,
			Confidence: 0.75, LangHint: "en",
			URLForQuery: func(q string) string {
				return searxngBaseURL() + "/search?q=" + url.QueryEscape(q) + "&format=json"
			},
			RateLimitMs: 1000,
			Parse:       parseSearXNG,
		},
		{
			Name: "brave", BaseURL: "https://api.search.brave.com/res/v1/web/search",
			Method: "GET", Auth: "BRAVE_API_KEY", Weight: 3,
			Confidence: 0.85, LangHint: "en",
			RateLimitMs: 200,
			Parse:       parseBraveWeb,
		},
	}
}

// academicBackends: OpenAlex (primary, free, no auth) → arXiv API
// (free, open) → Semantic Scholar (free w/ key).
func academicBackends() []Backend {
	return []Backend{
		{
			// OpenAlex filters works with `search=` (free-text across
			// title/abstract/fulltext). The router's default `?q=`
			// parameter is rejected by the API with HTTP 400
			// ("q only filters the keys of the id, doi, ..."), which
			// made this backend fail on every call.
			Name: "openalex", BaseURL: "https://api.openalex.org/works",
			Method: "GET", Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.92, LangHint: "en", FreshnessField: "publication_date",
			URLForQuery: func(q string) string {
				return "https://api.openalex.org/works?search=" + url.QueryEscape(q) + "&per-page=20"
			},
			RateLimitMs: 100,
			Parse:       parseOpenAlex,
		},
		{
			// arXiv's API takes `search_query=` with its own query
			// syntax, not `q=`. Passing `q` yields HTTP 400 with an
			// empty Atom feed.
			Name: "arxiv", BaseURL: "http://export.arxiv.org/api/query",
			Method: "GET", Free: true, OpenSource: true, Weight: 2,
			Confidence: 0.95, LangHint: "en", FreshnessField: "published",
			URLForQuery: func(q string) string {
				// arXiv's search_query syntax prefixes the field with
				// "all:". The whole value is escaped so a query with
				// spaces or punctuation stays a single parameter.
				return "http://export.arxiv.org/api/query?search_query=" +
					url.QueryEscape("all:"+q) + "&max_results=20"
			},
			RateLimitMs: 1000,
			Parse:       parseArxiv,
		},
		{
			// Semantic Scholar authenticates with the `x-api-key`
			// header (official tutorial, 2026-11-13), not with
			// "Authorization: Bearer". The bulk search endpoint
			// supports richer fields (publicationDate, year,
			// citationCount) and pagination via a `token`.
			Name: "semanticscholar", BaseURL: "https://api.semanticscholar.org/graph/v1/paper/search/bulk",
			Method: "GET", Auth: "S2_API_KEY", AuthHeader: "x-api-key", Weight: 3,
			Confidence: 0.9, LangHint: "en", FreshnessField: "publicationDate",
			URLForQuery: func(q string) string {
				return "https://api.semanticscholar.org/graph/v1/paper/search/bulk?query=" + url.QueryEscape(q) +
					"&fields=title,url,abstract,year,publicationDate,citationCount,externalIds,openAccessPdf"
			},
			RateLimitMs: 1000,
			Parse:       parseSemanticScholar,
		},
	}
}

// codeBackends: crates.io (open) → npm registry (open) → GitHub (free tier).
func codeBackends() []Backend {
	return []Backend{
		{
			// crates.io requires a non-empty User-Agent; the router
			// already sets one. The `Accept: application/json`
			// header avoids the HTML redirect the registry serves
			// for unknown paths.
			Name: "cratesio", BaseURL: "https://crates.io/api/v1/crates",
			Method: "GET", Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.9, LangHint: "en", FreshnessField: "updated_at",
			Headers: map[string]string{
				"Accept": "application/json",
			},
			RateLimitMs: 100,
			Parse:       parseCratesIO,
		},
		{
			// The npm registry search endpoint takes `text=` (not
			// `q=`); passing `q` returns HTTP 400 with
			// {"code":"ERR_TEXT_MISSING"}.
			Name: "npm", BaseURL: "https://registry.npmjs.com/-/v1/search",
			Method: "GET", Free: true, OpenSource: true, Weight: 2,
			Confidence: 0.88, LangHint: "en", FreshnessField: "date",
			URLForQuery: func(q string) string {
				return "https://registry.npmjs.com/-/v1/search?text=" + url.QueryEscape(q) + "&size=20"
			},
			RateLimitMs: 100,
			Parse:       parseNPM,
		},
		{
			Name: "github", BaseURL: "https://api.github.com/search/repositories",
			Method: "GET", Auth: "GITHUB_TOKEN", Weight: 3,
			Confidence: 0.85, LangHint: "en", FreshnessField: "updated_at",
			Headers: map[string]string{
				"Accept": "application/vnd.github+json",
			},
			RateLimitMs: 200,
			Parse:       parseGitHubSearch,
		},
	}
}

// cveBackends: OSV.dev (primary, free, no auth) → NVD (rate-limited).
func cveBackends() []Backend {
	return []Backend{
		{
			Name: "osv", Free: true, OpenSource: true, Weight: 1,
			URLForQuery: func(q string) string {
				return "https://api.osv.dev/v1/vulns/" + url.PathEscape(q)
			},
			Method:     "GET",
			Confidence: 0.95, LangHint: "en",
			RateLimitMs: 200,
			Parse:       parseOSV,
		},
		{
			Name: "nvd", Weight: 2,
			URLForQuery: func(q string) string {
				return "https://services.nvd.nist.gov/rest/json/cves/2.0?cveId=" + url.QueryEscape(q)
			},
			Method:     "GET",
			Confidence: 0.97, LangHint: "en", FreshnessField: "published",
			RateLimitMs: 6000, // NVD public rate limit: 5 req / 30 s
			Parse:       parseNVD,
		},
	}
}

// domainBackends: RDAP via IANA bootstrap → DNS via Cloudflare DoH
// (we treat domain lookups as RDAP first; DNS is a separate intent).
func domainBackends() []Backend {
	return []Backend{
		{
			Name: "rdap", BaseURL: "https://rdap.org/",
			Method: "GET", Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.95, LangHint: "en",
			URLForQuery: func(q string) string {
				return "https://rdap.org/domain/" + url.PathEscape(q)
			},
			RateLimitMs: 500,
			Parse:       parseRDAP,
		},
	}
}

// dnsBackends: Cloudflare DoH → Google DoH.
//
// Cloudflare's DoH endpoint only answers with JSON when the request
// carries `Accept: application/dns-json`; with the router's generic
// Accept header it answers HTTP 400 (verified 2026-11-13). Google DoH
// returns JSON regardless, so it needs no special header.
func dnsBackends() []Backend {
	return []Backend{
		{
			Name: "cloudflare-doh", BaseURL: "https://cloudflare-dns.com/dns-query",
			Method: "GET", Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.9, LangHint: "en",
			URLForQuery: func(q string) string {
				// q is expected as "type,domain" or just "domain" (defaults to A).
				parts := strings.SplitN(q, ",", 2)
				domain := parts[0]
				qtype := "A"
				if len(parts) > 1 && parts[1] != "" {
					qtype = parts[1]
				}
				return "https://cloudflare-dns.com/dns-query?name=" + url.QueryEscape(domain) + "&type=" + url.QueryEscape(qtype)
			},
			Headers: map[string]string{
				"Accept": "application/dns-json",
			},
			RateLimitMs: 100,
			Parse:       parseDoH,
		},
		{
			Name: "google-doh", BaseURL: "https://dns.google/resolve",
			Method: "GET", Free: true, OpenSource: true, Weight: 2,
			Confidence: 0.92, LangHint: "en",
			URLForQuery: func(q string) string {
				parts := strings.SplitN(q, ",", 2)
				domain := parts[0]
				qtype := "A"
				if len(parts) > 1 && parts[1] != "" {
					qtype = parts[1]
				}
				return "https://dns.google/resolve?name=" + url.QueryEscape(domain) + "&type=" + url.QueryEscape(qtype)
			},
			Headers: map[string]string{
				"Accept": "application/json",
			},
			RateLimitMs: 100,
			Parse:       parseGoogleDoH,
		},
	}
}

// certBackends: crt.sh (free, no auth) → Censys (free w/ key).
func certBackends() []Backend {
	return []Backend{
		{
			// crt.sh is the only Certificate Transparency search that
			// works without an API key, but it answers 502 very often
			// under load. A short backoff makes the retries collide
			// with the same overload window, so the base delay is
			// raised to 3s (3s, 6s, 12s).
			Name: "crtsh", BaseURL: "https://crt.sh/",
			Method: "GET", Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.95, LangHint: "en", FreshnessField: "not_before",
			URLForQuery: func(q string) string {
				return "https://crt.sh/?q=" + url.QueryEscape(q) + "&output=json"
			},
			RateLimitMs:    1000,
			Retries:        3,
			RetryBackoffMs: 3000,
			Parse:          parseCrtsh,
		},
	}
}

// ipBackends: ip-api.com (free 45/min) → RIPE DB → ipinfo.io (free w/ key).
func ipBackends() []Backend {
	return []Backend{
		{
			Name: "ipapi", Free: true, OpenSource: true, Weight: 1,
			URLForQuery: func(q string) string {
				return "http://ip-api.com/json/" + url.PathEscape(q)
			},
			Method:     "GET",
			Confidence: 0.8, LangHint: "en",
			RateLimitMs: 1500, // 45/min ≈ 1.3s/call
			Parse:       parseIPAPI,
		},
		{
			Name: "ripe", Free: true, OpenSource: true, Weight: 2,
			URLForQuery: func(q string) string {
				return "https://stat.ripe.net/data/whois/data.json?resource=" + url.QueryEscape(q) + "&type=inetnum"
			},
			Method:     "GET",
			Confidence: 0.95, LangHint: "en",
			RateLimitMs: 200,
			Parse:       parseRIPE,
		},
	}
}

// threatBackends: abuse.ch URLhaus (free, requires Auth-Key) →
// AlienVault OTX (free w/ key).
//
// URLhaus contract (verified against the official API docs on
// 2026-11-13, https://urlhaus-api.abuse.ch/):
//
//   - Every endpoint requires the `Auth-Key` header. Without it the
//     API answers 401. There is no anonymous tier.
//   - The host lookup is an HTTP POST to /v1/host/ with the host as
//     form-encoded body data (`host=<value>`). A GET with the host in
//     the path is rejected.
//   - The response carries `date_added`, not `dateadded`.
func threatBackends() []Backend {
	return []Backend{
		{
			Name: "abusech", BaseURL: "https://urlhaus-api.abuse.ch/v1/host/",
			Method: "POST", Auth: "URLHAUS_AUTH_KEY", AuthHeader: "Auth-Key",
			Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.92, LangHint: "en", FreshnessField: "date_added",
			URLForQuery: func(q string) string {
				return "https://urlhaus-api.abuse.ch/v1/host/"
			},
			Body: func(q string) io.Reader {
				return strings.NewReader(url.Values{"host": {q}}.Encode())
			},
			Headers: map[string]string{
				"Content-Type": "application/x-www-form-urlencoded",
			},
			RateLimitMs: 500,
			Parse:       parseAbuseCH,
		},
		{
			// OTX search/limited was removed from the public API (404).
			// The indicator endpoint is the supported route: it
			// returns the full indicator record for a domain/IP/hash.
			Name: "otx", BaseURL: "https://otx.alienvault.com/api/v1/indicators",
			Method: "GET", Auth: "OTX_API_KEY", AuthHeader: "X-OTX-API-KEY",
			Weight:     2,
			Confidence: 0.88, LangHint: "en",
			URLForQuery: func(q string) string {
				kind := otxIndicatorKind(q)
				return "https://otx.alienvault.com/api/v1/indicators/" + kind + "/" + url.PathEscape(q) + "/general"
			},
			RateLimitMs: 500,
			Parse:       parseOTX,
		},
	}
}

// otxIndicatorKind classifies an OTX indicator into the type segment
// the API expects (domain, IPv4, IPv6, FileHash, hostname...).
func otxIndicatorKind(q string) string {
	q = strings.TrimSpace(q)
	if net.ParseIP(q) != nil {
		if strings.Contains(q, ":") {
			return "IPv6"
		}
		return "IPv4"
	}
	switch {
	case isAllHex(q) && len(q) == 64:
		return "FileHash-sha256"
	case isAllHex(q) && len(q) == 40:
		return "FileHash-sha1"
	case isAllHex(q) && len(q) == 32:
		return "FileHash-md5"
	case strings.Contains(q, "."):
		return "domain"
	default:
		return "hostname"
	}
}

// isAllHex reports whether s is a non-empty lowercase-or-uppercase hex
// string.
func isAllHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// emailBackends: HIBP (breach lookup, free w/ key).
//
// Username enumeration (holehe / Sherlock style) is implemented as
// a separate tool later — those tools check sites via HTTP and need
// per-site logic, not a single API.
func emailBackends() []Backend {
	return []Backend{
		{
			Name: "hibp", BaseURL: "https://haveibeenpwned.com/api/v3/breachedaccount/",
			Method: "GET", Auth: "HIBP_API_KEY", Weight: 1,
			Confidence: 0.97, LangHint: "en", FreshnessField: "BreachDate",
			URLForQuery: func(q string) string {
				return "https://haveibeenpwned.com/api/v3/breachedaccount/" + url.PathEscape(q)
			},
			RateLimitMs: 1500,
			Parse:       parseHIBP,
		},
	}
}

// darkBackends: Ahmia.fi (clearnet index of .onion). Falls back to
// onion_fetch via Tor proxy (configured in TorConfig).
//
// Ahmia protects its search endpoint with a rolling token (verified
// against ahmia-site's ahmia/views.py on 2026-11-13). The
// TorResultsView reads a token from a request parameter whose NAME
// also rotates every minute:
//
//	for field_name in rotating_field_names():
//	    token = request.GET.get(field_name, "")
//	    if token: break
//	if not valid_token(token):
//	    return redirect("home")
//
// Without a valid token the server answers HTTP 302 to "/" and the
// caller gets the homepage instead of results. The token is derived
// from a server-side secret salt, so it cannot be computed — it must
// be read from the search form on the homepage, which renders it as
// a hidden input:
//
//	<input type="hidden" name="940710" value="abd9eb">
//
// The backend therefore performs a two-step request: fetch the
// homepage, scrape the hidden field, then issue the search.
func darkBackends() []Backend {
	return []Backend{
		{
			Name: "ahmia", BaseURL: "https://ahmia.fi/search/",
			Method: "GET", Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.7, LangHint: "en", FreshnessField: "last_seen",
			URLForQuery: func(q string) string {
				return "https://ahmia.fi/search/?q=" + url.QueryEscape(q) + ahmiaTokenQuery()
			},
			RateLimitMs: 1000,
			Parse:       parseAhmia,
		},
	}
}

// ahmiaTokenQuery returns the extra query string carrying Ahmia's
// rolling token, e.g. "&940710=abd9eb".
//
// The token must be fetched from the homepage first, so this helper
// performs that fetch (once per token window) and caches the result.
// On failure it returns an empty string; the search request then
// gets redirected to the homepage and the parser detects that the
// response is not a result page.
func ahmiaTokenQuery() string {
	tok, field, ok := ahmiaToken()
	if !ok {
		return ""
	}
	return "&" + url.QueryEscape(field) + "=" + url.QueryEscape(tok)
}

// ahmiaTokenCache holds the scraped token and the wall-clock minute
// it was obtained for. The token rotates every minute, so it is
// refreshed at most once per minute.
var ahmiaTokenCache struct {
	token  string
	field  string
	minute int64
	ok     bool
}

// ahmiaToken scrapes the homepage and returns the rolling token and
// its parameter name.
func ahmiaToken() (token, field string, ok bool) {
	nowMinute := time.Now().Unix() / 60
	if ahmiaTokenCache.ok && ahmiaTokenCache.minute == nowMinute {
		return ahmiaTokenCache.token, ahmiaTokenCache.field, true
	}

	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest("GET", "https://ahmia.fi/", nil)
	if err != nil {
		return "", "", false
	}
	req.Header.Set("User-Agent", "dark-research-mcp/"+Version()+" (+https://github.com/dark-agents/research-mcp)")
	req.Header.Set("Accept", "text/html")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", false
	}

	tok, fld, found := parseAhmiaToken(string(body))
	if !found {
		return "", "", false
	}
	ahmiaTokenCache = struct {
		token  string
		field  string
		minute int64
		ok     bool
	}{token: tok, field: fld, minute: nowMinute, ok: true}
	return tok, fld, true
}

// ahmiaTokenRe matches the hidden input that carries the rolling
// token: <input type="hidden" name="940710" value="abd9eb">.
var ahmiaTokenRe = regexp.MustCompile(`(?is)<input[^>]*type="hidden"[^>]*>`)

// ahmiaNameRe extracts the name attribute.
var ahmiaNameRe = regexp.MustCompile(`(?i)\bname="([^"]+)"`)

// ahmiaValueRe extracts the value attribute.
var ahmiaValueRe = regexp.MustCompile(`(?i)\bvalue="([^"]*)"`)

// parseAhmiaToken extracts the rolling token from the homepage HTML.
// The hidden input's NAME is the rotating parameter name and its
// VALUE is the token.
func parseAhmiaToken(html string) (token, field string, ok bool) {
	for _, tag := range ahmiaTokenRe.FindAllString(html, -1) {
		nameM := ahmiaNameRe.FindStringSubmatch(tag)
		valM := ahmiaValueRe.FindStringSubmatch(tag)
		if nameM == nil || valM == nil {
			continue
		}
		name, val := nameM[1], valM[1]
		if name == "" || val == "" {
			continue
		}
		// The token is 6 hex characters and the field name is 6 hex
		// characters too: ahmia derives BOTH from the same SHA-1
		// digest ("digest[:6]" for the token and "digest[6:12]" for
		// the field name), so the field name can contain letters
		// (e.g. "40d410"), not just digits.
		if len(val) == 6 && isAllHex(val) && len(name) == 6 && isAllHex(name) {
			return val, name, true
		}
	}
	return "", "", false
}

// geoBackends: OpenStreetMap Nominatim (free w/ UA) → Overpass API.
func geoBackends() []Backend {
	return []Backend{
		{
			Name: "osm-nominatim", BaseURL: "https://nominatim.openstreetmap.org/search",
			Method: "GET", Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.9, LangHint: "en",
			URLForQuery: func(q string) string {
				return "https://nominatim.openstreetmap.org/search?q=" + url.QueryEscape(q) + "&format=json"
			},
			RateLimitMs: 1000,
			Parse:       parseNominatim,
		},
	}
}

// newsBackends: GDELT (free, no auth) → Wayback Machine CDX.
func newsBackends() []Backend {
	return []Backend{
		{
			Name: "gdelt", BaseURL: "https://api.gdeltproject.org/api/v2/doc/doc",
			Method: "GET", Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.8, LangHint: "en", FreshnessField: "seendate",
			URLForQuery: func(q string) string {
				return "https://api.gdeltproject.org/api/v2/doc/doc?query=" + url.QueryEscape(q) + "&mode=ArtList&maxrecords=20&format=json"
			},
			// GDELT DOC 2.0 enforces "one request every 5 seconds"
			// and answers HTTP 429 otherwise. The previous 500ms limit
			// tripped the throttle on nearly every call.
			RateLimitMs: 5500,
			// 429 is a throttle, not a hard failure — back off and retry.
			Retries: 2,
			Parse:   parseGDELT,
		},
		{
			// Hacker News (Algolia Search API) — free, no auth, no rate
			// limit to speak of. It indexes tech/news discussion, which
			// makes it a useful complement to GDELT's global news
			// graph and a reliable fallback when GDELT throttles
			// (GDELT enforces ~1 request every 5 seconds and answers
			// 429 on shared egress IPs).
			Name: "hackernews", BaseURL: "https://hn.algolia.com/api/v1/search",
			Method: "GET", Free: true, OpenSource: true, Weight: 2,
			Confidence: 0.75, LangHint: "en", FreshnessField: "created_at",
			URLForQuery: func(q string) string {
				return "https://hn.algolia.com/api/v1/search?query=" + url.QueryEscape(q) + "&hitsPerPage=20&tags=story"
			},
			RateLimitMs: 200,
			Parse:       parseHackerNews,
		},
		{
			// The Wayback CDX API archives URLs, not topics: `url=` is
			// matched against the URL of the capture. Handing it a
			// news keyword ("climate change") does not search news —
			// it looks for captures whose URL contains those words,
			// which returns unrelated hits (or nothing). The backend
			// therefore only runs for queries that actually look like
			// a URL or a domain; for a topical query the news intent
			// is served by GDELT alone and the router reports
			// GDELT's availability honestly instead of padding the
			// result with archive noise.
			Name: "wayback", BaseURL: "https://web.archive.org/cdx/search/cdx",
			Method: "GET", Free: true, OpenSource: true, Weight: 2,
			Confidence: 0.85, LangHint: "en", FreshnessField: "timestamp",
			URLForQuery: func(q string) string {
				return "https://web.archive.org/cdx/search/cdx?url=" + url.QueryEscape(q) + "&output=json&limit=20"
			},
			// Only attempt the call when the query is URL-shaped.
			Skip: func(query string) bool {
				return !looksLikeURLOrDomain(query)
			},
			RateLimitMs: 500,
			Parse:       parseWayback,
		},
	}
}

// parseHackerNews parses the HN Algolia Search API response.
//
// Shape (verified live 2026-11-13):
//
//	{"nbHits":N,"hits":[{"title":"...","url":"...","author":"...",
//	                    "created_at":"2026-01-02T03:04:05.000Z",
//	                    "points":42,"objectID":"12345", ...}]}
//
// The title carries HTML <em> highlight markup, which is stripped.
func parseHackerNews(body []byte) ([]Item, error) {
	var resp struct {
		NbHits int `json:"nbHits"`
		Hits   []struct {
			Title     string `json:"title"`
			URL       string `json:"url"`
			StoryURL  string `json:"story_url"`
			Author    string `json:"author"`
			CreatedAt string `json:"created_at"`
			Points    int    `json:"points"`
			NumCom    int    `json:"num_comments"`
			ObjectID  string `json:"objectID"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Hits))
	for _, h := range resp.Hits {
		link := h.URL
		if link == "" {
			// Ask HN / Show HN posts have no external URL; link to the
			// discussion instead.
			link = "https://news.ycombinator.com/item?id=" + h.ObjectID
		}
		title := stripTags(h.Title)
		snippet := fmt.Sprintf("author=%s points=%d comments=%d", h.Author, h.Points, h.NumCom)
		out = append(out, Item{
			Title:       title,
			URL:         link,
			Snippet:     snippet,
			Score:       1.0,
			Source:      "hackernews",
			FreshnessAt: parseRFC3339OrZero(h.CreatedAt),
			Raw: map[string]any{
				"author":   h.Author,
				"points":   h.Points,
				"comments": h.NumCom,
				"objectID": h.ObjectID,
			},
		})
	}
	return out, nil
}

// looksLikeURLOrDomain reports whether a query is plausibly a URL or
// a domain name, i.e. whether an archival lookup makes sense for it.
func looksLikeURLOrDomain(q string) bool {
	q = strings.TrimSpace(q)
	if q == "" {
		return false
	}
	if strings.HasPrefix(q, "http://") || strings.HasPrefix(q, "https://") {
		return true
	}
	// A bare domain: "example.com", "www.example.com/path".
	return looksLikeDomain(q)
}

// ---------------------------------------------------------------------------
// Parsers (backend-specific → normalized Item)
// ---------------------------------------------------------------------------

// parseDuckDuckGo parses DuckDuckGo HTML results. The HTML endpoint
// returns a page with .result__a links.
func parseDuckDuckGo(body []byte) ([]Item, error) {
	// Anchor pattern: <a rel="nofollow" class="result__a" href="URL">TITLE</a>
	re := regexp.MustCompile(`<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	snippetRe := regexp.MustCompile(`<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`)
	anchorMatches := re.FindAllStringSubmatch(string(body), 20)
	snippetMatches := snippetRe.FindAllStringSubmatch(string(body), 20)

	out := make([]Item, 0, len(anchorMatches))
	for i, m := range anchorMatches {
		href := strings.TrimSpace(m[1])
		title := stripTags(strings.TrimSpace(m[2]))
		var snippet string
		if i < len(snippetMatches) {
			snippet = stripTags(strings.TrimSpace(snippetMatches[i][1]))
		}
		// DuckDuckGo HTML redirects via //duckduckgo.com/l/?uddg=...
		// Extract the real URL from the uddg param.
		if u, err := url.QueryUnescape(href); err == nil {
			if strings.Contains(u, "uddg=") {
				if q, err := url.Parse(u); err == nil {
					if real := q.Query().Get("uddg"); real != "" {
						href = real
					}
				}
			}
		}
		out = append(out, Item{Title: title, URL: href, Snippet: snippet, Score: 1.0, Source: "duckduckgo"})
	}
	return out, nil
}

// searxngBaseURL returns the SearXNG instance to query.
//
// The default is a public instance. Operators can override it with the
// SEARXNG_INSTANCES env var (comma-separated list; the first entry is
// used) to point at a self-hosted instance, which is the only reliable
// way to use SearXNG in production — public instances are frequently
// behind anti-bot walls that answer HTTP 200 with an HTML challenge
// page instead of the JSON payload.
func searxngBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("SEARXNG_INSTANCES")); v != "" {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			return strings.TrimRight(part, "/")
		}
	}
	return "https://searx.be"
}

func parseSearXNG(body []byte) ([]Item, error) {
	// SearXNG's JSON output is {"results":[...], "number_of_results":N}.
	// When the instance is behind an anti-bot wall it answers HTTP 200
	// with an HTML challenge page; detect that so the caller gets an
	// actionable message instead of "invalid character '<' looking for
	// beginning of value".
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "<") {
		return nil, fmt.Errorf("searxng returned HTML instead of JSON " +
			"(instance is behind an anti-bot wall or has JSON output disabled; " +
			"set SEARXNG_INSTANCES to a self-hosted instance)")
	}
	var resp struct {
		NumberOfResults int `json:"number_of_results"`
		Results         []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
			Engine  string  `json:"engine"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Results))
	for _, r := range resp.Results {
		out = append(out, Item{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
			Score:   float32(r.Score),
			Source:  "searxng",
			Raw:     map[string]any{"engine": r.Engine},
		})
	}
	return out, nil
}

func parseBraveWeb(body []byte) ([]Item, error) {
	var resp struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Web.Results))
	for _, r := range resp.Web.Results {
		out = append(out, Item{
			Title: r.Title, URL: r.URL, Snippet: r.Description,
			Score: 1.0, Source: "brave",
		})
	}
	return out, nil
}

func parseOpenAlex(body []byte) ([]Item, error) {
	var resp struct {
		Results []struct {
			ID                    string           `json:"id"`
			Title                 string           `json:"title"`
			PublicationDate       string           `json:"publication_date"`
			Doi                   string           `json:"doi"`
			AbstractInvertedIndex map[string][]any `json:"abstract_inverted_index"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Results))
	for _, r := range resp.Results {
		itemURL := r.ID
		if r.Doi != "" {
			switch {
			case strings.HasPrefix(r.Doi, "http://") || strings.HasPrefix(r.Doi, "https://"):
				itemURL = r.Doi
			case strings.HasPrefix(r.Doi, "10."):
				itemURL = "https://doi.org/" + r.Doi
			}
		}
		snippet := "Published: " + r.PublicationDate
		out = append(out, Item{
			Title:       r.Title,
			URL:         itemURL,
			Snippet:     snippet,
			Score:       1.0,
			Source:      "openalex",
			FreshnessAt: parseRFC3339DateOrZero(r.PublicationDate),
		})
	}
	return out, nil
}

// arXiv returns Atom XML. We parse with encoding/xml.
func parseArxiv(body []byte) ([]Item, error) {
	type entry struct {
		Title   string `xml:"title"`
		ID      string `xml:"id"`
		Summary string `xml:"summary"`
	}
	var feed struct {
		Entries []entry `xml:"entry"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		summary := strings.TrimSpace(e.Summary)
		// Atom <summary> often has embedded newlines; collapse.
		summary = strings.Join(strings.Fields(summary), " ")
		out = append(out, Item{
			Title:   strings.TrimSpace(e.Title),
			URL:     e.ID,
			Snippet: summary,
			Score:   1.0,
			Source:  "arxiv",
		})
	}
	return out, nil
}

func parseSemanticScholar(body []byte) ([]Item, error) {
	// Both /paper/search and /paper/search/bulk answer with
	// {"total":N,"data":[...]} — the results live under "data".
	var resp struct {
		Total int64 `json:"total"`
		Data  []struct {
			PaperID         string `json:"paperId"`
			Title           string `json:"title"`
			URL             string `json:"url"`
			Abstract        string `json:"abstract"`
			Year            int    `json:"year"`
			PublicationDate string `json:"publicationDate"`
			CitationCount   int    `json:"citationCount"`
			ExternalIDs     struct {
				DOI string `json:"DOI"`
			} `json:"externalIds"`
			OpenAccessPDF *struct {
				URL string `json:"url"`
			} `json:"openAccessPdf"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Data))
	for _, p := range resp.Data {
		itemURL := p.URL
		if itemURL == "" && p.ExternalIDs.DOI != "" {
			itemURL = "https://doi.org/" + p.ExternalIDs.DOI
		}
		snippet := p.Abstract
		meta := []string{}
		if p.Year > 0 {
			meta = append(meta, fmt.Sprintf("year=%d", p.Year))
		}
		if p.PublicationDate != "" {
			meta = append(meta, "published="+p.PublicationDate)
		}
		if p.CitationCount > 0 {
			meta = append(meta, fmt.Sprintf("citations=%d", p.CitationCount))
		}
		if len(meta) > 0 {
			if snippet != "" {
				snippet += " — "
			}
			snippet += strings.Join(meta, " ")
		}
		out = append(out, Item{
			Title:       p.Title,
			URL:         itemURL,
			Snippet:     snippet,
			Score:       1.0,
			Source:      "semanticscholar",
			FreshnessAt: parseS2PublicationDate(p.PublicationDate, p.Year),
			Raw: map[string]any{
				"paperId":       p.PaperID,
				"year":          p.Year,
				"citationCount": p.CitationCount,
				"doi":           p.ExternalIDs.DOI,
			},
		})
	}
	return out, nil
}

// parseS2PublicationDate converts an S2 publicationDate
// ("2024-05-03") into a time.Time. Falls back to a year-only
// timestamp when the full date is absent.
func parseS2PublicationDate(s string, year int) time.Time {
	if s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			return t
		}
	}
	if year > 0 {
		return time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Time{}
}

func parseCratesIO(body []byte) ([]Item, error) {
	var resp struct {
		Crates []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Homepage    string `json:"homepage"`
			Repository  string `json:"repository"`
		} `json:"crates"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Crates))
	for _, c := range resp.Crates {
		url := c.Homepage
		if url == "" {
			url = c.Repository
		}
		if url == "" {
			url = "https://crates.io/crates/" + c.Name
		}
		out = append(out, Item{
			Title: c.Name, URL: url, Snippet: c.Description,
			Score: 1.0, Source: "cratesio",
		})
	}
	return out, nil
}

func parseNPM(body []byte) ([]Item, error) {
	var resp struct {
		Objects []struct {
			Package struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Links       struct {
					Homepage string `json:"homepage"`
					Repo     string `json:"repository"`
				} `json:"links"`
			} `json:"package"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Objects))
	for _, o := range resp.Objects {
		pkg := o.Package
		url := pkg.Links.Homepage
		if url == "" {
			url = pkg.Links.Repo
		}
		if url == "" {
			url = "https://www.npmjs.com/package/" + pkg.Name
		}
		out = append(out, Item{
			Title: pkg.Name, URL: url, Snippet: pkg.Description,
			Score: 1.0, Source: "npm",
		})
	}
	return out, nil
}

func parseGitHubSearch(body []byte) ([]Item, error) {
	var resp struct {
		TotalCount int `json:"total_count"`
		Items      []struct {
			FullName    string   `json:"full_name"`
			HTMLURL     string   `json:"html_url"`
			Description string   `json:"description"`
			Stars       int      `json:"stargazers_count"`
			Language    string   `json:"language"`
			UpdatedAt   string   `json:"updated_at"`
			PushedAt    string   `json:"pushed_at"`
			Topics      []string `json:"topics"`
			License     *struct {
				SPDXID string `json:"spdx_id"`
			} `json:"license"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Items))
	for _, r := range resp.Items {
		snippet := r.Description
		meta := []string{}
		if r.Language != "" {
			meta = append(meta, "lang="+r.Language)
		}
		if r.Stars > 0 {
			meta = append(meta, fmt.Sprintf("stars=%d", r.Stars))
		}
		if len(meta) > 0 {
			if snippet != "" {
				snippet += " — "
			}
			snippet += strings.Join(meta, " ")
		}
		out = append(out, Item{
			Title:       r.FullName,
			URL:         r.HTMLURL,
			Snippet:     snippet,
			Score:       1.0,
			Source:      "github",
			FreshnessAt: parseRFC3339OrZero(r.PushedAt),
			Raw: map[string]any{
				"stars":    r.Stars,
				"language": r.Language,
				"topics":   r.Topics,
			},
		})
	}
	return out, nil
}

// parseOSV handles GET /v1/vulns/{id} (single vuln response).
func parseOSV(body []byte) ([]Item, error) {
	var v struct {
		ID        string   `json:"id"`
		Summary   string   `json:"summary"`
		Details   string   `json:"details"`
		Aliases   []string `json:"aliases"`
		Modified  string   `json:"modified"`
		Published string   `json:"published"`
		Severity  []struct {
			Type  string `json:"type"`
			Score string `json:"score"`
		} `json:"severity"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, err
	}
	snippet := v.Summary
	if snippet == "" {
		snippet = v.Details
	}
	if len(snippet) > 240 {
		snippet = snippet[:240] + "..."
	}
	freshness := parseRFC3339OrZero(v.Modified)
	if freshness.IsZero() {
		freshness = parseRFC3339OrZero(v.Published)
	}
	return []Item{{
		Title:       v.ID,
		URL:         "https://osv.dev/vulnerability/" + v.ID,
		Snippet:     snippet,
		Score:       1.0,
		Source:      "osv.dev",
		FreshnessAt: freshness,
		Raw: map[string]any{
			"aliases":  v.Aliases,
			"severity": v.Severity,
		},
	}}, nil
}

func parseNVD(body []byte) ([]Item, error) {
	var resp struct {
		Vulnerabilities []struct {
			CVE struct {
				ID           string `json:"id"`
				Descriptions []struct {
					Lang  string `json:"lang"`
					Value string `json:"value"`
				} `json:"descriptions"`
			} `json:"cve"`
		} `json:"vulnerabilities"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Vulnerabilities) == 0 {
		return nil, fmt.Errorf("nvd returned no vulnerabilities")
	}
	out := make([]Item, 0, len(resp.Vulnerabilities))
	for _, v := range resp.Vulnerabilities {
		snippet := ""
		for _, d := range v.CVE.Descriptions {
			if d.Lang == "en" {
				snippet = d.Value
				break
			}
		}
		out = append(out, Item{
			Title:   v.CVE.ID,
			URL:     "https://nvd.nist.gov/vuln/detail/" + v.CVE.ID,
			Snippet: snippet,
			Score:   1.0,
			Source:  "nvd",
		})
	}
	return out, nil
}

func parseRDAP(body []byte) ([]Item, error) {
	// RDAP is a self-describing JSON; we only surface the most useful
	// fields. A full RDAP client would emit the whole structure.
	var resp struct {
		LDHName string   `json:"ldhName"`
		Handle  string   `json:"handle"`
		Status  []string `json:"status"`
		Events  []struct {
			EventAction string `json:"eventAction"`
			EventDate   string `json:"eventDate"`
		} `json:"events"`
		Entities []struct {
			Roles      []string `json:"roles"`
			VCardArray []any    `json:"vcardArray"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	snippet := fmt.Sprintf("handle=%s status=%v events=%d entities=%d",
		resp.Handle, resp.Status, len(resp.Events), len(resp.Entities))
	return []Item{{
		Title:   resp.LDHName,
		URL:     "https://rdap.org/domain/" + resp.LDHName,
		Snippet: snippet,
		Score:   1.0,
		Source:  "rdap.org",
	}}, nil
}

func parseDoH(body []byte) ([]Item, error) {
	// Cloudflare DoH returns JSON when Accept: application/dns-json.
	// We surface the Answer section.
	var resp struct {
		Status int `json:"Status"`
		Answer []struct {
			Name string `json:"name"`
			Type int    `json:"type"`
			TTL  int    `json:"TTL"`
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Answer))
	for _, a := range resp.Answer {
		out = append(out, Item{
			Title:   fmt.Sprintf("%s (type=%d)", a.Name, a.Type),
			URL:     "",
			Snippet: a.Data,
			Score:   1.0,
			Source:  "cloudflare-doh",
		})
	}
	return out, nil
}

func parseGoogleDoH(body []byte) ([]Item, error) {
	// Same shape as Cloudflare's response.
	return parseDoH(body)
}

func parseCrtsh(body []byte) ([]Item, error) {
	// crt.sh returns JSON array of cert records.
	var records []struct {
		ID         int64  `json:"id"`
		IssuerName string `json:"issuer_name"`
		CommonName string `json:"common_name"`
		NotBefore  string `json:"not_before"`
		NotAfter   string `json:"not_after"`
	}
	if err := json.Unmarshal(body, &records); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(records))
	for _, r := range records {
		out = append(out, Item{
			Title:   r.CommonName,
			URL:     fmt.Sprintf("https://crt.sh/?id=%d", r.ID),
			Snippet: fmt.Sprintf("issuer=%s not_before=%s not_after=%s", r.IssuerName, r.NotBefore, r.NotAfter),
			Score:   1.0,
			Source:  "crt.sh",
		})
	}
	return out, nil
}

func parseIPAPI(body []byte) ([]Item, error) {
	var resp struct {
		Status      string  `json:"status"`
		Country     string  `json:"country"`
		CountryCode string  `json:"countryCode"`
		Region      string  `json:"regionName"`
		City        string  `json:"city"`
		Zip         string  `json:"zip"`
		Lat         float64 `json:"lat"`
		Lon         float64 `json:"lon"`
		Timezone    string  `json:"timezone"`
		ISP         string  `json:"isp"`
		Org         string  `json:"org"`
		AS          string  `json:"as"`
		Query       string  `json:"query"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	snippet := fmt.Sprintf("%s, %s, %s — ISP=%s Org=%s AS=%s",
		resp.City, resp.Region, resp.Country, resp.ISP, resp.Org, resp.AS)
	return []Item{{
		Title:   resp.Query,
		URL:     fmt.Sprintf("https://ip-api.com/#%s", resp.Query),
		Snippet: snippet,
		Score:   1.0,
		Source:  "ip-api.com",
		Raw: map[string]any{
			"lat": resp.Lat, "lon": resp.Lon,
			"country_code": resp.CountryCode,
			"timezone":     resp.Timezone,
		},
	}}, nil
}

func parseRIPE(body []byte) ([]Item, error) {
	// RIPE Stat whois returns "records" as an array of ARRAYS OF
	// OBJECTS, each object being {"key":...,"value":...,"comment":...}:
	//
	//	"records":[[{"key":"NetRange","value":"8.8.8.0 - 8.8.8.255",...},
	//	            {"key":"CIDR","value":"8.8.8.0/24",...}, ...]]
	//
	// The previous parser typed rows as [][]string, which always
	// failed with "cannot unmarshal object into .data.records.0.0 of
	// type string".
	var resp struct {
		Data struct {
			Records [][]struct {
				Key         string `json:"key"`
				Value       string `json:"value"`
				Comment     string `json:"comment"`
				DetailsLink string `json:"details_link"`
			} `json:"records"`
			Columns []string `json:"columns"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Data.Records))
	for _, row := range resp.Data.Records {
		kv := map[string]string{}
		for _, cell := range row {
			kv[cell.Key] = cell.Value
		}
		// Pick the most identifying prefix for the title.
		title := firstNonEmpty(kv["inetnum"], kv["NetRange"], kv["CIDR"], kv["netname"], kv["NetName"])
		if title == "" {
			continue
		}
		snippet := fmt.Sprintf("inetnum=%s netname=%s descr=%s country=%s",
			kv["inetnum"], kv["netname"], kv["descr"], kv["country"])
		if kv["NetRange"] != "" {
			snippet += " range=" + kv["NetRange"]
		}
		if kv["CIDR"] != "" {
			snippet += " cidr=" + kv["CIDR"]
		}
		out = append(out, Item{
			Title:   title,
			URL:     "https://apps.db.ripe.net/db-web-ui/query",
			Snippet: snippet,
			Score:   1.0,
			Source:  "ripe",
			Raw:     map[string]any{"row": kv},
		})
	}
	return out, nil
}

func parseAbuseCH(body []byte) ([]Item, error) {
	// URLhaus /v1/host/ returns the host record plus the list of
	// malicious URLs observed on it. Field names verified against the
	// live API on 2026-11-13: the timestamp is `date_added` (not
	// `dateadded`), and the host-level summary lives at the top level
	// rather than under a `query` key.
	var resp struct {
		QueryStatus string `json:"query_status"`
		URLhausRef  string `json:"urlhaus_reference"`
		Host        string `json:"host"`
		FirstSeen   string `json:"firstseen"`
		// url_count is a STRING in the live API ("2"), not a number.
		URLCount string `json:"url_count"`
		URLs     []struct {
			ID        string   `json:"id"`
			URL       string   `json:"url"`
			URLStatus string   `json:"url_status"`
			DateAdded string   `json:"date_added"`
			Threat    string   `json:"threat"`
			Tags      []string `json:"tags"`
			Reporter  string   `json:"reporter"`
		} `json:"urls"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	// A non-"ok" query_status means the API answered but found nothing
	// (or rejected the query). Surface that as an error rather than
	// silently returning zero items.
	if resp.QueryStatus != "" && resp.QueryStatus != "ok" && resp.QueryStatus != "no_results" {
		return nil, fmt.Errorf("urlhaus query_status=%s", resp.QueryStatus)
	}

	out := make([]Item, 0, len(resp.URLs)+1)

	// The host-level record is itself a useful finding: it tells the
	// caller the host is tracked at all, how many URLs were seen, and
	// when it was first observed.
	if resp.Host != "" {
		out = append(out, Item{
			Title:   resp.Host,
			URL:     firstNonEmpty(resp.URLhausRef, "https://urlhaus.abuse.ch/host/"+resp.Host+"/"),
			Snippet: fmt.Sprintf("host=%s url_count=%s firstseen=%s", resp.Host, resp.URLCount, resp.FirstSeen),
			Score:   1.0,
			Source:  "abuse.ch",
			Raw: map[string]any{
				"host":      resp.Host,
				"url_count": resp.URLCount,
				"firstseen": resp.FirstSeen,
			},
		})
	}

	for _, u := range resp.URLs {
		out = append(out, Item{
			Title:   u.URL,
			URL:     "https://urlhaus.abuse.ch/url/" + u.ID + "/",
			Snippet: fmt.Sprintf("status=%s threat=%s tags=%v", u.URLStatus, u.Threat, u.Tags),
			Score:   1.0,
			Source:  "abuse.ch",
			Raw: map[string]any{
				"id":         u.ID,
				"url_status": u.URLStatus,
				"threat":     u.Threat,
				"tags":       u.Tags,
				"reporter":   u.Reporter,
			},
		})
	}
	return out, nil
}

// firstNonEmpty returns the first non-empty string, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseOTX(body []byte) ([]Item, error) {
	// OTX /api/v1/indicators/{type}/{value}/general returns a single
	// indicator record. The old /search/limited endpoint returned a
	// `results` array and is no longer served (404).
	var resp struct {
		Indicator   string `json:"indicator"`
		Type        string `json:"type"`
		TypeTitle   string `json:"type_title"`
		Description string `json:"description"`
		Reputation  int    `json:"reputation"`
		Validation  []struct {
			Source string `json:"source"`
			Valid  bool   `json:"valid"`
		} `json:"validation"`
		Malware []struct {
			DetectionRatio string `json:"detection_ratio"`
			Family         string `json:"family"`
		} `json:"malware"`
		Tags     []string `json:"tags"`
		Created  string   `json:"created"`
		Modified string   `json:"modified"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.Indicator == "" {
		return nil, fmt.Errorf("otx: response contained no indicator")
	}

	snippet := resp.Description
	if snippet == "" {
		snippet = fmt.Sprintf("type=%s reputation=%d tags=%v", resp.Type, resp.Reputation, resp.Tags)
	}

	out := []Item{{
		Title:   resp.Indicator,
		URL:     "https://otx.alienvault.com/indicator/" + resp.Indicator,
		Snippet: snippet,
		Score:   1.0,
		Source:  "otx",
		Raw: map[string]any{
			"type":       resp.Type,
			"reputation": resp.Reputation,
			"tags":       resp.Tags,
			"validation": resp.Validation,
			"malware":    resp.Malware,
		},
	}}

	// Malware families attached to the indicator are separate findings.
	for _, m := range resp.Malware {
		if m.Family == "" {
			continue
		}
		out = append(out, Item{
			Title:   m.Family,
			URL:     "https://otx.alienvault.com/indicator/" + resp.Indicator,
			Snippet: fmt.Sprintf("malware family detected on %s (detection_ratio=%s)", resp.Indicator, m.DetectionRatio),
			Score:   1.0,
			Source:  "otx",
		})
	}
	return out, nil
}

func parseHIBP(body []byte) ([]Item, error) {
	var breaches []struct {
		Name        string `json:"Name"`
		Domain      string `json:"Domain"`
		BreachDate  string `json:"BreachDate"`
		PwnCount    int    `json:"PwnCount"`
		Description string `json:"Description"`
	}
	if err := json.Unmarshal(body, &breaches); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(breaches))
	for _, b := range breaches {
		out = append(out, Item{
			Title:   b.Name,
			URL:     "https://haveibeenpwned.com/breach/" + b.Name,
			Snippet: fmt.Sprintf("%s — %s (pwn count: %d)", b.BreachDate, b.Domain, b.PwnCount),
			Score:   1.0,
			Source:  "hibp",
		})
	}
	return out, nil
}

// parseAhmia parses Ahmia's HTML search results.
//
// The real markup (verified against ahmia-site's
// ahmia/templates/tor_results.html on 2026-11-13) is:
//
//	<ol class="searchResults">
//	  <li class="result">
//	    <h4><a href="/onion-redirect/?search_term=...&redirect_url=http://xxx.onion/">Title</a></h4>
//	    <p>description</p>
//	    <cite>domain</cite>
//	    &mdash; <span class="lastSeen" data-timestamp="...">...</span>
//	  </li>
//	</ol>
//
// Two things broke the previous implementation:
//
//  1. The result anchor href is a REDIRECT URL whose real target
//     lives in the `redirect_url` query parameter. The old parser
//     used the href verbatim, so every item pointed at ahmia.fi
//     instead of the .onion service.
//  2. The old regex required `<li class="result">` to be immediately
//     followed by `<a ...>`, but the title anchor is nested inside
//     an `<h4>`, so the non-greedy pattern never matched and the
//     parser returned zero items against every real response.
func parseAhmia(body []byte) ([]Item, error) {
	html := string(body)

	// A missing or stale rolling token makes Ahmia redirect the
	// search to the homepage, so the response is the landing page
	// rather than a result list. Detect that explicitly: otherwise the
	// caller would see "0 results" and conclude the query has no
	// matches, which is a materially wrong answer.
	//
	// The discriminator is the presence of the results list. The
	// search form (id="searchForm") is rendered in the page header of
	// BOTH the landing page and the results page, so it cannot be
	// used to tell them apart.
	if !strings.Contains(html, `class="searchResults"`) {
		return nil, fmt.Errorf("ahmia: search requires a rolling token; " +
			"the response was the homepage (token fetch failed or was stale)")
	}

	// Each result is an <li class="result"> block. Split on it so
	// each block can be parsed independently.
	blocks := ahmiaResultBlocks(html)
	out := make([]Item, 0, len(blocks))

	for _, blk := range blocks {
		// Title anchor: <h4><a href="...">Title</a></h4>
		anchorRe := regexp.MustCompile(`(?s)<h4>\s*<a\s[^>]*href="([^"]+)"[^>]*>(.*?)</a>\s*</h4>`)
		m := anchorRe.FindStringSubmatch(blk)
		if m == nil {
			// Fall back to a looser anchor match for markup variants.
			anchorRe = regexp.MustCompile(`(?s)<a\s[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
			m = anchorRe.FindStringSubmatch(blk)
			if m == nil {
				continue
			}
		}
		href := strings.TrimSpace(m[1])
		title := stripTags(strings.TrimSpace(m[2]))

		// Resolve the onion-redirect wrapper to the real .onion URL.
		realURL := ahmiaResolveRedirect(href)

		// Description: the first <p> after the title anchor.
		snippet := ""
		if pRe := regexp.MustCompile(`(?s)<p[^>]*>(.*?)</p>`); pRe != nil {
			pm := pRe.FindStringSubmatch(blk)
			if pm != nil {
				snippet = strings.Join(strings.Fields(stripTags(strings.TrimSpace(pm[1]))), " ")
			}
		}

		// Domain: <cite>...</cite>
		if cRe := regexp.MustCompile(`(?s)<cite[^>]*>(.*?)</cite>`); cRe != nil {
			cm := cRe.FindStringSubmatch(blk)
			if cm != nil {
				d := stripTags(strings.TrimSpace(cm[1]))
				if d != "" {
					snippet = strings.TrimSpace(snippet + " — " + d)
				}
			}
		}

		// Last-seen timestamp.
		var freshness time.Time
		if tsRe := regexp.MustCompile(`data-timestamp="([^"]+)"`); tsRe != nil {
			tsm := tsRe.FindStringSubmatch(blk)
			if tsm != nil {
				freshness = parseRFC3339OrZero(tsm[1])
			}
		}

		if title == "" && realURL == "" {
			continue
		}
		out = append(out, Item{
			Title:       title,
			URL:         realURL,
			Snippet:     snippet,
			Score:       1.0,
			Source:      "ahmia",
			FreshnessAt: freshness,
		})
	}
	return out, nil
}

// ahmiaResultBlocks splits the results page into one block per
// <li class="result"> element.
//
// Go's regexp package is RE2 and does NOT support lookahead, so the
// split is done by finding each block start and slicing up to the next
// one.
func ahmiaResultBlocks(html string) []string {
	startRe := regexp.MustCompile(`(?is)<li[^>]*class="result"[^>]*>`)
	locs := startRe.FindAllStringIndex(html, -1)
	out := make([]string, 0, len(locs))
	for i, loc := range locs {
		bodyStart := loc[1]
		bodyEnd := len(html)
		if i+1 < len(locs) {
			bodyEnd = locs[i+1][0]
		} else {
			// Last block: cut at the closing </ol> if present.
			if idx := strings.Index(html[bodyStart:], "</ol>"); idx >= 0 {
				bodyEnd = bodyStart + idx
			}
		}
		out = append(out, html[bodyStart:bodyEnd])
	}
	return out
}

// ahmiaResolveRedirect unwraps Ahmia's onion-redirect link and
// returns the real .onion URL. If the href is not a redirect, it is
// returned unchanged.
func ahmiaResolveRedirect(href string) string {
	if href == "" {
		return ""
	}
	// Relative redirect: /onion-redirect/?search_term=...&redirect_url=...
	if strings.Contains(href, "redirect_url=") {
		if u, err := url.Parse(href); err == nil {
			if real := u.Query().Get("redirect_url"); real != "" {
				if decoded, err := url.QueryUnescape(real); err == nil {
					return decoded
				}
				return real
			}
		}
		// Fallback: pull the parameter out of the raw string.
		if i := strings.Index(href, "redirect_url="); i >= 0 {
			rest := href[i+len("redirect_url="):]
			if j := strings.IndexAny(rest, "&"); j >= 0 {
				rest = rest[:j]
			}
			if decoded, err := url.QueryUnescape(rest); err == nil {
				return decoded
			}
			return rest
		}
	}
	return href
}

func parseNominatim(body []byte) ([]Item, error) {
	var resp []struct {
		DisplayName string `json:"display_name"`
		Lat         string `json:"lat"`
		Lon         string `json:"lon"`
		Type        string `json:"type"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp))
	for _, r := range resp {
		out = append(out, Item{
			Title:   r.DisplayName,
			URL:     fmt.Sprintf("https://www.openstreetmap.org/?lat=%s&lon=%s", r.Lat, r.Lon),
			Snippet: fmt.Sprintf("%s @ %s,%s", r.Type, r.Lat, r.Lon),
			Score:   1.0,
			Source:  "osm-nominatim",
		})
	}
	return out, nil
}

func parseGDELT(body []byte) ([]Item, error) {
	// GDELT DOC 2.0 with format=json returns an OBJECT with an
	// "articles" array, not a bare array. Verified against the live
	// API on 2026-11-13:
	//
	//	{"articles":[{"url":"...","title":"...","seendate":"20261002T024500Z",...}]}
	//
	// The previous parser unmarshalled into a top-level slice, which
	// always failed with "cannot unmarshal object into Go value of
	// type []struct {...}".
	var resp struct {
		Articles []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			SeenDate    string `json:"seendate"`
			SocialImage string `json:"socialimage"`
			Domain      string `json:"domain"`
			Language    string `json:"language"`
			Source      string `json:"sourcecountry"`
		} `json:"articles"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Articles))
	for _, r := range resp.Articles {
		snippet := "seen=" + r.SeenDate
		if r.Domain != "" {
			snippet += " domain=" + r.Domain
		}
		if r.Language != "" {
			snippet += " lang=" + r.Language
		}
		out = append(out, Item{
			Title: r.Title, URL: r.URL, Snippet: snippet,
			Score: 1.0, Source: "gdelt",
			FreshnessAt: parseGDELTSeenDate(r.SeenDate),
		})
	}
	return out, nil
}

// parseGDELTSeenDate converts GDELT's compact "20060102T150405Z"
// timestamp into a time.Time. Returns the zero time when the field is
// absent or malformed.
func parseGDELTSeenDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse("20060102T150405Z", s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func parseWayback(body []byte) ([]Item, error) {
	// The CDX API with output=json returns a JSON ARRAY whose FIRST
	// ROW is a header describing the columns:
	//
	//	[["urlkey","timestamp","original","mimetype","statuscode","digest","length"],
	//	 ["com,example)/","20020120142510","http://example.com:80/",...]]
	//
	// Verified against the live API on 2026-11-13. The previous
	// implementation split the body on newlines and unmarshalled each
	// LINE as a JSON array, which never matches because the whole
	// payload is ONE array — so it parsed zero items from every real
	// response. The bug was hidden by a fixture whose body was the
	// empty array "[]" (a "no match" reply), which the old code also
	// parsed as zero items.
	out := []Item{}

	var rows [][]string
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return out, nil
	}

	// Map header column names to indices so we don't depend on the
	// column order (CDX supports fl= to reorder fields).
	header := rows[0]
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	tsIdx, ok := idx["timestamp"]
	if !ok {
		tsIdx = 1
	}
	origIdx, ok := idx["original"]
	if !ok {
		origIdx = 2
	}
	mimeIdx, hasMime := idx["mimetype"]
	statusIdx, hasStatus := idx["statuscode"]

	for _, row := range rows[1:] {
		if len(row) <= origIdx {
			continue
		}
		original := row[origIdx]
		if original == "" {
			continue
		}
		ts := ""
		if len(row) > tsIdx {
			ts = row[tsIdx]
		}
		snippet := "captured=" + ts
		if hasMime && len(row) > mimeIdx {
			snippet += " mime=" + row[mimeIdx]
		}
		if hasStatus && len(row) > statusIdx {
			snippet += " status=" + row[statusIdx]
		}
		out = append(out, Item{
			Title:   original,
			URL:     fmt.Sprintf("https://web.archive.org/web/%s/%s", ts, original),
			Snippet: snippet,
			Score:   1.0,
			Source:  "wayback",
			Raw: map[string]any{
				"timestamp": ts,
				"original":  original,
			},
		})
	}
	return out, nil
}

// stripTags removes HTML tags from s. Lightweight; for production use
// ammonia/bluemonday.
var tagRe = regexp.MustCompile(`<[^>]+>`)

func stripTags(s string) string {
	return tagRe.ReplaceAllString(s, "")
}
