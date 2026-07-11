package tools

import (
	"sort"
	"strings"
	"testing"

	"github.com/dark-agents/research-mcp/internal/config"
)

// TestCatalog verifies the full set of MCP tools registered by dark-research-mcp.
//
// This is the regression guard against the v0.2 launch mistake: dropping or
// silently renaming a tool during refactors. The contract is:
//
//   - Each tool's name appears in `expected` exactly once.
//   - The catalog `All()` returns exactly `len(expected)` tools (no orphans).
//   - Every tool has a non-empty description (LLM clients surface these).
//   - Tool names use the dark_ namespace (or web_ / text_ for the small
//     standalone set), snake_case, and have no duplicates.
func TestCatalog(t *testing.T) {
	cfg := config.Config{}
	catalog := All(cfg)

	names := make([]string, 0, len(catalog))
	seen := make(map[string]int)
	for _, tl := range catalog {
		if tl.Definition.Name == "" {
			t.Errorf("tool has empty Name (handler attached? %v)", tl.Handler != nil)
			continue
		}
		if strings.TrimSpace(tl.Definition.Description) == "" {
			t.Errorf("tool %q has empty Description", tl.Definition.Name)
		}
		names = append(names, tl.Definition.Name)
		seen[tl.Definition.Name]++
	}

	// No duplicates.
	for name, n := range seen {
		if n > 1 {
			t.Errorf("tool %q appears %d times in catalog (duplicate registration)", name, n)
		}
	}

	// Every registered tool must be in the expected list, and vice versa.
	// The expected list is the single source of truth for the public API.
	expected := []string{
		// Meta router.
		"dark_research",
		// 13 intent-specialized tools.
		"dark_research_web",
		"dark_research_academic",
		"dark_research_code",
		"dark_research_cve",
		"dark_research_domain",
		"dark_research_dns",
		"dark_research_cert",
		"dark_research_ip",
		"dark_research_threat",
		"dark_research_email",
		"dark_research_dark",
		"dark_research_geo",
		"dark_research_news",
		// Multi-intent.
		"dark_research_multi",
		// Memory layer (6 tools + 2 in Tier 2 = 8 total).
		"dark_mem_recall_research",
		"dark_mem_status",
		"dark_mem_schema_status",
		"dark_mem_link_research",
		"dark_mem_list_runs",
		"dark_mem_list_items",
		"dark_mem_export_run",
		"dark_mem_diff",
		// vibe-flow CRUD: specs (6).
		"dark_research_spec_create",
		"dark_research_spec_get",
		"dark_research_spec_list",
		"dark_research_spec_update",
		"dark_research_spec_delete",
		"dark_research_spec_render",
		// vibe-flow CRUD: brands (4).
		"dark_research_brand_register",
		"dark_research_brand_get",
		"dark_research_brand_list",
		"dark_research_brand_delete",
		// vibe-flow CRUD: compliance (3).
		"dark_research_compliance_register",
		"dark_research_compliance_get",
		"dark_research_compliance_list",
		// vibe-flow CRUD: artifacts (6).
		"dark_research_artifact_log",
		"dark_research_artifact_get",
		"dark_research_artifact_list",
		"dark_research_artifact_update",
		"dark_research_artifact_delete",
		"dark_research_artifact_download",
		// vibe-flow CRUD: drift (3).
		"dark_research_drift_log",
		"dark_research_drift_get",
		"dark_research_drift_list",
		// dark-ssd: 7 LLM-as-judge tools (including consensus from v0.3).
		"dark_ssd_brand_match",
		"dark_ssd_compliance_check",
		"dark_ssd_drift_judge",
		"dark_ssd_grounding_check",
		"dark_ssd_pii_detect",
		"dark_ssd_prompt_injection_scan",
		"dark_ssd_consensus",
		"dark_ssd_list_evaluations",
		// Standalone tools.
		"web_search",
		"web_fetch",
		"url_extract_components",
		"text_anonymize",
	}
	sort.Strings(expected)
	got := append([]string(nil), names...)
	sort.Strings(got)

	if len(got) != len(expected) {
		t.Errorf("catalog size: got %d, want %d", len(got), len(expected))
		t.Logf("got:      %v", got)
		t.Logf("expected: %v", expected)
	}

	// Set diff to surface exactly which names are missing/extra.
	gotSet := make(map[string]bool, len(got))
	for _, n := range got {
		gotSet[n] = true
	}
	for _, n := range expected {
		if !gotSet[n] {
			t.Errorf("expected tool missing from catalog: %q", n)
		}
	}
	for _, n := range got {
		want := false
		for _, e := range expected {
			if e == n {
				want = true
				break
			}
		}
		if !want {
			t.Errorf("unexpected tool in catalog: %q (forgot to add to expected list?)", n)
		}
	}
}

// TestCatalogNamingConvention enforces that all dark_* tool names are
// snake_case and use only [a-z0-9_]. The standalone web_/text_/url_ tools
// follow the same rule. This catches typos like "dark_research-Artifact_DL".
func TestCatalogNamingConvention(t *testing.T) {
	cfg := config.Config{}
	for _, tl := range All(cfg) {
		name := tl.Definition.Name
		if name == "" {
			continue // already flagged by TestCatalog
		}
		for _, r := range name {
			ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
			if !ok {
				t.Errorf("tool name %q contains non-snake-case character %q", name, r)
				break
			}
		}
	}
}