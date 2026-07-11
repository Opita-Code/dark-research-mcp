package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/dark-agents/research-mcp/internal/research"
	"github.com/mark3labs/mcp-go/mcp"
)

// darkResearchArgs is the input for the meta-tool dark_research.
// If Intent is empty, the classifier picks.
type darkResearchArgs struct {
	Query  string `json:"query" jsonschema:"Search query"`
	Intent string `json:"intent,omitempty" jsonschema:"Optional: web|academic|code|cve|domain|dns|cert|ip|threat|email|dark|geo|news (default: classifier chooses)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Optional cap on number of results returned (default 20)"`
}

func darkResearchTool() Tool {
	def := mcp.NewTool("dark_research",
		mcp.WithDescription("Intent-based OSINT search. Routes the query to the right backend (DuckDuckGo for web, OpenAlex for academic, OSV.dev for CVE, ip-api.com for IP, crates.io/npm/GitHub for code, etc.) with automatic fallback. The classifier detects intent from query shape (CVE IDs, DOIs, .onion, IPs, domains, GitHub URLs, etc.) unless you pass intent explicitly. Returns a normalized shape with backend_used, took_ms, and per-result title/url/snippet."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		mcp.WithString("intent", mcp.Description("Optional intent override: web|academic|code|cve|domain|dns|cert|ip|threat|email|dark|geo|news")),
		mcp.WithNumber("limit", mcp.Description("Optional cap on number of results returned")),
	)

	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args darkResearchArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			if args.Query == "" {
				return nil, fmt.Errorf("query is required")
			}
			intent := research.ParseIntent(args.Intent)
			r := newResearchRouter()
			res, err := r.Route(ctx, args.Query, intent)
			if err != nil {
				// Even on aggregate error, return the result so the agent
				// sees backends_tried and errors. Returning a plain error
				// would lose that diagnostic context.
				return jsonResult(res), nil
			}
			if args.Limit > 0 && len(res.Items) > args.Limit {
				res.Items = res.Items[:args.Limit]
			}
			return jsonResult(res), nil
		},
	}
}

// darkResearchIntentTool builds a specialized tool for a single intent.
// The handler ignores any intent hint in the args (the tool name already
// declares the intent) and forces the router to that intent.
func darkResearchIntentTool(intent research.Intent, description string) Tool {
	name := "dark_research_" + string(intent)
	def := mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		mcp.WithNumber("limit", mcp.Description("Optional cap on number of results returned")),
	)

	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args struct {
				Query string `json:"query"`
				Limit int    `json:"limit,omitempty"`
			}
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			if args.Query == "" {
				return nil, fmt.Errorf("query is required")
			}
			r := newResearchRouter()
			res, err := r.Route(ctx, args.Query, intent)
			if err != nil {
				return jsonResult(res), nil
			}
			if args.Limit > 0 && len(res.Items) > args.Limit {
				res.Items = res.Items[:args.Limit]
			}
			return jsonResult(res), nil
		},
	}
}

// newResearchRouter returns the shared Router (singleton), with mem
// persistence wired in if a Store was injected at Register time.
// Falls back to a fresh Router if Register was not called (tests).
func newResearchRouter() *research.Router {
	return sharedRouter()
}

// IntentToString helper exposed so other tool files (and tests) can
// reference intent names without redefining the constants.
func IntentToString(i research.Intent) string { return string(i) }

// jsonDump is a tiny helper for debugging — not wired into any tool.
func jsonDump(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

// darkResearchMultiArgs: parallel research across multiple intents.
// One query, multiple backends, deduped by URL, sorted by confidence.
type darkResearchMultiArgs struct {
	Query   string   `json:"query" jsonschema:"Search query"`
	Intents []string `json:"intents" jsonschema:"Intents to query in parallel (e.g. ['cve','ip','domain']). Empty = all 13."`
	Limit   int      `json:"limit,omitempty" jsonschema:"Optional cap on total items returned (default 30)"`
}

// MultiResult bundles per-intent results + the deduped union.
type MultiResult struct {
	Query      string                            `json:"query"`
	PerIntent  map[string]*research.Result       `json:"per_intent"`
	Deduped    []research.Item                   `json:"deduped"`
	TotalFound int                               `json:"total_found"`
}

func darkResearchMultiTool() Tool {
	def := mcp.NewTool("dark_research_multi",
		mcp.WithDescription("Parallel multi-intent OSINT. Runs the same query against multiple backends simultaneously (e.g. web + academic + cve + ip) and returns per-intent results plus a deduped union sorted by confidence. Use this to triangulate a target from multiple angles in one call. Backends: same as the per-intent tools (open-source primary, free fallback)."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		mcp.WithArray("intents", mcp.Description("Intents to query in parallel. Omit for all 13. Example: [\"cve\",\"academic\",\"code\"]")),
		mcp.WithNumber("limit", mcp.Description("Cap on total items returned (default 30)")),
	)

	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args darkResearchMultiArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			if args.Query == "" {
				return nil, fmt.Errorf("query is required")
			}

			// Resolve intents: if empty, all known intents.
			intents := []research.Intent{}
			if len(args.Intents) == 0 {
				for _, i := range allIntents() {
					intents = append(intents, i)
				}
			} else {
				for _, s := range args.Intents {
					i := research.ParseIntent(s)
					if i == "" {
						return nil, fmt.Errorf("unknown intent: %q", s)
					}
					intents = append(intents, i)
				}
			}

			// Run each intent in its own goroutine; collect results.
			r := newResearchRouter()
			type slot struct {
				intent research.Intent
				res   *research.Result
				err   error
			}
			results := make([]slot, len(intents))
			var wg sync.WaitGroup
			for i, in := range intents {
				i, in := i, in
				wg.Add(1)
				go func() {
					defer wg.Done()
					res, err := r.Route(ctx, args.Query, in)
					results[i] = slot{intent: in, res: res, err: err}
				}()
			}
			wg.Wait()

			// Build per-intent map and dedupe by URL.
			perIntent := make(map[string]*research.Result, len(intents))
			seen := make(map[string]bool)
			var deduped []research.Item
			totalFound := 0
			for _, s := range results {
				if s.res == nil {
					perIntent[string(s.intent)] = nil
					continue
				}
				perIntent[string(s.intent)] = s.res
				totalFound += len(s.res.Items)
				for _, it := range s.res.Items {
					key := it.URL
					if key == "" {
						key = it.Title + "|" + it.Source
					}
					if seen[key] {
						continue
					}
					seen[key] = true
					deduped = append(deduped, it)
				}
			}

			// Sort by confidence desc (then freshness desc).
			sortItemsByConfidence(deduped)

			limit := args.Limit
			if limit <= 0 {
				limit = 30
			}
			if len(deduped) > limit {
				deduped = deduped[:limit]
			}

			out := MultiResult{
				Query:      args.Query,
				PerIntent:  perIntent,
				Deduped:    deduped,
				TotalFound: totalFound,
			}
			return jsonResult(out), nil
		},
	}
}

// allIntents returns every intent known to the registry.
func allIntents() []research.Intent {
	return research.DefaultRegistry().Intents()
}

// sortItemsByConfidence orders items by confidence desc; ties broken
// by FreshnessAt desc (more recent first).
func sortItemsByConfidence(items []research.Item) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && itemLess(items[j], items[j-1]); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func itemLess(a, b research.Item) bool {
	if a.Confidence != b.Confidence {
		return a.Confidence > b.Confidence
	}
	return a.FreshnessAt.After(b.FreshnessAt)
}