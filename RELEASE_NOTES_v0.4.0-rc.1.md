# dark-research-mcp v0.4.0-rc.1

**Schema migration to v2 (constitutions + mods) and v3 (sdd audit columns).** Phase 0 of the constitution + mods architecture. No runtime behavior change for existing callers.

## What this is

This is a **pre-release** (`-rc.1`) that lands the persistence layer for the upcoming constitution system. The runtime is unchanged for every existing tool: 57 MCP tools, all 156+ tests pass, behavior identical to v0.3.1 for anyone who doesn't actively use the new tables.

What IS new is on disk:

- A v1 database is automatically upgraded to v3 on first Open (transactional, idempotent).
- Three new tables (`constitutions`, `mods`, `mod_loads`) are created and empty.
- Five new columns on `sdd_evaluations` are added; pre-existing rows default to NULL/`0` and remain valid.
- `inspect-schema` now lists the new tables and surfaces the `refused_attempts > 0` count.

Nothing in the agent loop, the system prompts, or the tool contracts changes. The full v1.0 architecture — constitution loader, mod loader, refusal interceptor — lands in subsequent `-rc` releases.

## Why this is a pre-release

The constitution architecture spans multiple phases. Phase 0 alone is backwards-compatible (the database migrates transparently) and ships as `rc.1` so we can validate the schema under real workloads before Phase 1 introduces the constitution loader, Phase 2 the mod loader, and Phase 3 the refusal interceptor that populates the new `sdd_evaluations` audit columns.

The version stamps to `0.4.0-rc.1` because the schema bumps a minor (added capabilities) and the `-rc` suffix signals that the broader system is still in development. The final `0.4.0` ships when the runtime uses the new schema.

## What changed

### For users of the binary

Nothing changes operationally. The first Open of an existing `dark.db` runs v2 + v3 migrations in their own transactions, records them in `schema_migrations`, and continues. Subsequent opens are no-ops.

To verify your DB was upgraded:

```sh
$ dark-research-mcp -version
dark-research-mcp 0.4.0-rc.1

$ inspect-schema
=== row counts ===
  ...

--- constitution system (v2) ---
  constitutions: 0
  mods: 0
  mod_loads: 0

--- sdd_evaluations (anti-refusal audit, v3) ---
  sdd_evaluations: 10
  ...where refused_attempts > 0: 0
```

### For contributors

**Three new tables (v2):**

```
constitutions   one row per (constitution_id, version) manifest
                UNIQUE(constitution_id, version) supports multiple
                versions of the same constitution over time
                source: builtin:light | builtin:dark | user:<path>
mods            one row per installed mod manifest
                mod_id is the immutable namespace/name handle
                risk_class + target_scope feed the future
                web-of-mods UI
mod_loads       one row per (mod, session) load event
                the join point that lets the agent answer
                "which constitution + which mods produced
                this verdict?"
```

**Five new columns on `sdd_evaluations` (v3):**

```sql
constitution_id      TEXT       -- e.g. "dark-research/light"
constitution_version TEXT       -- e.g. "1.0.0"
active_mods_json     TEXT       -- ["mod_id@version", ...]
refused_attempts     INTEGER    -- 0 = first try; >0 = retries needed
refusal_pattern      TEXT       -- regex that matched (NULL otherwise)
```

The `refused_attempts` column is the foundation the dark-matrix-analysis
skill will group by for refusal-rate analytics once the interceptor
(Fase 3) starts populating it.

### For database integrators

The `dark-agents-v2` fork's `internal/dark/mem/` package shares the same
SQLite file as `dark-research-mcp/internal/mem/` (different tables, no
conflict — the WAL mode and foreign_keys=1 DSN handle this). The
constitution system is fully namespaced under `constitutions`, `mods`,
and `mod_loads`; the audit columns live only on `sdd_evaluations`. No
existing `dark-agents-v2` table is touched.

## Test counts (162 total, +6 vs v0.3.1)

| Package | Tests | Note |
|---|---:|---|
| `internal/llm` | 16 | client + cache (unchanged) |
| `internal/mem` | 44 | +6: schema_version, table presence, index presence, v3 column presence, v3 round-trip, v1→v3 upgrade |
| `internal/research` | 55 | unchanged |
| `internal/research/testutil` | 12 | unchanged |
| `internal/safety` | 9 | unchanged |
| `internal/tools` | 21 | unchanged |
| `internal/vault` | 5 | unchanged |

## Assets

| File | Size | SHA-256 |
|---|---:|---|
| `dark-research-mcp-windows-amd64.exe` | 12 MB | `285628c6…c6c8` |
| `dark-research-mcp-linux-amd64` | 12 MB | `06b3b984…9160` |
| `dark-research-mcp-linux-arm64` | 11 MB | `1e73535f…bff9` |
| `dark-research-mcp-darwin-amd64` | 12 MB | `a86460d6…613f` |
| `dark-research-mcp-darwin-arm64` | 11 MB | `d24f96fe…d916` |

See `SHA256SUMS.txt` for the full list.

## Install / upgrade

```sh
# Download the binary for your platform
gh release download v0.4.0-rc.1 --repo Opita-Code/dark-research-mcp

# Verify against SHA256SUMS.txt
sha256sum -c SHA256SUMS.txt --ignore-missing

# Run; the first Open migrates the DB transparently
./dark-research-mcp-linux-amd64 -version
# dark-research-mcp 0.4.0-rc.1
```

## What's next

- **Phase 1** — `internal/constitution/` (TOML loader, precedence rules) and `internal/llm/prompts.go` (`BuildSystemPrompt` from constitution + mods); rewire the 6 `dark_ssd_*` judges in `ssd.go` to use it. Ships as `v0.4.0-rc.2` with `constitutions/light.toml` built-in and `constitutions/dark.toml` behind the `-tags allow_builtin_dark` build flag.
- **Phase 2** — `internal/mods/` data-only loader. Ships as `v0.4.0-rc.3` with 2 example mods.
- **Phase 3** — refusal interceptor in `internal/llm/` that populates the v3 audit columns. Ships as `v0.4.0`.
