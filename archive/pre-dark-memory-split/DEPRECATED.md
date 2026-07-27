# DEPRECATED — pre-dark-memory-split (dark-research-mcp v0.7.0)

**Status:** Archived on 2026-07-18 as part of the dark-agents consolidation
(DARK-MEM-001 → DARK-MEM-004).

**Canonical successor:** [`dark-memory-mcp`](https://github.com/Opita-Code/dark-memory-mcp)
(github.com/Opita-Code/dark-memory-mcp) — peer module, same operator, same data
format on shared tables.

---

## What was deprecated

The memory + vibe-flow + SSD layer that previously lived inside
`dark-research-mcp` was a prototype / early implementation that has since
been extracted into a peer Go module (`dark-memory-mcp`). The deprecated
layer in `dark-research-mcp` covered three namespaces:

### 1. `dark_mem_*` tools (8 tools)

| Tool | Replacement |
|---|---|
| `dark_mem_recall_research` | `dark_memory_research_topic` + `dark_memory_research_recall` |
| `dark_mem_status` | `dark_memory_memory_state` |
| `dark_mem_schema_status` | `dark_memory_admin_schema_status` |
| `dark_mem_link_research` | (use the cross-namespace lookup `dark_memory_federation_lookup`) |
| `dark_mem_list_runs` | `dark_memory_research_recall` with `filter_run_id` |
| `dark_mem_list_items` | `dark_memory_research_recall` |
| `dark_mem_export_run` | `dark_memory_research_export` (planned; v1.5.0) |
| `dark_mem_diff` | `dark_memory_pipeline_status` + federation |

### 2. `dark_research_spec_*`, `_brand_*`, `_compliance_*`, `_artifact_*`, `_drift_*` (vibe-flow CRUD, 22 tools)

All 22 vibe-flow CRUD tools (`dark_research_spec_create`, `dark_research_brand_get`,
`dark_research_compliance_list`, `dark_research_artifact_log`, `dark_research_drift_log`,
…) are now in `dark-memory-mcp` under the `vibe_*` and `dark_memory_*` namespaces.
The `dark_research_*` prefix is retained only for the OSINT-intent tools
(`dark_research_web`, `dark_research_cve`, `dark_research_ip`, …).

### 3. `dark_ssd_*` judges (8 tools + 1 list)

The 8 LLM-as-judge tools (`dark_ssd_brand_match`, `dark_ssd_compliance_check`,
`dark_ssd_drift_judge`, `dark_ssd_grounding_check`, `dark_ssd_pii_detect`,
`dark_ssd_prompt_injection_scan`, `dark_ssd_consensus`, plus the
`dark_ssd_list_evaluations` list) live in `dark-memory-mcp` as
`dark_memory_judge` + `dark_memory_consensus` + `dark_memory_judgment_history`.

---

## What stayed in dark-research-mcp

The OSINT core (13 intent-specialized tools + 4 standalone tools) is the
canonical purpose of `dark-research-mcp` going forward:

| Namespace | Count | Tools |
|---|---|---|
| OSINT intents | 13 | `dark_research_web`, `_academic`, `_code`, `_cve`, `_domain`, `_dns`, `_cert`, `_ip`, `_threat`, `_email`, `_dark`, `_geo`, `_news` |
| Multi-intent | 1 | `dark_research_multi` |
| Standalone | 4 | `web_search`, `web_fetch`, `url_extract_components`, `text_anonymize` |

Total: 18 tools (down from 57). The OSINT core is unchanged in behaviour.

---

## Migration guide

For operators currently using the deprecated tools:

1. Install `dark-memory-mcp` alongside `dark-research-mcp` (they are peer
   modules; both register as separate `mcp` entries in `opencode.jsonc`).
2. Replace `dark_mem_*` calls with the corresponding `dark_memory_*` calls
   (table above).
3. Replace `dark_research_spec_*` / `_brand_*` / `_compliance_*` /
   `_artifact_*` / `_drift_*` with the `dark_memory_vibe_spec`,
   `dark_memory_vibe_publish`, `dark_memory_artifact_context`,
   `dark_memory_resolve_drift`, `dark_memory_pipeline_status` tools.
4. Replace `dark_ssd_*` with `dark_memory_judge` (single-shot) or
   `dark_memory_consensus` (N-shot).
5. The cross-namespace `dark_memory_federation_lookup` (opt-in via
   `DARK_FEDERATION_PEER_DSN`) replaces the in-binary `dark_mem_link_research`
   with a read-only bridge to the peer module.

For the v0.7.0 release window, the deprecated tools in `dark-research-mcp`
are kept as **compatibility shims** that respond with
`{deprecated: true, successor: "dark-memory-mcp", tool: "..."}`. This
gives operators a deprecation grace period to migrate without losing
functionality. The shims are removed in v0.8.0.

---

## File map (Phase 2 — for the future refactor)

When dark-research-mcp moves to v0.8.0, the following files move to
`archive/pre-dark-memory-split/`:

- `internal/tools/dark_mem.go` (8 dark_mem_* tool handlers)
- `internal/tools/export_diff.go` (dark_mem_export_run + dark_mem_diff)
- `internal/tools/vibeflow_data.go` (the 22 vibe-flow CRUD tools)
- `internal/tools/ssd.go` (8 dark_ssd_* judges — split into archive/ssd/)
- `internal/mem/recall.go` (research-item recall — superseded by dark_memory_research_recall)
- `internal/mem/ssd.go` (SSD evaluators — superseded by dark_memory_judge)
- `internal/mem/vibeflow.go` (vibe-flow persistence — superseded by dark_memory_vibe_*)

The shared research-store code stays in `internal/mem/`:

- `internal/mem/store.go` (Store type, SaveRun, etc.)
- `internal/mem/types.go` (ResearchRun, Item, BackendError, etc. — split into a minimal active subset)
- `internal/mem/migrate.go` (research schema migrations)
- `internal/mem/schema.go` (research schema constants)

`internal/research/router.go` continues to depend on the shared
research-store subset. The split is a v0.8.0 refactor and is not part
of v0.7.0.

---

## Why archive and not delete

Per the dark-memory-mcp `release-integrity@1.0.0` constitution (Rule 2:
archive, do not delete), the deprecated files are moved to this archive
directory with the deprecation context preserved. Deleting the code
without a trail would:

- Lose the design history of why the split was made
- Break the audit chain (`dark_agents.harvest` references some of these
  symbols in fixtures)
- Make the migration guide unverifiable (no diff to point to)

The archive is committed to git and remains in the repository history
indefinitely. The compatibility shims in v0.7.0 still import from the
archive package (read-only) to ensure the deprecation message is
generated from the canonical source.

---

## Contact

Questions about the split: see the dark-agents-v2 working group notes
at `Documents\dark-mem-research-workspace\` (workspace used for the
state-of-the-art research that informed the split decision).
