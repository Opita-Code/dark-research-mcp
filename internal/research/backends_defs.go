package research

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"regexp"
	"strings"
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
			Name: "searxng", BaseURL: "https://searx.be/search",
			Method: "GET", Free: true, OpenSource: true, Weight: 2,
			Confidence: 0.75, LangHint: "en",
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
			Name: "openalex", BaseURL: "https://api.openalex.org/works",
			Method: "GET", Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.92, LangHint: "en", FreshnessField: "publication_date",
			RateLimitMs: 100,
			Parse:       parseOpenAlex,
		},
		{
			Name: "arxiv", BaseURL: "http://export.arxiv.org/api/query",
			Method: "GET", Free: true, OpenSource: true, Weight: 2,
			Confidence: 0.95, LangHint: "en", FreshnessField: "published",
			RateLimitMs: 1000,
			Parse:       parseArxiv,
		},
		{
			Name: "semanticscholar", BaseURL: "https://api.semanticscholar.org/graph/v1/paper/search",
			Method: "GET", Auth: "S2_API_KEY", Weight: 3,
			Confidence: 0.9, LangHint: "en", FreshnessField: "year",
			RateLimitMs: 1000,
			Parse:       parseSemanticScholar,
		},
	}
}

// codeBackends: crates.io (open) → npm registry (open) → GitHub (free tier).
func codeBackends() []Backend {
	return []Backend{
		{
			Name: "cratesio", BaseURL: "https://crates.io/api/v1/crates",
			Method: "GET", Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.9, LangHint: "en", FreshnessField: "updated_at",
			RateLimitMs: 100,
			Parse:       parseCratesIO,
		},
		{
			Name: "npm", BaseURL: "https://registry.npmjs.com/-/v1/search",
			Method: "GET", Free: true, OpenSource: true, Weight: 2,
			Confidence: 0.88, LangHint: "en", FreshnessField: "date",
			RateLimitMs: 100,
			Parse:       parseNPM,
		},
		{
			Name: "github", BaseURL: "https://api.github.com/search/repositories",
			Method: "GET", Auth: "GITHUB_TOKEN", Weight: 3,
			Confidence: 0.85, LangHint: "en", FreshnessField: "updated_at",
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

// dnsBackends: Cloudflare DoH → Google DoH → Quad9 DoH.
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
				return "https://cloudflare-dns.com/dns-query?name=" + url.QueryEscape(domain) + "&type=A"
			},
			RateLimitMs: 100,
			Parse:       parseDoH,
		},
		{
			Name: "google-doh", BaseURL: "https://dns.google/resolve",
			Method: "GET", Free: true, OpenSource: true, Weight: 2,
			Confidence: 0.92, LangHint: "en",
			URLForQuery: func(q string) string {
				return "https://dns.google/resolve?name=" + url.QueryEscape(q) + "&type=A"
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
			Name: "crtsh", BaseURL: "https://crt.sh/",
			Method: "GET", Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.95, LangHint: "en", FreshnessField: "not_before",
			URLForQuery: func(q string) string {
				return "https://crt.sh/?q=" + url.QueryEscape(q) + "&output=json"
			},
			RateLimitMs: 1000,
			Parse:       parseCrtsh,
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

// threatBackends: abuse.ch (free, no auth) → AlienVault OTX (free w/ key).
func threatBackends() []Backend {
	return []Backend{
		{
			Name: "abusech", BaseURL: "https://urlhaus-api.abuse.ch/v1/host/",
			Method: "GET", Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.92, LangHint: "en", FreshnessField: "dateadded",
			URLForQuery: func(q string) string {
				return "https://urlhaus-api.abuse.ch/v1/host/" + url.PathEscape(q)
			},
			RateLimitMs: 500,
			Parse:       parseAbuseCH,
		},
		{
			Name: "otx", BaseURL: "https://otx.alienvault.com/api/v1/search/limited",
			Method: "GET", Auth: "OTX_API_KEY", Weight: 2,
			Confidence: 0.88, LangHint: "en",
			RateLimitMs: 500,
			Parse:       parseOTX,
		},
	}
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
func darkBackends() []Backend {
	return []Backend{
		{
			Name: "ahmia", BaseURL: "https://ahmia.fi/search/",
			Method: "GET", Free: true, OpenSource: true, Weight: 1,
			Confidence: 0.7, LangHint: "en", FreshnessField: "last_seen",
			URLForQuery: func(q string) string {
				return "https://ahmia.fi/search/?q=" + url.QueryEscape(q)
			},
			RateLimitMs: 1000,
			Parse:       parseAhmia,
		},
	}
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
			RateLimitMs: 500,
			Parse:       parseGDELT,
		},
		{
			Name: "wayback", BaseURL: "https://web.archive.org/cdx/search/cdx",
			Method: "GET", Free: true, OpenSource: true, Weight: 2,
			Confidence: 0.85, LangHint: "en", FreshnessField: "timestamp",
			URLForQuery: func(q string) string {
				return "https://web.archive.org/cdx/search/cdx?url=" + url.QueryEscape(q) + "&output=json&limit=20"
			},
			RateLimitMs: 500,
			Parse:       parseWayback,
		},
	}
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

func parseSearXNG(body []byte) ([]Item, error) {
	var resp struct {
		Results []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
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
			ID              string `json:"id"`
			Title           string `json:"title"`
			PublicationDate string `json:"publication_date"`
			Doi             string `json:"doi"`
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
	var resp struct {
		Data []struct {
			PaperID string `json:"paperId"`
			Title   string `json:"title"`
			URL     string `json:"url"`
			Abstract string `json:"abstract"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Data))
	for _, p := range resp.Data {
		out = append(out, Item{
			Title: p.Title, URL: p.URL, Snippet: p.Abstract,
			Score: 1.0, Source: "semanticscholar",
		})
	}
	return out, nil
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
		Items []struct {
			FullName string  `json:"full_name"`
			HTMLURL  string  `json:"html_url"`
			Description string `json:"description"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Items))
	for _, r := range resp.Items {
		out = append(out, Item{
			Title: r.FullName, URL: r.HTMLURL, Snippet: r.Description,
			Score: 1.0, Source: "github",
		})
	}
	return out, nil
}

// parseOSV handles GET /v1/vulns/{id} (single vuln response).
func parseOSV(body []byte) ([]Item, error) {
	var v struct {
		ID        string `json:"id"`
		Summary   string `json:"summary"`
		Details   string `json:"details"`
		Aliases   []string `json:"aliases"`
		Modified  string `json:"modified"`
		Published string `json:"published"`
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
		LDHName    string `json:"ldhName"`
		Handle     string `json:"handle"`
		Status     []string `json:"status"`
		Events    []struct {
			EventAction string `json:"eventAction"`
			EventDate   string `json:"eventDate"`
		} `json:"events"`
		Entities []struct {
			Roles    []string `json:"roles"`
			VCardArray []any `json:"vcardArray"`
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
		Status    string  `json:"status"`
		Country   string  `json:"country"`
		CountryCode string `json:"countryCode"`
		Region    string  `json:"regionName"`
		City      string  `json:"city"`
		Zip       string  `json:"zip"`
		Lat       float64 `json:"lat"`
		Lon       float64 `json:"lon"`
		Timezone  string  `json:"timezone"`
		ISP       string  `json:"isp"`
		Org       string  `json:"org"`
		AS        string  `json:"as"`
		Query     string  `json:"query"`
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
	var resp struct {
		Data struct {
			Records [][]string `json:"records"`
			Columns []string   `json:"columns"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Data.Records))
	for _, row := range resp.Data.Records {
		kv := map[string]string{}
		for i, col := range resp.Data.Columns {
			if i < len(row) {
				kv[col] = row[i]
			}
		}
		out = append(out, Item{
			Title:   kv["inetnum"],
			URL:     "https://apps.db.ripe.net/db-web-ui/query",
			Snippet: fmt.Sprintf("inetnum=%s netname=%s descr=%s country=%s",
				kv["inetnum"], kv["netname"], kv["descr"], kv["country"]),
			Score:   1.0,
			Source:  "ripe",
			Raw:     map[string]any{"row": kv},
		})
	}
	return out, nil
}

func parseAbuseCH(body []byte) ([]Item, error) {
	// URLhaus API returns a list of URLs for the queried host.
	var resp struct {
		Query    string `json:"query"`
		URLs     []struct {
			ID          string `json:"id"`
			URL         string `json:"url"`
			URLStatus   string `json:"url_status"`
			DateAdded   string `json:"dateadded"`
			Threat      string `json:"threat"`
			Tags        []string `json:"tags"`
			Reporter    string `json:"reporter"`
		} `json:"urls"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.URLs))
	for _, u := range resp.URLs {
		out = append(out, Item{
			Title:   u.URL,
			URL:     "https://urlhaus.abuse.ch/url/" + u.ID + "/",
			Snippet: fmt.Sprintf("status=%s threat=%s tags=%v", u.URLStatus, u.Threat, u.Tags),
			Score:   1.0,
			Source:  "abuse.ch",
		})
	}
	return out, nil
}

func parseOTX(body []byte) ([]Item, error) {
	var resp struct {
		Results []struct {
			ID          string `json:"id"`
			Indicator   string `json:"indicator"`
			Type        string `json:"type"`
			Description string `json:"description"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp.Results))
	for _, r := range resp.Results {
		out = append(out, Item{
			Title:   r.Indicator,
			URL:     "https://otx.alienvault.com/indicator/" + r.Indicator,
			Snippet: fmt.Sprintf("type=%s %s", r.Type, r.Description),
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

func parseAhmia(body []byte) ([]Item, error) {
	// Ahmia.fi returns HTML. We grep for result links.
	re := regexp.MustCompile(`<li[^>]*class="result"[^>]*>.*?<a[^>]*href="([^"]+)"[^>]*>(.*?)</a>.*?<p>(.*?)</p>`)
	matches := re.FindAllStringSubmatch(string(body), 30)
	out := make([]Item, 0, len(matches))
	for _, m := range matches {
		out = append(out, Item{
			Title:   stripTags(m[2]),
			URL:     m[1],
			Snippet: stripTags(m[3]),
			Score:   1.0,
			Source:  "ahmia",
		})
	}
	return out, nil
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
	// GDELT returns JSON; we surface the title + URL.
	var resp []struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		SeenDate    string `json:"seendate"`
		SocialImage string `json:"socialimage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(resp))
	for _, r := range resp {
		out = append(out, Item{
			Title: r.Title, URL: r.URL, Snippet: "seen=" + r.SeenDate,
			Score: 1.0, Source: "gdelt",
		})
	}
	return out, nil
}

func parseWayback(body []byte) ([]Item, error) {
	// CDX returns JSON-lines: each line is a capture.
	out := []Item{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec []string
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if len(rec) < 3 {
			continue
		}
		// CDX fields: urlkey, timestamp, original, mimetype, ...
		ts, original := rec[1], rec[2]
		out = append(out, Item{
			Title:   original,
			URL:     fmt.Sprintf("https://web.archive.org/web/%s/%s", ts, original),
			Snippet: "captured=" + ts,
			Score:   1.0,
			Source:  "wayback",
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