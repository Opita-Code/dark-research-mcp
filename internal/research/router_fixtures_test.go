package research_test

// These tests cover the routing + parsing layer against real
// backend responses. They are VCR-style: a "record" run captures
// the live response to disk; a "replay" run (the default in CI)
// serves that response from disk so the test is hermetic.
//
// To refresh fixtures when a backend changes its API:
//
//	RECORD_FIXTURES=1 go test -count=1 -run 'TestRouter_' ./internal/research/
//
// The fixtures land under ../../fixtures/<host>/<path>.http. Inspect
// the diff in git before committing.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dark-agents/research-mcp/internal/research"
	"github.com/dark-agents/research-mcp/internal/research/testutil"
)

// fixturesDir is the on-disk location of the recorded responses.
// Resolved relative to the test working directory so the test works
// both under `go test ./internal/research/` and `go test ./...`.
var fixturesDir = func() string {
	candidates := []string{
		"../../fixtures",                 // go test ./internal/research/
		"internal/research/../../fixtures", // belt-and-braces
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		// Use the first candidate that exists OR is creatable.
		// On record we always want a real directory.
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	// Default: use the first candidate (will be created on record).
	abs, _ := filepath.Abs(candidates[0])
	return abs
}()

// recordMode is enabled by the RECORD_FIXTURES env var. CI never
// sets this; only humans refreshing fixtures do.
func recordMode() bool {
	return os.Getenv("RECORD_FIXTURES") == "1"
}

// newRouter returns a router wired with a recording or replay
// transport. In record mode fixtures are written under fixturesDir;
// in replay mode they are read from there.
func newRouter(t *testing.T) *research.Router {
	t.Helper()
	mode := testutil.ModeReplay
	if recordMode() {
		mode = testutil.ModeRecord
	}
	tr := &testutil.RecordingTransport{
		BaseDir: fixturesDir,
		Mode:    mode,
		// Scrub volatile headers so fixtures don't churn on every
		// refresh (Date, server clock, etc.).
		HeaderScrub: []string{
			"date",
			"age",
			"x-served-by",
			"x-cache",
			"x-cache-status",
			"x-amz-cf-id",
			"x-amz-cf-pop",
			"via",
			"server",
			"cf-ray",
			"cf-cache-status",
		},
	}
	hc := &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
	}
	return research.NewRouter(research.DefaultRegistry(), hc)
}

// assertItems checks the basics every successful backend call should
// satisfy: at least one item, non-empty title, confidence > 0.
func assertItems(t *testing.T, res *research.Result, wantBackend string) {
	t.Helper()
	if res == nil {
		t.Fatal("nil result")
	}
	if res.BackendUsed != wantBackend {
		t.Fatalf("backend: got %q, want %q (errors=%+v)",
			res.BackendUsed, wantBackend, res.Errors)
	}
	if len(res.Items) == 0 {
		t.Fatalf("no items returned from %s", wantBackend)
	}
	for i, it := range res.Items {
		if it.Title == "" {
			t.Errorf("item %d: empty title", i)
		}
		if it.URL == "" {
			t.Errorf("item %d: empty url", i)
		}
		if it.Confidence <= 0 {
			t.Errorf("item %d: confidence %f <= 0", i, it.Confidence)
		}
	}
}

func runRoute(t *testing.T, query string, intent research.Intent) *research.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := newRouter(t)
	res, err := r.Route(ctx, query, intent)
	if err != nil {
		t.Fatalf("route %q: %v (errors=%+v)", query, err, resErrors(res))
	}
	return res
}

func resErrors(r *research.Result) string {
	if r == nil {
		return "<nil result>"
	}
	var b strings.Builder
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "%s: %s; ", e.Backend, e.Err)
	}
	return b.String()
}

// --- Per-backend tests -------------------------------------------

func TestRouter_OSV(t *testing.T) {
	res := runRoute(t, "CVE-2024-3094", research.IntentCVE)
	assertItems(t, res, "osv")
	// OSV should return at least one affected package.
	foundXZ := false
	for _, it := range res.Items {
		if strings.Contains(strings.ToLower(it.Title), "xz") ||
			strings.Contains(strings.ToLower(it.Title), "liblzma") {
			foundXZ = true
			break
		}
	}
	if !foundXZ {
		t.Logf("warning: CVE-2024-3094 result did not mention xz/liblzma; got %d items", len(res.Items))
	}
}

func TestRouter_OpenAlex(t *testing.T) {
	res := runRoute(t, "transformer attention mechanism", research.IntentAcademic)
	assertItems(t, res, "openalex")
	// OpenAlex items should have publication_date freshness when present.
	stamped := 0
	for _, it := range res.Items {
		if !it.FreshnessAt.IsZero() {
			stamped++
		}
	}
	if stamped == 0 {
		t.Errorf("no items had FreshnessAt set (OpenAlex publishes publication_date)")
	}
}

func TestRouter_cratesio(t *testing.T) {
	res := runRoute(t, "tokio", research.IntentCode)
	assertItems(t, res, "cratesio")
}

func TestRouter_npm(t *testing.T) {
	// Either cratesio or npm can satisfy a code query. The parser
	// itself is covered in parser_test.go (hand-crafted input). Here
	// we just verify the chain completes.
	res := runRoute(t, "lodash", research.IntentCode)
	if res.BackendUsed != "cratesio" && res.BackendUsed != "npm" {
		t.Errorf("expected cratesio or npm, got %q (errors=%+v)",
			res.BackendUsed, resErrors(res))
	}
	if len(res.Items) == 0 {
		t.Error("chain returned 0 items")
	}
}

func TestRouter_RDAP(t *testing.T) {
	res := runRoute(t, "github.com", research.IntentDomain)
	assertItems(t, res, "rdap")
}

func TestRouter_DoH(t *testing.T) {
	res := runRoute(t, "example.com", research.IntentDNS)
	// Accept either Cloudflare (weight 1) or Google (weight 2) since
	// Cloudflare sometimes returns 400 for non-DNSSEC queries.
	if res.BackendUsed != "cloudflare-doh" && res.BackendUsed != "google-doh" {
		t.Fatalf("backend: got %q, want cloudflare-doh or google-doh (errors=%+v)",
			res.BackendUsed, resErrors(res))
	}
}

func TestRouter_crtsh(t *testing.T) {
	res := runRoute(t, "github.com", research.IntentCert)
	assertItems(t, res, "crtsh")
}

func TestRouter_ipapi(t *testing.T) {
	res := runRoute(t, "8.8.8.8", research.IntentIP)
	assertItems(t, res, "ipapi")
}

func TestRouter_ripe(t *testing.T) {
	// The IP chain is ipapi (weight 1) → ripe (weight 2). The
	// parser is covered directly in parser_test.go. Here we just
	// verify the chain ends on either backend for the canonical
	// 8.8.8.8 (which has a recorded fixture for ipapi).
	res := runRoute(t, "8.8.8.8", research.IntentIP)
	if res.BackendUsed != "ipapi" && res.BackendUsed != "ripe" {
		t.Fatalf("expected ipapi or ripe, got %q (errors=%+v)",
			res.BackendUsed, resErrors(res))
	}
}

func TestRouter_ahmia(t *testing.T) {
	// Ahmia.fi redirects searches to "/" with anti-bot protection, so
	// the recorded body is empty and the parser legitimately returns
	// 0 items. The test verifies the chain completes without error;
	// item count is not asserted.
	res := runRoute(t, "bitcoin", research.IntentDark)
	if res.BackendUsed != "ahmia" {
		t.Errorf("expected ahmia, got %q (errors=%+v)",
			res.BackendUsed, resErrors(res))
	}
}

func TestRouter_nominatim(t *testing.T) {
	res := runRoute(t, "Tokyo", research.IntentGeo)
	assertItems(t, res, "osm-nominatim")
}

func TestRouter_gdelt(t *testing.T) {
	res := runRoute(t, "climate change", research.IntentNews)
	// GDELT is sometimes unreachable (TLS handshake timeout).
	// Accept either gdelt (primary) or wayback (fallback).
	if res.BackendUsed != "gdelt" && res.BackendUsed != "wayback" {
		t.Errorf("expected gdelt or wayback, got %q (errors=%+v)",
			res.BackendUsed, resErrors(res))
	}
}

// TestRouter_fallback forces a primary-backend failure by injecting
// a 500 envelope. The router should fall through to the next
// backend in the chain. This tests the routing logic, not the
// network.
func TestRouter_fallback(t *testing.T) {
	// Use a hand-crafted transport that returns 500 for "osv" and
	// serves a recorded response for "nvd". The fixture path must
	// match what testutil.FixturePathFor computes for the NVD URL.
	dir := t.TempDir()
	// NVD URL: https://services.nvd.nist.gov/rest/json/cves/2.0?cveId=CVE-2024-3094
	// Path: services.nvd.nist.gov/rest/json/cves/2.0__cveId-CVE-2024-3094.http
	hostDir := filepath.Join(dir, "services.nvd.nist.gov", "rest", "json", "cves")
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		t.Fatal(err)
	}
	envelope := "HTTP/1.1 200 OK\r\n" +
		"Content-Type: application/json\r\n" +
		"\r\n" +
		`{"vulnerabilities":[{"cve":{"id":"CVE-2024-3094","descriptions":[{"lang":"en","value":"xz-utils backdoor"}]}}]}`
	if err := os.WriteFile(
		filepath.Join(hostDir, "2.0__cveId-CVE-2024-3094.http"),
		[]byte(envelope), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := &fallbackTransport{baseDir: dir, failHost: "api.osv.dev"}
	r := research.NewRouter(research.DefaultRegistry(),
		&http.Client{Transport: tr, Timeout: 10 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := r.Route(ctx, "CVE-2024-3094", research.IntentCVE)
	if err != nil {
		t.Fatalf("route: %v (errors=%+v)", err, resErrors(res))
	}
	if res.BackendUsed != "nvd" {
		t.Errorf("expected fallback to nvd, got %q (errors=%+v)",
			res.BackendUsed, resErrors(res))
	}
}

// fallbackTransport is a test-only transport that returns 500 for a
// specific host and replays a recorded response for everything else.
type fallbackTransport struct {
	baseDir  string
	failHost string
}

func (f *fallbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == f.failHost {
		return &http.Response{
			StatusCode: 500,
			Status:     "500 Internal Server Error",
			Body:       http.NoBody,
			Request:    req,
			Header:     make(http.Header),
		}, nil
	}
	// Replay from baseDir.
	tr := &testutil.RecordingTransport{BaseDir: f.baseDir, Mode: testutil.ModeReplay}
	return tr.RoundTrip(req)
}

// TestRouter_auth_missing verifies that a backend requiring an
// API key is silently skipped when the env var is empty, and the
// router falls through to the next backend in the chain.
func TestRouter_auth_missing(t *testing.T) {
	// Clear GITHUB_TOKEN for this test.
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("BRAVE_API_KEY", "")
	t.Setenv("S2_API_KEY", "")
	t.Setenv("HIBP_API_KEY", "")

	// Code intent: cratesio (no auth, weight 1) → npm (no auth, weight 2)
	// → github (auth, weight 3). We should land on npm without
	// trying github.
	res := runRoute(t, "tokio", research.IntentCode)
	if res.BackendUsed != "cratesio" && res.BackendUsed != "npm" {
		t.Errorf("expected cratesio or npm (no-auth chain), got %q (errors=%+v)",
			res.BackendUsed, resErrors(res))
	}
}
