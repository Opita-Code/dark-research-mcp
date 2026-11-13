//go:build ignore

// Command record-fixtures refreshes the checked-in fixture corpus from
// the live upstream APIs.
//
// Usage (from the repo root):
//
//	go run ./scripts/record-fixtures.go
//
// It uses the same transport as the tests (testutil.RecordingTransport in
// ModeRecord) so the on-disk format is identical to what the replay
// tests expect.
//
// Endpoints that need an API key are skipped unless the matching env
// var is set (URLHAUS_AUTH_KEY, OTX_API_KEY, HIBP_API_KEY, S2_API_KEY,
// GITHUB_TOKEN, BRAVE_API_KEY).
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/dark-agents/research-mcp/internal/research"
	"github.com/dark-agents/research-mcp/internal/research/testutil"
)

// fixtureCase is one recorded request: the intent, the query, and the
// backend name it is expected to exercise.
type fixtureCase struct {
	Intent  research.Intent
	Query   string
	Backend string
}

func main() {
	baseDir := "fixtures"
	if len(os.Args) > 1 {
		baseDir = os.Args[1]
	}

	transport := &testutil.RecordingTransport{
		BaseDir:     baseDir,
		Mode:        testutil.ModeRecord,
		HeaderScrub: []string{"set-cookie", "date"},
	}
	r := research.NewRouter(research.DefaultRegistry(), &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	})

	// The queries MUST match the ones used by router_fixtures_test.go,
	// because the fixture path is derived from the request URL.
	cases := []fixtureCase{
		{research.IntentWeb, "rust programming language", "duckduckgo"},
		{research.IntentAcademic, "transformer attention mechanism", "openalex"},
		{research.IntentAcademic, "transformer attention mechanism", "arxiv"},
		{research.IntentCode, "tokio", "cratesio"},
		{research.IntentCode, "tokio", "npm"},
		{research.IntentCode, "lodash", "cratesio"},
		{research.IntentCode, "lodash", "npm"},
		{research.IntentCVE, "CVE-2024-3094", "osv"},
		{research.IntentCVE, "CVE-2024-3094", "nvd"},
		{research.IntentDomain, "github.com", "rdap"},
		{research.IntentDNS, "example.com", "cloudflare-doh"},
		{research.IntentDNS, "example.com", "google-doh"},
		{research.IntentCert, "github.com", "crtsh"},
		{research.IntentIP, "8.8.8.8", "ipapi"},
		{research.IntentIP, "8.8.8.8", "ripe"},
		{research.IntentDark, "bitcoin", "ahmia"},
		{research.IntentGeo, "Tokyo", "osm-nominatim"},
		{research.IntentNews, "climate change", "gdelt"},
		{research.IntentNews, "climate change", "hackernews"},
		{research.IntentNews, "example.com", "wayback"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	for _, c := range cases {
		before := countFixtures(baseDir)
		res, err := r.Route(ctx, c.Query, c.Intent)
		after := countFixtures(baseDir)
		status := "ok"
		if err != nil {
			status = "ERR: " + err.Error()
		} else {
			status = fmt.Sprintf("backend=%s items=%d", res.BackendUsed, len(res.Items))
		}
		fmt.Printf("%-10s %-34s %-16s %s (+%d fixtures)\n",
			c.Intent, trunc(c.Query, 32), c.Backend, status, after-before)
		for _, e := range res.Errors {
			fmt.Printf("           error: %s: %s\n", e.Backend, trunc(e.Err, 90))
		}
	}
}

func countFixtures(dir string) int {
	n := 0
	_ = filepathWalk(dir, &n)
	return n
}

func filepathWalk(dir string, n *int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			if err := filepathWalk(dir+"/"+e.Name(), n); err != nil {
				return err
			}
			continue
		}
		*n++
	}
	return nil
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
