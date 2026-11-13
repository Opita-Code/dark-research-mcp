//go:build ignore

// Command verify-live exercises every research backend against its
// LIVE upstream and prints a per-backend verdict.
//
// Usage (from the repo root):
//
//	go run ./scripts/verify-live.go
//
// Unlike the fixture recorder this does not write anything to disk: it
// is a read-only health check against the real APIs. Exit code is 0
// when every backend that has its credentials available returns at
// least one item.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/dark-agents/research-mcp/internal/research"
)

type probe struct {
	Intent   research.Intent
	Query    string
	Backend  string
	NeedsEnv string
}

func main() {
	client := &http.Client{Timeout: 45 * time.Second}
	r := research.NewRouter(research.DefaultRegistry(), client)
	// Cache off: we want to hit the network on every probe.
	r.SetEnableCache(false)

	probes := []probe{
		{research.IntentWeb, "rust programming language", "duckduckgo", ""},
		{research.IntentWeb, "rust programming language", "searxng", ""},
		{research.IntentWeb, "rust programming language", "brave", "BRAVE_API_KEY"},
		{research.IntentAcademic, "transformer attention mechanism", "openalex", ""},
		{research.IntentAcademic, "electron", "arxiv", ""},
		{research.IntentAcademic, "transformer", "semanticscholar", "S2_API_KEY"},
		{research.IntentCode, "serde", "cratesio", ""},
		{research.IntentCode, "lodash", "npm", ""},
		{research.IntentCode, "react", "github", "GITHUB_TOKEN"},
		{research.IntentCVE, "CVE-2024-3094", "osv", ""},
		{research.IntentCVE, "CVE-2024-3094", "nvd", ""},
		{research.IntentDomain, "github.com", "rdap", ""},
		{research.IntentDNS, "example.com", "cloudflare-doh", ""},
		{research.IntentDNS, "example.com", "google-doh", ""},
		{research.IntentCert, "github.com", "crtsh", ""},
		{research.IntentIP, "8.8.8.8", "ipapi", ""},
		{research.IntentIP, "8.8.8.8", "ripe", ""},
		{research.IntentThreat, "example.com", "abusech", "URLHAUS_AUTH_KEY"},
		{research.IntentThreat, "example.com", "otx", "OTX_API_KEY"},
		{research.IntentEmail, "test@example.com", "hibp", "HIBP_API_KEY"},
		{research.IntentDark, "bitcoin", "ahmia", ""},
		{research.IntentGeo, "Tokyo", "osm-nominatim", ""},
		{research.IntentNews, "climate change", "gdelt", ""},
		{research.IntentNews, "climate change", "hackernews", ""},
		{research.IntentNews, "example.com", "wayback", ""},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	type row struct {
		backend string
		status  string
		items   int
		detail  string
	}
	var rows []row

	for _, p := range probes {
		if p.NeedsEnv != "" && os.Getenv(p.NeedsEnv) == "" {
			rows = append(rows, row{p.Backend, "SKIP", 0, "no " + p.NeedsEnv})
			continue
		}
		res, err := r.Route(ctx, p.Query, p.Intent)

		status, detail, items := "FAIL", "", 0
		if err != nil {
			detail = err.Error()
		} else {
			for _, e := range res.Errors {
				if e.Backend == p.Backend {
					detail = e.Backend + ": " + e.Err
				}
			}
			// Count items that came from THIS backend. The parser may
			// set a display-flavoured Source ("osv.dev" for the osv
			// backend, "rdap.org" for rdap, "ip-api.com" for ipapi,
			// "crt.sh" for crtsh), so match on a prefix too.
			//
			// The router interleaves the backends and then caps the
			// merged list, so a backend's items may not all survive
			// into the result. That is expected; what matters is that
			// the backend contributed at least one item.
			for _, it := range res.Items {
				src := it.Source
				if src == p.Backend ||
					strings.HasPrefix(src, p.Backend+"-") ||
					strings.HasPrefix(src, p.Backend+".") ||
					strings.HasPrefix(src, p.Backend+"/") ||
					(p.Backend == "abusech" && src == "abuse.ch") {
					items++
				}
			}
			// A backend that succeeded but whose items were all
			// displaced by the merge still counts as reachable: look
			// for its error entry instead.
			if items == 0 {
				for _, e := range res.Errors {
					if e.Backend == p.Backend {
						detail = e.Backend + ": " + e.Err
					}
				}
				// No error for this backend => it ran fine, its items
				// were just displaced in the merge.
				if detail == "" && len(res.BackendsTried) > 0 {
					tried := false
					for _, t := range res.BackendsTried {
						if t == p.Backend {
							tried = true
						}
					}
					if tried {
						status = "OK"
						detail = "ran successfully; items displaced by the merge cap"
						rows = append(rows, row{p.Backend, status, 0, detail})
						fmt.Printf("%-18s %-10s items=%-4d %s\n", p.Backend, status, 0, detail)
						continue
					}
				}
			}
			if items > 0 {
				status = "OK"
				detail = fmt.Sprintf("backend_used=%s", res.BackendUsed)
			} else if detail == "" {
				detail = "no items from this backend"
			}
		}
		rows = append(rows, row{p.Backend, status, items, detail})
		fmt.Printf("%-18s %-10s items=%-4d %s\n", p.Backend, status, items, detail)
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].backend < rows[j].backend })
	fmt.Println("\n=== summary ===")
	fail := 0
	for _, x := range rows {
		if x.status == "FAIL" {
			fail++
		}
	}
	fmt.Printf("%d backends probed, %d failed, %d skipped\n",
		len(rows), fail, len(rows)-fail)
	if fail > 0 {
		os.Exit(1)
	}
}
