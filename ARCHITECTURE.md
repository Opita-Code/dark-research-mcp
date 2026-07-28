# dark-research-mcp v0.8.0 — Architecture

**Version**: v0.8.0
**Spec**: `BRIDGE_AND_COEXISTENCE.md` v2.0.0 (cx.v3 conformance)
**Status**: Tool backing for the dark-agents/memory coexistence group. Demoted from sibling surface (cx.v2) to backing (cx.v3) per BRIDGE §3.2.

---

## Role in dark-agents (cx.v3)

Under cx.v3, dark-memory-mcp is the **policy gateway** for the dark-agents coexistence
group. dark-research-mcp is a **tool backing** that provides OSINT capabilities
which the gateway composes into persona-shaped, capability-checked, drift-audited
responses.

```
┌─────────────────┐
│  LLM (opencode) │
└────────┬────────┘
         │ tools/call dark_research_cve(...)
         ▼
┌──────────────────────────┐
│  dark-memory gateway     │  ← policy_gateway=true
│  ├ pre-hook: frame       │
│  ├ capability check      │
│  ├ scope check           │
│  ├ compose persona       │
│  ├ invoke backing ───────┼──┐
│  ├ post-hook: drift      │  │
│  └ emit audit            │  │
└──────────────────────────┘  │
                              ▼
                ┌──────────────────────────┐
                │  dark-research backing   │  ← policy_gateway=false
                │  - 13 OSINT intents      │
                │  - 19 active tools       │
                │  - 38 frozen shims       │
                └──────────────────────────┘
```

Direct `dark_research_*` calls from the harness still work (legacy fallback). Under
cx.v3 active mode, the harness SHOULD route them through the gateway.

---

## Tool surface (57 registered, 19 active + 38 frozen shims)

| Family | Count | Status | Notes |
|---|---:|---|---|
| OSINT meta router | 1 | **active** | `dark_research` — auto-classifies query → intent → backend chain |
| OSINT intents | 13 | **active** | web, academic, code, cve, domain, dns, cert, ip, threat, email, dark, geo, news |
| OSINT multi | 1 | **active** | `dark_research_multi` — parallel fanout across intents |
| Standalone | 4 | **active** | web_search, web_fetch, url_extract_components, text_anonymize |
| **Effective active** | **19** | | |
| dark_mem_* shims | 8 | **frozen** | recall_research, status, schema_status, link_research, list_runs, list_items, export_run, diff |
| dark_research_{spec,brand,compliance,artifact,drift}_* shims | 22 | **frozen** | migrated to dark_memory_* per BRIDGE §2.2 |
| dark_ssd_* judges shims | 8 | **frozen** | brand_match, compliance_check, drift_judge, grounding_check, pii_detect, prompt_injection_scan, consensus, list_evaluations |
| **Total wire catalog** | **57** | | (unchanged for harness backward compat) |

The 38 shims respond with `{deprecated: true, successor: "dark-memory-mcp", ...}`.
Removal is planned for a later major release.

---

## Coexistence declarations (v0.8.0 conformance)

The `initialize` response from this binary carries the following metadata via the MCP
`instructions` channel (mcp-go v0.56.0's `Implementation` struct does not support custom
fields — see BRIDGE §2.1 for the upstream-tracking rationale):

```
dark-research-mcp server. coexistence_group=dark-agents/research
policy_gateway=false (spec 164 bridge.2 cx.v3). ...
```

Conformance tests live in `internal/server/server_test.go`:

- `TestBuildInstructions_DeclaresCoexistenceGroup` — asserts `coexistence_group=dark-agents/research`
- `TestBuildInstructions_DeclaresPolicyGateway` — asserts `policy_gateway=false`
- `TestBuildInstructions_MentionsDarkMemoryAsSuccessor` — asserts `dark_mem_*` migration pointer
- `TestBuildInstructions_IncludesVersion` — asserts version stamping
- `TestBuildInstructions_StampsCxV3` — asserts spec 164 bridge.2 cx.v3 reference

---

## Layout

```
dark-research-mcp/
  cmd/dark-research-mcp/main.go   entry point: config + mem + server
  cmd/inspect-schema/              one-shot CLI that dumps the schema as JSON
  cmd/mock-llm/                   mock LLM server for tests
  cmd/probe-daemon/                health-probe daemon
  internal/
    config/                        YAML / env / flag configuration
    constitution/                  constitution loader + store (cerebro/1.1.0, etc.)
    llm/                           MiniMax-M3 client (Anthropic-compatible)
    mem/                           SQLite persistence + migrations (research, vibe, ssd)
      schema.go                    package doc + version comment
      store.go                     Open / Close / Exec / QueryRow
      migrate.go                   versioned migrations, Migrate(), SchemaVersion()
      recall.go                    research_runs / research_items CRUD
      vibeflow.go                  vibe_* CRUD (spec, brand, compliance, artifact, drift)
      ssd.go                       sdd_evaluations CRUD
      types.go                     Go structs with snake_case json tags
    research/                      OSINT backends (23) + intent router
      router.go                    auto-classifies query → intent → backend chain
      backends_defs.go             each backend's URL builder + parser
      intent.go                    Intent enum + classification heuristic
    safety/                        SSRF guard + L7 defense layer
    server/                        MCP server wiring
      server.go                    NewMCPServer with cx.v3 instructions
      server_test.go               cx.v3 conformance tests (NEW v0.8.0)
    mods/                          data-only mod loader (research mods)
    tools/                         one MCP tool per public function
      dark_research.go             14 OSINT tools (router + 13 intents)
      deprecation.go               38 deprecation shims (frozen)
      dark_mem.go                  8 dark_mem_* legacy shims
      web_search.go / web_fetch.go / url_extract.go / html.go / http_client.go
      common.go                    JSON helpers, shared mem accessor
      tools.go                     All() registration list (57 tools)
    vault/                         secrets auto-load from dark-agents vault
  go.mod                           module github.com/dark-agents/research-mcp
```

---

## Backend registry (23 backends across 13 intents)

| Intent | Backends | Default first-try |
|---|---|---|
| `web` | duckduckgo, searxng, brave | duckduckgo (HTML scraping; no auth) |
| `academic` | openalex, arxiv, semanticscholar | openalex (240M+ works; no auth) |
| `code` | cratesio, npm, github | cratesio (Rust; no auth) |
| `cve` | osv, nvd | osv (Google's curated vuln db; no auth) |
| `domain` | rdap | rdap (IANA bootstrap) |
| `dns` | cloudflare-doh, google-doh | cloudflare-doh |
| `cert` | crtsh | crtsh (crt.sh JSON endpoint) |
| `ip` | ipapi, ripe | ipapi (45 req/min) |
| `threat` | abusech, otx | abusech (URLhaus public endpoint) |
| `email` | hibp, leakcheck | hibp (requires HIBP_API_KEY) |
| `dark` | ahmia | ahmia (clearnet index of .onion) |
| `geo` | osm-nominatim | nominatim (OpenStreetMap) |
| `news` | gdelt, wayback | gdelt (global news graph) |

**Backend health** (snapshot from `BACKEND_STATUS.md`):
- ✅ 14/16 healthy on last probe (2026-07-11)
- ⚠️ 2 degraded: `crtsh` (502), `gdelt` (10s timeout)
- ⚠️ 2 require auth: `hibp` (HIBP_API_KEY), `otx` (AlienVault OTX key)

The router's stop-at-first-success policy (see `internal/research/router.go:62`) means a
healthy primary backend masks a broken secondary. This is the primary motivator for the
Fase 3 router enhancements in the v0.8.0 vibe_spec (multi-backend merge + dedup).

---

## Persistence (shared `dark.db`)

dark-research writes to `research_runs` and `research_items` (its own tables). It reads
from dark-memory-owned tables (`vibe_*`, `sessions`, `write_audit`, `agent_memory`)
**only via direct SQL for diagnostics** — never writes to them.

| Table group | Owner (writes) | Readers |
|---|---|---|
| `research_runs`, `research_items`, `research_links` | **dark-research** | dark-memory (read for cross-link) |
| `vibe_specs`, `vibe_brands`, `vibe_compliance`, `vibe_artifacts`, `vibe_drift_reports` | dark-memory | (none — frozen shims in dark-research) |
| `sdd_evaluations` | dark-memory | (none — judges consolidated in dark-memory v1.4.0) |
| `constitutions`, `mods`, `mod_loads` | dark-memory | dark-research (read for active mod injection) |
| `sessions`, `write_audit`, `agent_memory` | dark-memory | (none from dark-research) |
| `schema_migrations` | dark-memory | dark-research (read for schema version) |

Write invariant: only the owner writes. dark-research fails fast if it sees its tables
migrated by dark-memory.

---

## Harness compatibility

dark-research-mcp speaks MCP over stdio with the standard
`initialize → notifications/initialized → tools/call` JSON-RPC framing. Every AI coding
harness that supports that protocol is a first-class consumer without any wrapper script
or fork. The binary auto-loads credentials from the dark-agents vault on startup (see
`internal/vault/vault.go` LoadIntoEnv) and degrades cleanly when no key is present.

| Harness | Transport | Install |
|---|---|---|
| OpenCode | stdio | `~/.config/opencode/opencode.jsonc` `mcp.dark-research.command` |
| Claude Code | stdio | `claude mcp add --transport stdio dark-research -- <exe>` |
| Cursor | stdio | Settings > MCP > command = `<exe>` |
| Aider | stdio (MCP Code Mode) | `aider --mcp-config <yaml>` |
| Cline | stdio | Cline > MCP Servers > Add > command = `<exe>` |

Under cx.v3 active mode (dark-memory declares `policy_gateway=true`), harnesses SHOULD
route `dark_*` calls through the gateway. The harness detects this from the `initialize`
response's `instructions` field.

---

## LLM-less mode / graceful degradation

Without `SDD_LLM_API_KEY` (and no fallback key in env or the dark-agents vault):

- **19 OSINT tools work full-strength** — never call the LLM.
- **38 dark_mem_* / dark_ssd_* / vibe_* shims** respond with deprecation envelopes (no LLM
  call needed for the envelope itself).

This is the same behavior as v0.7.x. v0.8.0 adds no new LLM-dependent features.

---

## What changed in v0.8.0 vs v0.7.x

| Aspect | v0.7.x | v0.8.0 |
|---|---|---|
| `coexistence_group` declared | (omitted) | `dark-agents/research` |
| `policy_gateway` declared | (omitted) | `false` |
| Coexistence version | cx.v2 (sibling) | cx.v3 (backing) |
| Active tool count | 19 | 19 (unchanged) |
| Frozen shim count | 38 | 38 (unchanged) |
| OSINT backends | 23 | 23 (unchanged) |
| `dark.db` ownership | partial (research_* only) | partial (research_* only; dark-memory owns the rest) |
| Default binary version | `0.7.1` | `0.8.0` |
| Test files | 9 packages | 9 packages + `internal/server/server_test.go` (NEW, 5 tests) |

The Fase 3 enhancements (multi-backend merge, dedup, persistence-aware recall, LLM
synthesis) are tracked separately in the v0.8.0 vibe_spec (spec_id 664 tasks F3.1-F3.6)
and will ship as a v0.8.x follow-up.

---

## License

MIT (Opita Code). See [`LICENSE`](LICENSE).
