package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dark-agents/research-mcp/internal/safety"
	"github.com/mark3labs/mcp-go/mcp"
)

type webFetchArgs struct {
	URL       string `json:"url" jsonschema:"URL to fetch (must be http or https, no private IPs)"`
	MaxLength int    `json:"max_length,omitempty" jsonschema:"Max characters to return (default 20000)"`
	Raw       bool   `json:"raw,omitempty" jsonschema:"If true, return HTML without sanitization (advanced; default false)"`
}

type webFetchOutput struct {
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Markdown  string    `json:"markdown"`
	Bytes     int       `json:"bytes"`
	FetchedAt time.Time `json:"fetched_at"`
	Warnings  []string  `json:"warnings,omitempty"`
}

func webFetchTool(c *clients) Tool {
	def := mcp.NewTool("web_fetch",
		mcp.WithDescription("Download a public URL, sanitize HTML to markdown, and wrap the result with explicit trust-boundary markers. Returns title, sanitized markdown, byte count, and any warnings (truncation, removed markup)."),
		mcp.WithString("url", mcp.Required(), mcp.Description("Public http/https URL")),
		mcp.WithNumber("max_length", mcp.Description("Max characters of returned markdown (default 20000)")),
		mcp.WithBoolean("raw", mcp.Description("If true, skip HTML sanitization (advanced)")),
	)

	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args webFetchArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			if args.URL == "" {
				return nil, fmt.Errorf("url is required")
			}
			if _, err := safety.ValidateURL(args.URL, c.Cfg.Safety.AllowLoopback); err != nil {
				return nil, err
			}
			maxLen := args.MaxLength
			if maxLen <= 0 {
				maxLen = 20000
			}
			if maxLen > c.Cfg.Safety.MaxOutputChars {
				maxLen = c.Cfg.Safety.MaxOutputChars
			}

			httpReq, err := http.NewRequestWithContext(ctx, "GET", args.URL, nil)
			if err != nil {
				return nil, err
			}
			httpReq.Header.Set("User-Agent", "dark-research-mcp/0.1 (+https://github.com/dark-agents/research-mcp)")
			httpReq.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.5")

			resp, err := c.clearnet.DoContext(ctx, httpReq)
			if err != nil {
				return nil, fmt.Errorf("fetch %s: %w", args.URL, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return nil, fmt.Errorf("fetch %s returned %d", args.URL, resp.StatusCode)
			}

			limit := int64(c.Cfg.Safety.MaxResponseBytes)
			body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
			if err != nil {
				return nil, fmt.Errorf("read body: %w", err)
			}

			var (
				content  string
				warnings []string
			)
			if args.Raw {
				content = string(body)
			} else {
				content, warnings = sanitizeHTML(string(body), maxLen)
			}

			// Truncate to maxLen with a warning if the sanitizer didn't already.
			if len(content) > maxLen {
				warnings = append(warnings, fmt.Sprintf("truncated at %d chars", maxLen))
				content = content[:maxLen]
			}

			title := extractTitle(string(body))
			out := webFetchOutput{
				Title:     title,
				URL:       args.URL,
				Markdown:  safety.WrapUntrusted(args.URL, time.Now().UTC(), content),
				Bytes:     len(body),
				FetchedAt: time.Now().UTC(),
				Warnings:  warnings,
			}
			return jsonResult(out), nil
		},
	}
}

// sanitizeHTML is a thin pass: strip <script>/<style>, collapse tags to text.
// Production: bluemonday or a markdown converter; v0.1 ships minimal.
// Defined as a var so tests can swap in a deterministic impl.
var sanitizeHTML = func(html string, maxChars int) (string, []string) {
	var warnings []string
	// Strip <script>, <style>, <noscript> blocks (case-insensitive).
	stripped := stripBlocks(html, []string{"script", "style", "noscript"})
	if len(stripped) != len(html) {
		warnings = append(warnings, "stripped script/style/noscript blocks")
	}
	// Strip tags.
	text := stripTags(stripped)
	// Collapse whitespace.
	text = collapseWS(text)
	if len(text) > maxChars {
		warnings = append(warnings, fmt.Sprintf("truncated at %d chars", maxChars))
		text = text[:maxChars]
	}
	return text, warnings
}

// extractTitle pulls <title>...</title> from raw HTML. Best-effort.
func extractTitle(html string) string {
	openIdx := indexCI(html, "<title", 0)
	if openIdx < 0 {
		return ""
	}
	closeOpen := indexCI(html, ">", openIdx)
	if closeOpen < 0 {
		return ""
	}
	closeIdx := indexCI(html, "</title>", closeOpen)
	if closeIdx < 0 {
		return ""
	}
	return strings.TrimSpace(html[closeOpen+1 : closeIdx])
}