package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

type urlExtractArgs struct {
	URL string `json:"url" jsonschema:"URL to parse"`
}

type urlExtractOutput struct {
	Scheme      string            `json:"scheme"`
	Host        string            `json:"host"`
	Port        string            `json:"port"`
	Path        string            `json:"path"`
	QueryParams map[string]string `json:"query_params"`
	Fragment    string            `json:"fragment"`
	IsSafe      bool              `json:"is_safe"`
}

func urlExtractComponentsTool() Tool {
	def := mcp.NewTool("url_extract_components",
		mcp.WithDescription("Parse and validate a URL. Decomposes scheme/host/port/path/query/fragment and flags whether the URL is safe to fetch (no blocked scheme, no private IP after DNS resolution)."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL to parse")),
	)

	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args urlExtractArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			// Reuse the safety layer; reuse err message.
			u, err := validateURL(args.URL, false)
			if err != nil {
				// Still return a structured breakdown of what was parseable,
				// so the LLM can reason about malformed input.
				return textResult(fmt.Sprintf("URL rejected: %v", err)), nil
			}
			q := u.Query()
			qm := make(map[string]string, len(q))
			for k := range q {
				qm[k] = q.Get(k)
			}
			out := urlExtractOutput{
				Scheme:      u.Scheme,
				Host:        u.Hostname(),
				Port:        u.Port(),
				Path:        u.Path,
				QueryParams: qm,
				Fragment:    u.Fragment,
				IsSafe:      true,
			}
			return jsonResult(out), nil
		},
	}
}

type textAnonymizeArgs struct {
	Text        string   `json:"text" jsonschema:"Text to anonymize"`
	EntityTypes []string `json:"entity_types,omitempty" jsonschema:"Which entities to mask: email, ip, phone, domain (default: all)"`
}

type textAnonymizeOutput struct {
	Anonymized string            `json:"anonymized"`
	Replacements map[string]int  `json:"replacements"`
}

func textAnonymizeTool() Tool {
	def := mcp.NewTool("text_anonymize",
		mcp.WithDescription("Mask emails, IP addresses, phone numbers, and domains in a text. Useful for sharing evidence without leaking PII. Returns the anonymized text and a count of replacements per entity type."),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to anonymize")),
		mcp.WithArray("entity_types", mcp.Description("Optional subset of [email, ip, phone, domain]; default all")),
	)

	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args textAnonymizeArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			out := anonymizeText(args.Text, args.EntityTypes)
			return jsonResult(out), nil
		},
	}
}

// dummy references to keep imports honest in v0.1 stubs that grow later.
var _ = json.Marshal