package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/dark-agents/research-mcp/internal/safety"
	"github.com/mark3labs/mcp-go/mcp"
)

type webSearchArgs struct {
	Query     string `json:"query" jsonschema:"Search query (max 400 chars, 50 words)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Number of results (1-20, default 10)"`
	Offset    int    `json:"offset,omitempty" jsonschema:"Pagination offset (0-9)"`
	Freshness string `json:"freshness,omitempty" jsonschema:"Filter: pd (24h), pw (week), pm (month), py (year)"`
	SafeSearch string `json:"safesearch,omitempty" jsonschema:"off, moderate, strict (default moderate)"`
}

type searchHit struct {
	Title   string  `json:"title" jsonschema:"Result title"`
	URL     string  `json:"url" jsonschema:"Result URL"`
	Snippet string  `json:"snippet" jsonschema:"Result snippet"`
	Score   float32 `json:"score" jsonschema:"Relevance score (provider-specific, 0..1)"`
	Source  string  `json:"source" jsonschema:"Provider that produced the hit (brave, duckduckgo, ...)"`
}

type webSearchOutput struct {
	Results        []searchHit `json:"results"`
	TotalEstimated int64       `json:"total_estimated"`
	Provider       string      `json:"provider"`
}

func webSearchTool(c *clients) Tool {
	def := mcp.NewTool("web_search",
		mcp.WithDescription("Search the public web. Returns title, URL, snippet, and relevance score. Provider is configurable (Brave by default). Use for discovering sources before fetching them with web_fetch."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query (max 400 chars)")),
		mcp.WithNumber("limit", mcp.Description("Number of results (1-20, default 10)")),
		mcp.WithNumber("offset", mcp.Description("Pagination offset (0-9)")),
		mcp.WithString("freshness", mcp.Description("Filter: pd (24h), pw (week), pm (month), py (year)")),
		mcp.WithString("safesearch", mcp.Description("off, moderate, strict (default moderate)")),
	)

	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args webSearchArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			if args.Query == "" || len(args.Query) > 400 {
				return nil, fmt.Errorf("query must be 1-400 chars")
			}
			limit := clampInt(args.Limit, 1, 20, 10)
			offset := clampInt(args.Offset, 0, 9, 0)
			safesearch := args.SafeSearch
			if safesearch == "" {
				safesearch = "moderate"
			}

			apiKey := readBraveKey()
			if apiKey == "" {
				return nil, fmt.Errorf("BRAVE_API_KEY not set; cannot use web_search (set env or add to config)")
			}

			q := url.Values{}
			q.Set("q", args.Query)
			q.Set("count", fmt.Sprintf("%d", limit))
			q.Set("offset", fmt.Sprintf("%d", offset))
			q.Set("safesearch", safesearch)
			if args.Freshness != "" {
				q.Set("freshness", args.Freshness)
			}

			httpReq, err := http.NewRequestWithContext(ctx, "GET",
				"https://api.search.brave.com/res/v1/web/search?"+q.Encode(), nil)
			if err != nil {
				return nil, err
			}
			httpReq.Header.Set("X-Subscription-Token", apiKey)
			httpReq.Header.Set("Accept", "application/json")

			resp, err := c.clearnet.DoContext(ctx, httpReq)
			if err != nil {
				return nil, fmt.Errorf("brave search request failed: %w", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(io.LimitReader(resp.Body, int64(c.Cfg.Safety.MaxResponseBytes)))
			if err != nil {
				return nil, fmt.Errorf("brave read error: %w", err)
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return nil, fmt.Errorf("brave search returned %d: %s", resp.StatusCode, string(body))
			}

			var raw struct {
				Query struct {
					NumResults int64 `json:"num_results"`
				} `json:"query"`
				Web struct {
					Results []struct {
						Title       string `json:"title"`
						URL         string `json:"url"`
						Description string `json:"description"`
					} `json:"results"`
				} `json:"web"`
			}
			if err := json.Unmarshal(body, &raw); err != nil {
				return nil, fmt.Errorf("brave parse error: %w", err)
			}

			out := webSearchOutput{
				Results:        make([]searchHit, 0, len(raw.Web.Results)),
				TotalEstimated: raw.Query.NumResults,
				Provider:       "brave",
			}
			for _, r := range raw.Web.Results {
				if len(out.Results) >= limit {
					break
				}
				out.Results = append(out.Results, searchHit{
					Title:   r.Title,
					URL:     r.URL,
					Snippet: r.Description,
					Score:   1.0, // Brave doesn't return a per-result score
					Source:  "brave",
				})
			}

			// Re-validate every result URL against safety rules so a Brave hit
			// pointing at 169.254.169.254 cannot slip through.
			for i, r := range out.Results {
				if _, err := safety.ValidateURL(r.URL, c.Cfg.Safety.AllowLoopback); err != nil {
					out.Results[i].Snippet = "[filtered: " + err.Error() + "] " + out.Results[i].Snippet
				}
			}

			return jsonResult(out), nil
		},
	}
}

// readBraveKey returns BRAVE_API_KEY from the process env. The config layer
// (config.RequireConsentFor, etc.) does not yet expose provider secrets;
// that's deferred to v0.2 when we add a config-driven secrets map.
func readBraveKey() string {
	return strings.TrimSpace(getenv("BRAVE_API_KEY"))
}