package research

import (
	"time"
)

// Backend describes one OSINT data source. Backends are tried in
// Weight order; missing Auth env var skips the backend; HTTP errors
// trigger fallback to the next backend.
type Backend struct {
	// Name is a short identifier ("osv.dev", "openalex").
	Name string

	// BaseURL is the default API endpoint. Used when URLForQuery is nil.
	BaseURL string

	// URLForQuery returns the full URL to call for a given query. If
	// nil, the router uses BaseURL with `?q=<query>` appended.
	URLForQuery func(query string) string

	// Method is "GET" or "POST".
	Method string

	// Auth is the env var name that holds the API key. Empty means no auth.
	Auth string

	// Free means no payment required. OpenSource means the source code
	// is publicly available. The router prefers Free+OpenSource; paid is
	// only used as last resort.
	Free       bool
	OpenSource bool

	// Confidence is the typical reliability of items from this backend,
	// 0..1. Authoritative curated sources (OSV.dev, OpenAlex) score 0.9+;
	// scraping or generic search (DuckDuckGo HTML) score 0.6–0.7.
	Confidence float32

	// FreshnessField is the JSON field name that carries the item's
	// "as of" date (publication_date, last_seen, not_before, ...). When
	// set, the parser reads it and stamps Item.FreshnessAt.
	FreshnessField string

	// LangHint declares the dominant language of results; "" = mixed.
	LangHint string

	// Weight sorts the try order. Lower = tried first.
	Weight int

	// RateLimitMs is the minimum interval between calls per process.
	RateLimitMs int

	// Parse converts a backend-specific response body into normalized
	// Result items. The router calls this after each successful fetch.
	Parse func(body []byte) ([]Item, error)
}

// Item is one search result, normalized across all backends.
type Item struct {
	Title       string         `json:"title"`
	URL         string         `json:"url"`
	Snippet     string         `json:"snippet"`
	Score       float32        `json:"score"`
	Source      string         `json:"source"`
	FetchedAt   time.Time      `json:"fetched_at"`
	FreshnessAt time.Time      `json:"freshness_at,omitempty"`
	Confidence  float32        `json:"confidence"`
	Lang        string         `json:"lang,omitempty"`
	Raw         map[string]any `json:"raw,omitempty"`
}

// Result is what the router returns to the caller.
type Result struct {
	Intent        Intent `json:"intent"`
	Query         string `json:"query"`
	BackendUsed   string `json:"backend_used"`
	BackendsTried []string `json:"backends_tried"`
	Took          time.Duration `json:"took_ms"`
	Errors        []BackendError `json:"errors,omitempty"`
	Items         []Item `json:"results"`
}

// BackendError records a non-fatal failure so the agent can debug.
type BackendError struct {
	Backend string `json:"backend"`
	Err     string `json:"error"`
}

// Registry holds the backends per intent. It is the single source of
// truth for "where do I search for X".
type Registry struct {
	backends map[Intent][]Backend
}

// DefaultRegistry returns the production registry. All backends here
// are open-source or free at time of writing (2026-07); some need an
// API key (Auth != "") and are silently skipped if missing.
func DefaultRegistry() *Registry {
	r := &Registry{backends: map[Intent][]Backend{}}
	r.add(IntentWeb, webBackends()...)
	r.add(IntentAcademic, academicBackends()...)
	r.add(IntentCode, codeBackends()...)
	r.add(IntentCVE, cveBackends()...)
	r.add(IntentDomain, domainBackends()...)
	r.add(IntentDNS, dnsBackends()...)
	r.add(IntentCert, certBackends()...)
	r.add(IntentIP, ipBackends()...)
	r.add(IntentThreat, threatBackends()...)
	r.add(IntentEmail, emailBackends()...)
	r.add(IntentDark, darkBackends()...)
	r.add(IntentGeo, geoBackends()...)
	r.add(IntentNews, newsBackends()...)
	return r
}

func (r *Registry) add(intent Intent, bs ...Backend) {
	r.backends[intent] = append(r.backends[intent], bs...)
}

// For returns the backends for an intent, sorted by Weight ascending.
func (r *Registry) For(intent Intent) []Backend {
	out := make([]Backend, len(r.backends[intent]))
	copy(out, r.backends[intent])
	// Stable sort by Weight asc.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Weight < out[j-1].Weight; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Intents lists every intent known to the registry, in declaration order.
func (r *Registry) Intents() []Intent {
	out := make([]Intent, 0, len(r.backends))
	for k := range r.backends {
		out = append(out, k)
	}
	return out
}