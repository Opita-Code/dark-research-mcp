// Package tools — deprecation.go: v0.7.0 deprecation shim.
//
// Per DARK-MEM-004, 38 tools in dark-research-mcp that were duplicates
// of dark-memory-mcp functionality are now wrapped in a deprecation
// envelope. The original handlers are not invoked; instead, the tool
// responds with `{deprecated: true, successor: "dark-memory-mcp", tool: ...,
// migration: ..., removed_in: "v0.8.0"}`.
//
// The wire catalog count is unchanged at 57 (the shim still registers
// in tools/list with the same definition). Harnesses that branch on the
// deprecation envelope can route the call to dark-memory-mcp's peer
// tool. v0.8.0 will remove the shims and the catalog drops to 18.
package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// deprecationSuccessor is the canonical peer module for all 38
// deprecated tools. Per DEPRECATED.md, every shim points here.
const deprecationSuccessor = "dark-memory-mcp"

// deprecationRemovedIn is the next major version that will drop the
// shims. Update this constant when v0.8.0 ships.
const deprecationRemovedIn = "v0.8.0"

// deprecatedTool returns a Tool that responds with the deprecation
// envelope. `name` is the original tool name; `migration` is the
// canonical replacement in dark-memory-mcp (e.g. "dark_memory_research_recall").
// `description` is the new tool description shown in tools/list; it
// explains the deprecation to LLM agents that read the catalog.
//
// Usage in tools.go:
//
//	All(cfg) {
//	    // ... active tools ...
//	    deprecatedTool("dark_mem_recall_research",
//	        "dark_memory_research_recall",
//	        "Recall past dark-research results. DEPRECATED: use dark_memory_research_recall from dark-memory-mcp."),
//	    // ... etc ...
//	}
func deprecatedTool(name, migration, description string) Tool {
	def := mcp.NewTool(name,
		mcp.WithDescription(description),
		// Empty schema: the deprecation shim accepts no arguments.
		// Tools that originally had a required argument still register
		// with a no-op schema; the agent that calls them gets the
		// envelope without having to supply the original arguments.
	)
	// We bypass the BindSimple/bindArgs path because we don't need
	// schema validation for the deprecation envelope. A raw ToolHandlerFunc
	// is sufficient.
	return Tool{
		Definition: def,
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// The deprecation envelope is returned as a JSON-encoded
			// string in the mcp-go content[0].text shape. This matches
			// the convention used by every other tool in this binary:
			// the JSON body is the `text` field of the first content
			// entry, and the harness unwraps it via mcp-go's standard
			// envelope handling.
			return deprecatedEnvelope(name, migration), nil
		},
	}
}

// deprecatedEnvelope is the JSON response shape every shim returns.
// Kept as a function (not a const string) so callers can template in
// the per-tool name and migration target.
//
// Wire shape:
//
//	{
//	  "deprecated": true,
//	  "successor": "dark-memory-mcp",
//	  "tool": "<original tool name>",
//	  "migration": "<canonical replacement>",
//	  "removed_in": "v0.8.0"
//	}
//
// The shape is documented in archive/pre-dark-memory-split/DEPRECATED.md
// and in RELEASE_NOTES_v0.7.0.md.
func deprecatedEnvelope(name, migration string) *mcp.CallToolResult {
	body := `{"deprecated":true,"successor":"` + deprecationSuccessor + `","tool":"` + name + `","migration":"` + migration + `","removed_in":"` + deprecationRemovedIn + `"}`
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: body,
			},
		},
	}
}

// deprecationList is the master list of the 38 deprecated tools and
// their dark-memory-mcp migration targets. Each entry is a tuple of
// (original_name, migration_target, short_description). The list is
// used in tools.go's All() function to generate the deprecatedTools
// slice without repeating the per-tool boilerplate 38 times.
//
// Ordering matches the original All() registration order, so a
// harness indexing by tool name sees no change.
type deprecationEntry struct {
	name        string
	migration   string
	description string
}

func deprecationList() []deprecationEntry {
	return []deprecationEntry{
		// 8 dark_mem_* tools
		{"dark_mem_recall_research", "dark_memory_research_recall",
			"DEPRECATED (v0.7.0): recall past dark-research results. Use dark_memory_research_recall from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_mem_status", "dark_memory_memory_state",
			"DEPRECATED (v0.7.0): aggregate stats over the research store. Use dark_memory_memory_state from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_mem_schema_status", "dark_memory_admin_schema_status",
			"DEPRECATED (v0.7.0): DB schema version + applied migrations. Use dark_memory_admin_schema_status from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_mem_link_research", "dark_memory_federation_lookup",
			"DEPRECATED (v0.7.0): link research across the dark-agents namespace. Use dark_memory_federation_lookup from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_mem_list_runs", "dark_memory_research_recall",
			"DEPRECATED (v0.7.0): list research runs. Use dark_memory_research_recall (with filter_run_id) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_mem_list_items", "dark_memory_research_recall",
			"DEPRECATED (v0.7.0): list research items. Use dark_memory_research_recall from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_mem_export_run", "dark_memory_research_export",
			"DEPRECATED (v0.7.0): export a research run as JSON. Use dark_memory_research_export (planned v1.5.0) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_mem_diff", "dark_memory_pipeline_status",
			"DEPRECATED (v0.7.0): diff two research runs. Use dark_memory_pipeline_status from dark-memory-mcp. Removed in v0.8.0."},

		// 22 vibe-flow CRUD tools
		{"dark_research_spec_create", "dark_memory_vibe_spec",
			"DEPRECATED (v0.7.0): create a vibe-flow spec. Use dark_memory_vibe_spec from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_spec_get", "dark_memory_spec_context",
			"DEPRECATED (v0.7.0): read a vibe-flow spec. Use dark_memory_spec_context from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_spec_list", "dark_memory_vibe_spec",
			"DEPRECATED (v0.7.0): list vibe-flow specs. Use dark_memory_vibe_spec (with no spec body) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_spec_update", "dark_memory_vibe_spec",
			"DEPRECATED (v0.7.0): update a vibe-flow spec. Use dark_memory_vibe_spec (re-publish) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_spec_delete", "dark_memory_vibe_spec",
			"DEPRECATED (v0.7.0): delete a vibe-flow spec. Use dark_memory_vibe_spec (with abort task) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_spec_render", "dark_memory_spec_context",
			"DEPRECATED (v0.7.0): render a vibe-flow spec to a prompt. Use dark_memory_spec_context from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_brand_register", "dark_memory_vibe_publish",
			"DEPRECATED (v0.7.0): register a brand guide. Use dark_memory_vibe_publish from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_brand_get", "dark_memory_active_policy",
			"DEPRECATED (v0.7.0): read a brand guide. Use dark_memory_active_policy from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_brand_list", "dark_memory_active_policy",
			"DEPRECATED (v0.7.0): list brand guides. Use dark_memory_active_policy from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_brand_delete", "dark_memory_vibe_publish",
			"DEPRECATED (v0.7.0): delete a brand guide. Use dark_memory_vibe_publish (with abort) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_compliance_register", "dark_memory_vibe_publish",
			"DEPRECATED (v0.7.0): register a compliance rule. Use dark_memory_vibe_publish from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_compliance_get", "dark_memory_active_policy",
			"DEPRECATED (v0.7.0): read a compliance rule. Use dark_memory_active_policy from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_compliance_list", "dark_memory_active_policy",
			"DEPRECATED (v0.7.0): list compliance rules. Use dark_memory_active_policy from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_artifact_log", "dark_memory_vibe_publish",
			"DEPRECATED (v0.7.0): log an artifact. Use dark_memory_vibe_publish from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_artifact_get", "dark_memory_artifact_context",
			"DEPRECATED (v0.7.0): read an artifact. Use dark_memory_artifact_context from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_artifact_list", "dark_memory_artifact_context",
			"DEPRECATED (v0.7.0): list artifacts. Use dark_memory_artifact_context (with empty id) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_artifact_update", "dark_memory_vibe_publish",
			"DEPRECATED (v0.7.0): update an artifact. Use dark_memory_vibe_publish (re-publish) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_artifact_delete", "dark_memory_vibe_publish",
			"DEPRECATED (v0.7.0): delete an artifact. Use dark_memory_vibe_publish (with abort) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_artifact_download", "dark_memory_artifact_context",
			"DEPRECATED (v0.7.0): download an artifact. Use dark_memory_artifact_context from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_drift_log", "dark_memory_vibe_publish",
			"DEPRECATED (v0.7.0): log a drift report. Use dark_memory_vibe_publish from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_drift_get", "dark_memory_resolve_drift",
			"DEPRECATED (v0.7.0): read a drift report. Use dark_memory_resolve_drift from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_research_drift_list", "dark_memory_pipeline_status",
			"DEPRECATED (v0.7.0): list drift reports. Use dark_memory_pipeline_status from dark-memory-mcp. Removed in v0.8.0."},

		// 8 dark_ssd_* judges
		{"dark_ssd_brand_match", "dark_memory_judge",
			"DEPRECATED (v0.7.0): LLM-as-judge brand match. Use dark_memory_judge(eval_type=brand_match) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_ssd_compliance_check", "dark_memory_judge",
			"DEPRECATED (v0.7.0): LLM-as-judge compliance check. Use dark_memory_judge(eval_type=compliance_check) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_ssd_drift_judge", "dark_memory_judge",
			"DEPRECATED (v0.7.0): LLM-as-judge drift verdict. Use dark_memory_judge(eval_type=drift_judge) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_ssd_grounding_check", "dark_memory_judge",
			"DEPRECATED (v0.7.0): LLM-as-judge grounding check. Use dark_memory_judge(eval_type=grounding_check) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_ssd_pii_detect", "dark_memory_judge",
			"DEPRECATED (v0.7.0): LLM-as-judge PII detect. Use dark_memory_judge(eval_type=pii_detect) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_ssd_prompt_injection_scan", "dark_memory_judge",
			"DEPRECATED (v0.7.0): LLM-as-judge prompt injection scan. Use dark_memory_judge(eval_type=prompt_injection_scan) from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_ssd_consensus", "dark_memory_consensus",
			"DEPRECATED (v0.7.0): N-shot LLM-as-judge consensus. Use dark_memory_consensus from dark-memory-mcp. Removed in v0.8.0."},
		{"dark_ssd_list_evaluations", "dark_memory_judgment_history",
			"DEPRECATED (v0.7.0): list SSD evaluations. Use dark_memory_judgment_history from dark-memory-mcp. Removed in v0.8.0."},
	}
}

// DeprecatedTools is the deprecation shim slice. Returned by All() in
// the same position the originals occupied, so the wire catalog count
// is unchanged at 57.
func DeprecatedTools() []Tool {
	entries := deprecationList()
	out := make([]Tool, 0, len(entries))
	for _, e := range entries {
		out = append(out, deprecatedTool(e.name, e.migration, e.description))
	}
	return out
}

// Ensure the import of server is used (the deprecationEnvelope returns
// mcp.CallToolResult which is the mcp-go type; we use server.ToolHandlerFunc
// indirectly via the Tool struct). This blank import is a no-op when
// the package builds cleanly.
var _ = server.ToolHandlerFunc(nil)
