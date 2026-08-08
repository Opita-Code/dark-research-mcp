// Package research orchestrates a query against the backend registry.
//
// v0.8.0: stop-at-first-success is replaced by multi-backend merge +
// cross-backend dedup. See Route() for the new algorithm.
package research

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dark-agents/research-mcp/internal/mem"
	"github.com/dark-agents/research-mcp/internal/safety"
	versionpkg "github.com/dark-agents/research-mcp/internal/version"
)

// Getenv reads an env var. Exposed as a var so tests can stub it.
var Getenv = os.Getenv

// Version is stamped into the User-Agent on every outbound request.
// It resolves from internal/version at call time: the release build
// injects the canonical git tag via -ldflags (`make release`), and a
// plain build falls back to "dev" or the module version. Backends
// that fingerprint by user-agent see one consistent string per binary.
func Version() string {
	return versionpkg.Resolve().Version
}

// MemSink is the interface dark-mem must satisfy for the router to
// persist runs. Defined here to avoid an import cycle (research does
// not import mem; mem imports nothing).
type MemSink interface {
	SaveRun(ctx context.Context, run *mem.ResearchRun) (int64, error)
}

// PersistenceReader is the interface for cache lookup (v0.8.1+).
// mem.Store satisfies it; tests can stub with an in-memory fake.
type PersistenceReader interface {
	LatestRunByQuery(ctx context.Context, query, intent string) (*mem.ResearchRun, error)
	ListResearchItems(ctx context.Context, runID int64, source string, limit int) ([]mem.Item, error)
}

// Router orchestrates a query against the registry.
type Router struct {
	reg     *Registry
	http    *http.Client
	lastHit map[string]time.Time // backend name → last call time
	rateMu  sync.Mutex           // guards lastHit (concurrent Route() calls)
	mem     MemSink              // optional; nil = no persistence
	session string               // session id stamped on saved runs

	// MemStore is the full persistence layer used for persistence-
	// aware recall (v0.8.1+). Optional; nil disables caching.
	// Distinct from `mem` (MemSink) because cache lookup needs more
	// methods than SaveRun. Both can be nil; the router still works.
	MemStore PersistenceReader

	// LLMClient (v0.8.1+) drives optional result synthesis. Optional;
	// nil means synthesis is skipped (the router never errors out).
	LLMClient LLMClient

	// MaxBackends caps how many backends are tried per Route() call.
	// 0 (zero value) means "all backends for the intent". Useful for
	// callers that want a quick-first-answer behavior even though the
	// router would normally merge across all backends.
	MaxBackends int

	// MaxItems caps the number of items returned after dedup.
	// 0 (zero value) defaults to 20. Callers can set higher (e.g.
	// dark_research_multi) or lower (e.g. a quick-preview tool).
	MaxItems int

	// EnableCache (v0.8.1+) toggles persistence-aware recall. When
	// true, the router consults MemStore.LatestRunByQuery before
	// fanning out to backends; if the most recent run for (query,
	// intent) is within TTL(intent), the cached items are returned
	// and BackendUsed is stamped "cache". When false (default),
	// every call hits the network. Set this when wiring the router.
	EnableCache bool

	// EnableSynthesize (v0.8.1+) toggles optional LLM synthesis of
	// the multi-backend result. When true and LLMClient != nil and
	// SDD_LLM_API_KEY is set, the router calls Synthesize after the
	// backend fan-out and populates Result.Summary. Failures degrade
	// gracefully (Summary stays empty, no error propagated to caller).
	EnableSynthesize bool
}

// NewRouter builds a router with the given registry.
func NewRouter(reg *Registry, hc *http.Client) *Router {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Router{reg: reg, http: hc, lastHit: map[string]time.Time{}, MaxItems: 20}
}

// SetMemStore wires the full persistence layer used for cache
// lookup. Optional; pass nil to disable caching.
func (r *Router) SetMemStore(s PersistenceReader) { r.MemStore = s }

// SetEnableCache toggles persistence-aware recall on/off.
func (r *Router) SetEnableCache(b bool) { r.EnableCache = b }

// SetLLMClient wires the optional LLM client used for result
// synthesis. Optional; pass nil to disable synthesis.
func (r *Router) SetLLMClient(c LLMClient) { r.LLMClient = c }

// SetEnableSynthesize toggles LLM synthesis on/off.
func (r *Router) SetEnableSynthesize(b bool) { r.EnableSynthesize = b }

// SetMem wires a MemSink so successful runs get persisted.
// Pass nil to disable persistence.
func (r *Router) SetMem(m MemSink) { r.mem = m }

// SetSession stamps a session id on subsequent persisted runs.
func (r *Router) SetSession(s string) { r.session = s }

// Route runs a query against the appropriate intent's backends,
// merges results across multiple backends, and deduplicates by
// canonical URL.
//
// v0.8.0 behavior (changed from v0.7.x):
//
//   - Iterate all (or up to MaxBackends) backends for the intent.
//   - Each successful backend contributes its items; failed backends
//     record an error but don't abort.
//   - All collected items are merged via DedupItems (see dedup.go).
//   - Confidence is preserved as max() across corroborating sources;
//     Source field shows "backend1+backend2" when corroborated.
//   - Result is capped to MaxItems (default 20).
//   - BackendUsed is the first successful backend (for audit); the
//     other contributors show in BackendsTried.
//
// Stop-at-first-success is gone — every reachable backend is queried.
// Set MaxBackends=1 to restore the legacy behavior.
func (r *Router) Route(ctx context.Context, query string, intentHint Intent) (*Result, error) {
	intent := intentHint
	if intent == "" {
		intent = Classify(query)
	}

	started := time.Now()
	res := &Result{
		Intent: intent,
		Query:  query,
	}

	// v0.8.1: persistence-aware recall. Before fanning out to backends,
	// consult MemStore for the most recent run matching (query, intent).
	// If it's within TTL(intent), return cached items + stamp BackendUsed
	// "cache" so the caller can tell no HTTP was made.
	if r.EnableCache && r.MemStore != nil {
		maxItems := r.MaxItems
		if maxItems <= 0 {
			maxItems = 20
		}
		cached, hit, err := LookupCached(ctx, r.MemStore, intent, query, maxItems)
		if err != nil {
			// Cache miss is non-fatal; log to stderr but proceed to
			// the normal backend fan-out.
			fmt.Fprintf(os.Stderr, "research: cache lookup failed: %v\n", err)
		} else if hit {
			res.Items = cached
			res.BackendUsed = "cache"
			res.BackendsTried = []string{"cache"}
			res.Took = time.Since(started)
			return res, nil
		}
	}

	backends := r.reg.For(intent)
	if len(backends) == 0 {
		return nil, fmt.Errorf("no backends registered for intent %q", intent)
	}

	maxB := r.MaxBackends
	if maxB <= 0 || maxB > len(backends) {
		maxB = len(backends)
	}

	// Per-backend result buckets. We collect everything then merge
	// via DedupItems. Buckets preserve backend order so the first
	// non-empty bucket wins Title/Snippet on ties.
	buckets := make([][]Item, 0, len(backends))
	now := time.Now().UTC()
	for i, b := range backends {
		if i >= maxB {
			break
		}
		if b.Auth != "" && Getenv(b.Auth) == "" {
			res.Errors = append(res.Errors, BackendError{Backend: b.Name, Err: "missing auth env " + b.Auth})
			continue
		}
		res.BackendsTried = append(res.BackendsTried, b.Name)

		// Rate-limit: sleep until lastHit + RateLimitMs.
		//
		// All lastHit access is guarded by r.rateMu. The previous
		// implementation read and wrote the map without locking;
		// two concurrent Route() calls into the same backend would
		// panic with 'fatal error: concurrent map read and map write'
		// (bug-hunt 2026-07-14 BUG-006). We compute the sleep duration
		// inside the lock, then drop the lock during the actual sleep
		// (which respects ctx.Done()) before re-acquiring to stamp.
		if b.RateLimitMs > 0 {
			var wait time.Duration
			r.rateMu.Lock()
			if last, ok := r.lastHit[b.Name]; ok {
				wait = time.Duration(b.RateLimitMs)*time.Millisecond - time.Since(last)
			}
			r.rateMu.Unlock()

			if wait > 0 {
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					res.Took = time.Since(started)
					return res, ctx.Err()
				}
			}
			r.rateMu.Lock()
			r.lastHit[b.Name] = time.Now()
			r.rateMu.Unlock()
		}

		body, err := r.call(ctx, b, query)
		if err != nil {
			res.Errors = append(res.Errors, BackendError{Backend: b.Name, Err: err.Error()})
			continue
		}

		if b.Parse == nil {
			res.Errors = append(res.Errors, BackendError{Backend: b.Name, Err: "no parser"})
			continue
		}
		items, err := b.Parse(body)
		if err != nil {
			res.Errors = append(res.Errors, BackendError{Backend: b.Name, Err: "parse: " + err.Error()})
			continue
		}

		// Stamp fetched_at, source, confidence, lang, and dedup_key
		// on every item so downstream consumers see provenance.
		for i := range items {
			if items[i].FetchedAt.IsZero() {
				items[i].FetchedAt = now
			}
			if items[i].Source == "" {
				items[i].Source = b.Name
			}
			if items[i].Confidence == 0 {
				items[i].Confidence = b.Confidence
			}
			if items[i].Lang == "" {
				items[i].Lang = b.LangHint
			}
			items[i].DedupKey = DedupKey(items[i].URL)
		}

		if res.BackendUsed == "" && len(items) > 0 {
			res.BackendUsed = b.Name
		}
		buckets = append(buckets, items)
	}

	// Merge + dedup across buckets.
	merged := DedupItems(buckets...)
	if r.MaxItems > 0 && len(merged) > r.MaxItems {
		merged = merged[:r.MaxItems]
	}
	res.Items = merged
	res.Took = time.Since(started)

	if len(merged) == 0 {
		if len(res.Errors) == 0 {
			return res, fmt.Errorf("no backends succeeded for intent %q", intent)
		}
		return res, fmt.Errorf("all backends failed for intent %q", intent)
	}

	// v0.8.1: optional LLM synthesis (degrades gracefully).
	if r.EnableSynthesize && r.LLMClient != nil {
		summary, _ := Synthesize(ctx, r.LLMClient, query, merged)
		res.Summary = summary
	}

	r.persist(ctx, query, intent, res)
	return res, nil
}

// persist writes the run to the configured MemSink, if any. Errors
// are logged via stderr but never propagated — persistence is best-effort
// and must not break the live query.
func (r *Router) persist(ctx context.Context, query string, intent Intent, res *Result) {
	if r.mem == nil {
		return
	}
	var confSum float32
	for _, it := range res.Items {
		confSum += it.Confidence
	}
	confAvg := float32(0)
	if len(res.Items) > 0 {
		confAvg = confSum / float32(len(res.Items))
	}

	run := &mem.ResearchRun{
		SessionID:     r.session,
		Query:         query,
		Intent:        string(intent),
		BackendUsed:   res.BackendUsed,
		BackendsTried: res.BackendsTried,
		TookMs:        res.Took.Milliseconds(),
		ConfidenceAvg: confAvg,
		Items:         make([]mem.Item, 0, len(res.Items)),
		Errors:        make([]mem.BackendError, 0, len(res.Errors)),
		CreatedAt:     mem.Now(),
	}
	for _, it := range res.Items {
		run.Items = append(run.Items, mem.Item{
			Title:       it.Title,
			URL:         it.URL,
			Snippet:     it.Snippet,
			Source:      it.Source,
			Confidence:  it.Confidence,
			FreshnessAt: it.FreshnessAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Lang:        it.Lang,
			DedupKey:    it.DedupKey,
		})
	}
	for _, e := range res.Errors {
		run.Errors = append(run.Errors, mem.BackendError{Backend: e.Backend, Err: e.Err})
	}
	if _, err := r.mem.SaveRun(ctx, run); err != nil {
		fmt.Fprintf(os.Stderr, "research: persist failed: %v\n", err)
	}
}

// call performs the HTTP request and returns the body. Transient
// failures (5xx, network errors) are retried with exponential backoff
// per the backend's Retries field (default 0 = no retries). 4xx
// errors are returned immediately since the client request won't
// change on retry.
func (r *Router) call(ctx context.Context, b Backend, query string) ([]byte, error) {
	method := b.Method
	if method == "" {
		method = "GET"
	}

	// Build the full URL. Default = BaseURL + ?q=query. Override via
	// URLForQuery for backends that put the query in the path or use
	// a non-`q` param name.
	var fullURL string
	if b.URLForQuery != nil {
		fullURL = b.URLForQuery(query)
	} else {
		u, err := url.Parse(b.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("parse backend url: %w", err)
		}
		q := u.Query()
		q.Set("q", query)
		u.RawQuery = q.Encode()
		fullURL = u.String()
	}

	// Safety: validate the resolved URL up-front. (Defensive — these are
	// hard-coded so they should always pass, but a future code change
	// shouldn't be able to SSRF via a backend URL.)
	if _, err := safety.ValidateURL(fullURL, false); err != nil {
		return nil, fmt.Errorf("backend url blocked by safety: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "dark-research-mcp/"+Version()+" (+https://github.com/dark-agents/research-mcp)")
	req.Header.Set("Accept", "application/json, text/html;q=0.9")

	if b.Auth != "" {
		req.Header.Set("Authorization", "Bearer "+Getenv(b.Auth))
		// Brave wants X-Subscription-Token instead of Authorization.
		if strings.EqualFold(b.Name, "brave") {
			req.Header.Set("X-Subscription-Token", Getenv(b.Auth))
			req.Header.Del("Authorization")
		}
	}

	// Retry policy: backends with Retries>0 retry on 5xx / network
	// errors with exponential backoff (1s, 2s, 4s, ...). 4xx is
	// returned immediately (client error, won't change on retry).
	maxAttempts := b.Retries + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		body, status, err := r.callOnce(ctx, req)
		if err == nil && status >= 200 && status < 300 {
			return body, nil
		}
		// Build the error.
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("http %d", status)
		}
		// Don't retry 4xx.
		if status >= 400 && status < 500 {
			break
		}
		// Don't sleep after the last attempt.
		if attempt == maxAttempts {
			break
		}
		// Exponential backoff: 1s, 2s, 4s, ...
		backoff := time.Duration(1<<(attempt-1)) * time.Second
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

// callOnce performs a single HTTP request and returns (body, status, err).
func (r *Router) callOnce(ctx context.Context, req *http.Request) ([]byte, int, error) {
	// Each attempt needs its own Request because http.Request bodies are consumed.
	attemptReq := req.Clone(ctx)
	attemptReq.Header = req.Header.Clone()
	resp, err := r.http.Do(attemptReq)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, resp.StatusCode, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Cap at 5 MB to avoid OOM on accidental huge responses.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	return body, resp.StatusCode, err
}
