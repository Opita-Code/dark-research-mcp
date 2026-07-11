# dark-research-mcp

OSINT, vibe-flow CRUD, and LLM-as-judge in a single MCP server. Built for
the dark-agents-v2 red-team framework (opencode fork).

> Single source of truth: `dark.db` (SQLite, shared with dark-eval).
> Single API: `dark-research-mcp.exe` (44 MCP tools over stdio).
> Single LLM: MiniMax-M3 via the Anthropic-compatible API.

## Layout

```
dark-research-mcp/
  cmd/dark-research-mcp/main.go    entry point, wires config + mem + server
  cmd/inspect-schema/              one-shot CLI that dumps the schema as JSON
  internal/
    config/                        YAML / env / flag configuration
    llm/                           MiniMax-M3 client (Anthropic-compatible)
    mem/                           SQLite persistence + migrations
      schema.go                    package doc + version comment
      store.go                     Open / Close / Exec / QueryRow
      migrate.go                   versioned migrations, Migrate(), SchemaVersion()
      recall.go                    research_runs / research_items CRUD
      vibeflow.go                  vibe_* CRUD (spec, brand, compliance, artifact, drift)
      ssd.go                       sdd_evaluations CRUD
      types.go                     Go structs with snake_case json tags
    research/                      OSINT backends (13) + intent router
      router.go                    auto-classifies query → intent → backend chain
      backends_defs.go             each backend's URL builder + parser
      httpx.go                     shared HTTP client (clearnet + tor)
    safety/                        SSRF guard for web_fetch / url_extract_components
    server/                        MCP server wiring
    tools/                         one MCP tool per public function
      dark_research.go             14 OSINT tools (router + 13 intents)
      dark_mem.go                  5 memory tools (recall, status, schema_status, link, list_runs, list_items)
      vibeflow_data.go             15 vibe-flow tools (5 tables × create/get/list)
      ssd.go                       5 dark-ssd LLM-as-judge tools
      web_search.go / web_fetch.go / url_extract.go / html.go / http_client.go
      common.go                    JSON helpers, shared mem accessor
      tools.go                     All() registration list (44 tools)
  .github/workflows/go-test.yml    CI: vet / build / test (-race) on Go 1.22 + 1.23
  go.mod                           module github.com/dark-agents/research-mcp
```

## Three-layer schema (one SQLite file)

`dark.db` holds three logically distinct layers, all in the same file so
cross-table joins are possible via direct SQL when needed.

```
┌────────────────────────────────────────────────────────────────────┐
│ 1. Red-team layer    (pre-existing; from dark-eval)                │
│    findings, attacks, responses, profiles, models, techniques,    │
│    papers, sessions, audit                                          │
├────────────────────────────────────────────────────────────────────┤
│ 2. Research layer    (dark-research-mcp)                           │
│    research_runs (run metadata)                                    │
│    research_items (one row per result)                             │
│    research_links (cross-link to attack/cve/technique/paper)       │
├────────────────────────────────────────────────────────────────────┤
│ 3. vibe-flow layer   (dark-research-mcp)                           │
│    vibe_specs         (declarative intent: constitution + spec +   │
│                        tasks; one row per case)                    │
│    vibe_brands        (PRIMARY KEY on brand_id; voice + visual +   │
│                        narrative + compliance)                     │
│    vibe_compliance    (PRIMARY KEY on jurisdiction; one row each)  │
│    vibe_artifacts     (one row per generated artifact)             │
│    vibe_drift_reports (LLM-as-judge spec-vs-artifact comparison)   │
├────────────────────────────────────────────────────────────────────┤
│ 4. dark-ssd layer    (LLM-as-judge verdicts)                       │
│    sdd_evaluations    (every brand_match, compliance_check,        │
│                        drift_judge, grounding_check verdict)       │
└────────────────────────────────────────────────────────────────────┘
```

### Naming

`vibe_*` tables avoid collision with dark-eval's existing `specs` table
(vendor / model / version columns, different concept). The Go type uses
`VibeCase` and the SQL column is `vibe_case` because `case` is a
reserved word in SQL.

## 44 MCP tools

| Family | Count | Tools |
|---|---|---|
| OSINT | 15 | `dark_research` (router), `dark_research_<13 intents>`, `dark_research_multi` |
| memory | 5 | `dark_mem_recall_research`, `dark_mem_status`, `dark_mem_schema_status`, `dark_mem_link_research`, `dark_mem_list_runs`, `dark_mem_list_items` |
| vibe-flow CRUD | 15 | 5 tables × {create, get, list} (spec, brand, compliance, artifact, drift) |
| dark-ssd | 5 | `dark_ssd_brand_match`, `dark_ssd_compliance_check`, `dark_ssd_drift_judge`, `dark_ssd_grounding_check`, `dark_ssd_list_evaluations` |
| standalone | 4 | `web_search`, `web_fetch`, `url_extract_components`, `text_anonymize` |

JSON contract: every tool emits snake_case. Go types have explicit
`json:"snake_case"` tags and `jsonschema` tags for the MCP layer.

## vibe-flow production pipeline (7-case taxonomy)

Universal flow:
```
1. constitution → 2. spec → 3. tasks → 4. generate → 5. validate
6. reconcile drift → 7. human gate → 8. publish
```

For any creative task (especially C2–C7):
1. **Register brand** (once): `dark_research_brand_register(brand_id, voice, visual, narrative, compliance)`
2. **Register jurisdiction** (once, mandatory for C4 video / C6 campaign):
   `dark_research_compliance_register(jurisdiction="EU", rules=..., effective_at="2026-08-02")`
3. **Persist spec**: `dark_research_spec_create(case_kind, constitution, spec, tasks)` → `spec_id`
4. **Generate** the artifact (case-specific tool, offloaded to user-provided service)
5. **Log artifact**: `dark_research_artifact_log(artifact_url, spec_id, brand_id, jurisdiction, has_disclosure)` → `artifact_id`
6. **LLM-as-judge brand fit**: `dark_ssd_brand_match(content, brand_id)` — gate before publishing
7. **LLM-as-judge compliance**: `dark_ssd_compliance_check(content, jurisdiction)` — gate, esp. for EU
8. **LLM-as-judge drift**: `dark_ssd_drift_judge(artifact_id, artifact_text)` — close the drift loop
9. **Log drift verdict**: `dark_research_drift_log(artifact_id, verdict, judge_reasoning, reconciled_at?)`
10. **Human gate** if any check failed

EU AI Act 2026-08-02: $51,744/violation for missing disclosure. For C4
video, set `has_disclosure=true` in artifact_log BEFORE publishing.

### Seven cases

| Case | Domain | Example |
|---|---|---|
| C1 | code | "write a Python script that..." |
| C2 | text | "draft a 1-page landing page copy" |
| C3 | image | "render a hero shot for the launch" |
| C4 | video | "produce a 30s product demo" |
| C5 | audio | "narrate the demo script" |
| C6 | multi-modal | "build an Instagram ad: image + caption + CTA" |
| C7 | mixed | "ship the product launch: code + landing page + ad" |

## dark-ssd LLM-as-judge layer

All four judges use the same `internal/llm/client.go` (Anthropic-compatible
client) with env-based config:

| env | default | used for |
|---|---|---|
| `SDD_LLM_API_KEY` | (none; falls back to `MINIMAX_API_KEY`) | auth |
| `SDD_LLM_BASE_URL` | `https://api.minimax.io/anthropic` | endpoint |
| `SDD_LLM_MODEL` | `MiniMax-M3` | model id |

Each judge:
1. Fetches context from `dark.db` (brand guide, compliance rule, spec, source URL)
2. Calls the LLM with a JSON-only system prompt
3. Persists the verdict + confidence + model in `sdd_evaluations`
4. Returns the structured verdict to the agent

| Tool | Input | Output schema |
|---|---|---|
| `dark_ssd_brand_match` | content, brand_id | `{match: 0-1, voice_match: bool, issues[], reasoning}` |
| `dark_ssd_compliance_check` | content, jurisdiction | `{compliant: bool, issues[], required_disclosures[], reasoning}` |
| `dark_ssd_drift_judge` | artifact_id, artifact_text? | `{verdict: aligned\|drift_detected\|needs_human, drift_items[], confidence, reasoning}` |
| `dark_ssd_grounding_check` | claim, source_url | `{grounded: bool, confidence, evidence, issues}` |
| `dark_ssd_list_evaluations` | eval_type?, target_type?, limit? | `[]SDDEvaluation` (audit) |

## Migrations

Versioned via `internal/mem/migrate.go`. On `Open()`:

1. `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`
2. Read applied versions
3. Apply each pending migration in `AllMigrations` order, each in its own tx
4. Record applied versions

Inspect with `dark_mem_schema_status`:
```
{ "schema_version": 1, "migrations": [{ "version": 1, "name": "initial_schema", "applied": true, "applied_at": "..." }] }
```

To add a migration:
```go
var AllMigrations = []Migration{
    {Version: 1, Name: "initial_schema", Up: schemaV1, Down: "..."},
    {Version: 2, Name: "add_sdd_prompt_index", Up: "CREATE INDEX IF NOT EXISTS idx_sdd_prompt_version ON sdd_evaluations(prompt_version)", Down: "DROP INDEX IF EXISTS idx_sdd_prompt_version"},
}
```

## Token economy (Atlan 2026 framework)

The agent has access to a `vibe_economize` opencode tool (TS) that runs a
default pipeline before passing OSINT result sets to LLM context:

```
dedup → filter_confidence(0.5) → truncate(500) → compress → cap(10)
```

Reduces 50K-token dumps to ~3–5K tokens. Also has `estimate_buckets`
(5-bucket Atlan allocation) and `cache_key` (FNV-1a for prompt caching
static prefix).

## Concurrency

- Reads (`Get`, `List`, `Status`, `Recall`) — concurrent-safe via `*sql.DB` directly.
- Writes (`Save`, `Update`) — serialized via `s.mu` mutex on Store.
- Migrations — applied once during `Open()` before any user code runs.

The MCP server receives one JSON-RPC request at a time per process
(stdio framing is line-based), so the mutex contention is essentially
zero. The `go test -race` step in CI exercises the concurrent paths.

## Tests

```
internal/llm    — 8 tests (httptest server, JSON parsing, code-fence stripping)
internal/mem    — 30 tests (CRUD + migrations + idempotency + list filters)
internal/research — backends + router (cached to avoid CI flakiness)
internal/safety — URL sanitization + SSRF guard
```

Coverage: `go tool cover -func=coverage.out`.

## Build & run

```bash
go build -o dark-research-mcp.exe ./cmd/dark-research-mcp
```

The binary speaks MCP over stdio. Configure in opencode.jsonc:

```json
{
  "mcp": {
    "dark-research-mcp": {
      "type": "stdio",
      "command": "C:/Users/Nico/Documents/dark-research-mcp/dark-research-mcp.exe",
      "env": {
        "DARK_DB": "C:/Users/Nico/AppData/Local/dark-agents/dark.db",
        "SDD_LLM_API_KEY": "{env:MINIMAX_API_KEY}",
        "SDD_LLM_BASE_URL": "https://api.minimax.io/anthropic",
        "SDD_LLM_MODEL": "MiniMax-M3",
        "BRAVE_API_KEY": "{env:BRAVE_API_KEY}"
      }
    }
  }
}
```

## Why Go (not Rust)?

- Single ecosystem with the dark-agents-v2 fork (`internal/dark/...`)
- Tooling reuse (`_ "modernc.org/sqlite"` for cgo-free SQLite)
- Faster iteration: no compile times in the 30+ second range
- 17 MB binary, no musl/glibc linkage concerns

Trade-offs accepted:
- Larger binary size (Go 17 MB vs Rust 4 MB)
- Slower raw CPU (irrelevant at MCP request rates)