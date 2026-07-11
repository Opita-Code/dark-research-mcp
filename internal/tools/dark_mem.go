package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// darkMemRecallArgs: search persisted research items by substring.
type darkMemRecallArgs struct {
	Query        string `json:"query" jsonschema:"Substring to match against title, snippet, and source (case-insensitive)"`
	FilterIntent string `json:"filter_intent,omitempty" jsonschema:"Optional: only items from runs with this intent"`
	FilterSource string `json:"filter_source,omitempty" jsonschema:"Optional: only items from this source (e.g. 'osv.dev', 'openalex')"`
	Limit        int    `json:"limit,omitempty" jsonschema:"Max items (default 20)"`
}

func darkMemRecallTool() Tool {
	def := mcp.NewTool("dark_mem_recall_research",
		mcp.WithDescription("Recall past dark-research results from dark.db (substring match on title/snippet/source). Use when starting a new research thread and you want to avoid re-fetching data we've already gathered. Pairs naturally with dark_research: try recall first, fall back to live fetch on miss."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Substring to search for")),
		mcp.WithString("filter_intent", mcp.Description("Optional intent filter (e.g. 'cve', 'ip')")),
		mcp.WithString("filter_source", mcp.Description("Optional source filter (e.g. 'osv.dev')")),
		mcp.WithNumber("limit", mcp.Description("Max items to return (default 20)")),
	)

	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args darkMemRecallArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured; restart with DARK_DB env or pass --db")
			}
			items, err := m.Recall(ctx, args.Query, args.FilterIntent, args.FilterSource, args.Limit)
			if err != nil {
				return nil, err
			}
			out := map[string]any{
				"query": args.Query,
				"count": len(items),
				"items": items,
			}
			return jsonResult(out), nil
		},
	}
}

func darkMemStatusTool() Tool {
	def := mcp.NewTool("dark_mem_status",
		mcp.WithDescription("Aggregate stats over the research store: total runs, total items, total cross-links, intent histogram, source histogram, oldest/newest run. Use to gauge how much research has accumulated."),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			st, err := m.Status(ctx)
			if err != nil {
				return nil, err
			}
			return jsonResult(st), nil
		},
	}
}

func darkMemSchemaStatusTool() Tool {
	def := mcp.NewTool("dark_mem_schema_status",
		mcp.WithDescription("Return the current DB schema version and the applied status of every registered migration. Use to confirm dark.db is at the expected version after upgrading dark-research-mcp."),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			v, err := m.SchemaVersion(ctx)
			if err != nil {
				return nil, err
			}
			status, err := m.MigrationStatus(ctx)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{
				"schema_version": v,
				"migrations":     status,
			}), nil
		},
	}
}

type darkMemLinkArgs struct {
	ItemID     int64  `json:"item_id" jsonschema:"Research item id (from dark_mem_recall_research or dark_research result)"`
	TargetType string `json:"target_type" jsonschema:"Type of target: 'attack' | 'cve' | 'technique' | 'paper'"`
	TargetID   string `json:"target_id" jsonschema:"Target id (e.g. attack_id from dark-eval)"`
	Note       string `json:"note,omitempty" jsonschema:"Free-form context for the link"`
}

func darkMemLinkTool() Tool {
	def := mcp.NewTool("dark_mem_link_research",
		mcp.WithDescription("Cross-link a research item to a dark-eval attack, a CVE id, a technique id, or a paper id. Use after researching a target's vulnerabilities: link the discovered CVE to the attack you're about to run. Use after reading a paper: link it to the technique you're testing. Builds a queryable web of provenance."),
		mcp.WithNumber("item_id", mcp.Required(), mcp.Description("Research item id")),
		mcp.WithString("target_type", mcp.Required(), mcp.Description("attack | cve | technique | paper")),
		mcp.WithString("target_id", mcp.Required(), mcp.Description("Target id")),
		mcp.WithString("note", mcp.Description("Free-form context")),
	)

	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args darkMemLinkArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			if err := m.LinkResearchToAttack(ctx, args.ItemID, args.TargetType, args.TargetID, args.Note); err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{
				"linked":     true,
				"item_id":    args.ItemID,
				"target":     fmt.Sprintf("%s:%s", args.TargetType, args.TargetID),
			}), nil
		},
	}
}

// keep imports used (encoding/json referenced via jsonResult helper).
var _ = json.Marshal

// --- list endpoints for the research layer ---

type darkMemListRunsArgs struct {
	Intent string `json:"intent,omitempty" jsonschema:"Optional: only runs with this intent (web|academic|cve|ip|domain|dns|...)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Max runs to return (default 50)"`
}

func darkMemListRunsTool() Tool {
	def := mcp.NewTool("dark_mem_list_runs",
		mcp.WithDescription("List research runs newest-first with optional intent filter. Use to audit prior research threads (e.g. 'every CVE lookup today')."),
		mcp.WithString("intent", mcp.Description("Optional intent filter")),
		mcp.WithNumber("limit", mcp.Description("Max runs (default 50)")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args darkMemListRunsArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			out, err := m.ListResearchRuns(ctx, args.Intent, args.Limit)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{
				"count": len(out),
				"runs":  out,
			}), nil
		},
	}
}

type darkMemListItemsArgs struct {
	RunID  int64  `json:"run_id,omitempty" jsonschema:"Optional: only items from this run"`
	Source string `json:"source,omitempty" jsonschema:"Optional: only items from this source (e.g. 'osv.dev')"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Max items (default 50)"`
}

func darkMemListItemsTool() Tool {
	def := mcp.NewTool("dark_mem_list_items",
		mcp.WithDescription("List research items newest-first with optional filters by run_id and source. Useful for 'show me every CVE I found earlier today' without running a substring recall."),
		mcp.WithNumber("run_id", mcp.Description("Optional run id filter")),
		mcp.WithString("source", mcp.Description("Optional source filter")),
		mcp.WithNumber("limit", mcp.Description("Max items (default 50)")),
	)
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args darkMemListItemsArgs
			if err := bindArgs(req, &args); err != nil {
				return nil, err
			}
			m := sharedMem()
			if m == nil {
				return nil, fmt.Errorf("mem store not configured")
			}
			out, err := m.ListResearchItems(ctx, args.RunID, args.Source, args.Limit)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{
				"count": len(out),
				"items": out,
			}), nil
		},
	}
}