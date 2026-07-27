# dark-research-mcp v0.7.0

**Deprecation grace window for the memory + vibe-flow + SSD layer.**

Builds on v0.6.0's multi-harness + vault auto-load + graceful degradation
work. v0.7.0 does NOT remove functionality; it marks the in-binary
memory layer as deprecated and steers operators toward the peer module
`dark-memory-mcp`.

## What changed

### Deprecation shim on 38 tools

The following 38 tools in `dark-research-mcp` now respond with a
deprecation envelope instead of their original behaviour:

- **8 `dark_mem_*` tools** (research-item recall / status / link / export / diff)
- **22 `dark_research_{spec,brand,compliance,artifact,drift}_*` tools** (vibe-flow CRUD)
- **8 `dark_ssd_*` judges** (LLM-as-judge brand_match, compliance_check,
  drift_judge, grounding_check, pii_detect, prompt_injection_scan,
  consensus, list_evaluations)

The deprecation response shape is:

```json
{
  "deprecated": true,
  "successor": "dark-memory-mcp",
  "tool": "<original tool name>",
  "migration": "<canonical replacement tool name>",
  "removed_in": "v0.8.0"
}
```

Operators querying the deprecation response can branch on
`deprecated: true` to log a warning or trigger a migration workflow.

### Active surface stays at 18 OSINT tools

The canonical tool count drops from 57 to 18 effective tools (the 38
deprecation shims still register but respond with the deprecation
envelope; the wire catalog count is unchanged at 57 to avoid breaking
harnesses that index by position).

| Namespace | Count | Tools |
|---|---|---|
| Meta router | 1 | `dark_research` |
| OSINT intents | 13 | `dark_research_web`, `_academic`, `_code`, `_cve`, `_domain`, `_dns`, `_cert`, `_ip`, `_threat`, `_email`, `_dark`, `_geo`, `_news` |
| Multi-intent | 1 | `dark_research_multi` |
| Standalone | 4 | `web_search`, `web_fetch`, `url_extract_components`, `text_anonymize` |
| **Effective active** | **18** | (the 18 above) |
| Deprecation shims | 38 | (1 dark_mem meta + 8 dark_mem_* + 22 vibe-flow CRUD + 8 dark_ssd_*) — respond with `{deprecated: true, ...}` |
| **Total registered** | **57** | (unchanged wire count) |

The active 18 are unchanged in behaviour. The 38 shims emit the
deprecation envelope as their `data` payload, with `error: null`.

### Archive directory established

`archive/pre-dark-memory-split/DEPRECATED.md` documents what moved,
why, and the migration guide. The actual file moves are deferred to
v0.8.0 — see the DEPRECATED.md "File map (Phase 2)" section.

## Why this is not breaking

Harnesses that index by tool name (most LLM agents) keep working:
the 38 deprecated tools still appear in `tools/list` and respond with
a JSON shape. The only difference is the `data` field is now the
deprecation envelope instead of the original payload. Harnesses that
inspect `data.<original_field>` will see `data == null` and should
branch on `deprecated: true` to handle the migration.

Harnesses that count the tool surface (e.g. tests asserting exactly
57 tools) still see 57. v0.8.0 will be the breaking release that
removes the shims and drops the count.

## Migration

See `archive/pre-dark-memory-split/DEPRECATED.md` for the per-tool
replacement table. TL;DR: install `dark-memory-mcp` alongside
`dark-research-mcp` and replace deprecated tool calls with their
`dark_memory_*` equivalents.

## Upgrade guide

No data migration required. Operators upgrading from v0.6.0:

1. `git pull` and rebuild (`go build -o dark-research-mcp ./cmd/dark-research-mcp`).
2. (Recommended) Install `dark-memory-mcp` peer module and wire it
   into `opencode.jsonc`. See
   [dark-memory-mcp README](https://github.com/Opita-Code/dark-memory-mcp).
3. (Optional) Set `DARK_FEDERATION_PEER_DSN` to enable
   `dark_memory_federation_lookup` for cross-namespace lookups.
4. Run your usual CI. The 38 deprecated tools respond with the
   deprecation envelope; if you have any tests that inspect their
   payloads, update them to expect the envelope.

## Versioning

`dark-research-mcp` stays at its own semver track. v0.7.0 is the
deprecation-grace release. v0.8.0 (Q4 2026) is the planned removal
release; before v0.8.0 ships, the 38 deprecated tools will be moved
to `archive/pre-dark-memory-split/` and removed from the wire catalog.

## Related

- DARK-MEM-001 (dark-memory-mcp): established `release-integrity@1.0.0`
  constitution; tagged v1.3.1 retroactively at `fbc5c03`.
- DARK-MEM-002 (dark-memory-mcp): single-source version resolver
  (`internal/version`) + `Makefile` + `scripts/inject-version.{sh,ps1}`.
- DARK-MEM-003 (dark-memory-mcp): drift detection in
  `dark_memory_health_ping` (git block + `drift` bool).
- DARK-MEM-004 (this release): deprecation shim on 38 tools in
  dark-research-mcp + archive directory.

## License

MIT. See `LICENSE`.
