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
)

// Getenv reads an env var. Exposed as a var so tests can stub it.
var Getenv = os.Getenv

// Version is stamped into the User-Agent on every outbound request.
// Defaults to "dev"; the release build sets it via -ldflags
// "-X github.com/dark-agents/research-mcp/internal/research.Version=0.3.1".
// Backends that fingerprint by user-agent see one consistent string
// per binary.
var Version = "dev"

// MemSink is the interface dark-mem must satisfy for the router to
// persist runs. Defined here to avoid an import cycle (research does
// not import mem; mem imports nothing).
type MemSink interface {
	SaveRun(ctx context.Context, run *mem.ResearchRun) (int64, error)
}

// Router orchestrates a query against the registry.
type Router struct {
	reg     *Registry
	http    *http.Client
	lastHit map[string]time.Time // backend name → last call time
	rateMu  sync.Mutex           // guards lastHit (concurrent Route() calls)
	mem     MemSink              // optional; nil = no persistence
	session string               // session id stamped on saved runs
}

// NewRouter builds a router with the given registry.
func NewRouter(reg *Registry, hc *http.Client) *Router {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Router{reg: reg, http: hc, lastHit: map[string]time.Time{}}
}

// SetMem wires a MemSink so successful runs get persisted.
// Pass nil to disable persistence.
func (r *Router) SetMem(m MemSink) { r.mem = m }

// SetSession stamps a session id on subsequent persisted runs.
func (r *Router) SetSession(s string) { r.session = s }

// Route runs a query against the appropriate intent's backends.
// Tries each backend in weight order until one succeeds or all fail.
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

	backends := r.reg.For(intent)
	if len(backends) == 0 {
		return nil, fmt.Errorf("no backends registered for intent %q", intent)
	}

	for _, b := range backends {
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

		// Stamp fetched_at and confidence on every item so the LLM
		// knows provenance + trust level.
		now := time.Now().UTC()
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
		}

		res.BackendUsed = b.Name
		res.Items = items
		res.Took = time.Since(started)
		r.persist(ctx, query, intent, res)
		return res, nil
	}

	res.Took = time.Since(started)
	if len(res.Errors) == 0 {
		return res, fmt.Errorf("no backends succeeded for intent %q", intent)
	}
	return res, fmt.Errorf("all backends failed for intent %q", intent)
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
		})
	}
	for _, e := range res.Errors {
		run.Errors = append(run.Errors, mem.BackendError{Backend: e.Backend, Err: e.Err})
	}
	if _, err := r.mem.SaveRun(ctx, run); err != nil {
		fmt.Fprintf(os.Stderr, "research: persist failed: %v\n", err)
	}
}

// call performs the HTTP request and returns the body.
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
	req.Header.Set("User-Agent", "dark-research-mcp/"+Version+" (+https://github.com/dark-agents/research-mcp)")
	req.Header.Set("Accept", "application/json, text/html;q=0.9")

	if b.Auth != "" {
		req.Header.Set("Authorization", "Bearer "+Getenv(b.Auth))
		// Brave wants X-Subscription-Token instead of Authorization.
		if strings.EqualFold(b.Name, "brave") {
			req.Header.Set("X-Subscription-Token", Getenv(b.Auth))
			req.Header.Del("Authorization")
		}
	}

	resp, err := r.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read up to 4KB of body for diagnostics.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Cap at 5 MB to avoid OOM on accidental huge responses.
	return io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
}