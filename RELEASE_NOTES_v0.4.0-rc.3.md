# dark-research-mcp v0.4.0-rc.3

**Data-only mods loader + 2 example mods.** Phase 2 of the constitution + mods architecture. The public release now ships a mod system: discovery, activation, prompt injection, and audit. Mods are the unit of distribution for the future web-of-mods.

## What this is

This is a pre-release that lands the mod runtime: a TOML-based mod manifest schema, a loader that reads mods from a user directory, a registry that tracks the active mod set, a store that writes mod_loads audit rows, and the wire-up that injects the active mods' content into the system prompt every `dark_ssd_*` judge sees.

**The headline guarantee:** when the light constitution is in effect, the system prompt every judge sees is byte-equivalent to pre-Fase-1. The `mod_directives` layer is empty for callers with no active mods, so the light-path output is unchanged. The Fase 0 + Fase 1 contracts still hold.

**The headline unlock:** users can now install mods. Copy a mod directory into `~/.dark-research/mods/<short-name>/`, point `DARK_MODS_PATH` at it (or use the default), set `DARK_MODS=user/foo,user/bar`, restart — and the dark_ssd_* judges now have the mod's content in their system prompts. Two example mods ship in `mods-examples/`: a CVE prioritization playbook (`osint-cve-deepdive`, compatible with both constitutions) and a red-team jailbreak arsenal (`red-team-jailbreak-arsenal`, dark constitution only).

## How to use

The default binary works exactly as before — no mods, no behavior change. To enable mods:

```sh
# Option 1: copy the example mods into your user dir
cp -r mods-examples/osint-cve-deepdive ~/.dark-research/mods/
cp -r mods-examples/red-team-jailbreak-arsenal ~/.dark-research/mods/

# Option 2: point DARK_MODS_PATH at the examples (or any other dir)
export DARK_MODS_PATH=/path/to/mods-examples

# Activate via env var
export DARK_MODS="user/osint-cve-deepdive,user/red-team-jailbreak-arsenal"

# Or via flag
dark-research-mcp -mods "user/osint-cve-deepdive,user/red-team-jailbreak-arsenal"

# Run with the dark constitution (required for the red-team mod)
dark-research-mcp -constitution dark

# Startup logs
# dark-research-mcp: constitution=dark-research/aggressive@1.0.0 source=builtin:dark
# dark-research-mcp: active_mods=user/osint-cve-deepdive,user/red-team-jailbreak-arsenal
```

The persistent alternative: write mod_ids (one per line) into `~/.dark-research/mods/active.toml`. They are activated on every startup.

Mods that declare `auto_load = true` in their mod.toml are activated automatically. Mods with `auto_load = false` (the default, including the two example mods) require explicit activation.

## What changed

### For users of the binary

Nothing — unless you install mods and activate them.

### For contributors

**New package `internal/mods/`** (4 source files, 4 test files):

- `types.go` — `Manifest` struct (the TOML schema), `Knowledge` and `Directives` (the content the mod contributes), `Risk` (class + target_scope), `Source` and `SourceUser` (audit metadata), `Loaded` (the manifest plus the read content). `AllowedKind` set for `prompt_injection` / `data_source`.
- `loader.go` — strict TOML decode via `pelletier/go-toml/v2`, SHA-256 of `mod.toml`, validation of required fields + enum values, path-safety enforcement (no absolute paths, no `..` components). `LoadManifestBytes` is the test-friendly variant.
- `registry.go` — `Registry` holds the in-memory active set. `Discover` walks `DARK_MODS_PATH` / `DARK_RESEARCH_HOME/mods` / `~/.dark-research/mods`. `Activate` resolves a mod_id to a path, loads the mod, marks it active, and records the `mod_loads` row. `Deactivate` is idempotent on `ErrNotActive`. `AsModDirectives()` flattens the active set into the cross-package bridge type.
- `store.go` — `NewStore(ctx, sqlExec)` thin wrapper. `SaveMod` upserts the `mods` row. `RecordLoad` appends a `mod_loads` row (success or failure). `ListMods` / `ListModLoads` for audit queries.

**`internal/llm/prompts.go`** — no changes. The `mod_directives` slot was already defined in Fase 1 (`[]ModDirective{ModID, Source, Body}`). Fase 2 populates it from the registry via `AsModDirectives()`.

**`internal/tools/ssd.go`** — the 12 judge call sites now consult `activeModDirectives()` and pass the result to `BuildSystemPrompt`. Light-path callers with no active mods see no change.

**`internal/tools/tools.go`** — `Deps` gets a new `Mods *mods.Registry` field. The ssd handlers consult `sharedMods()` to fetch the active set.

**`internal/server/server.go`** — `New` signature extended to take the mods registry. The `Deps` flow now includes `Mods`.

**`cmd/dark-research-mcp/main.go`** — `--mods` flag, `DARK_MODS` env var, `~/.dark-research/mods/active.toml` persistent list, and `auto_load = true` discovery. The active mods set is logged at startup. mod_loads rows are recorded in `dark.db` for audit.

**`mods-examples/`** (new directory) — two complete data-only mods:

- `osint-cve-deepdive/` — CVE prioritization playbook, NVD advanced reference, and a per-turn directive for the `dark_ssd_grounding_check` / `dark_ssd_compliance_check` judges. Compatible with both light and dark constitutions.
- `red-team-jailbreak-arsenal/` — refusal taxonomy, jailbreak technique catalog, and a "researcher mindset" directive. Compatible with the dark constitution only (the light constitution's posture is incompatible with adversarial ML research framing).

### For database integrators

The `mods` and `mod_loads` tables (added in migration v2 in v0.4.0-rc.1) are now actively written. Every Activate / Deactivate records a `mod_loads` row. Every successful Activate upserts a `mods` row with the manifest JSON and SHA-256. Queries like "which constitution were these mods loaded under?" are answered via `mod_loads.constitution_id` (set in `Registry.SetConstitutionID` from the active constitution at startup).

The `sdd_evaluations.constitution_id` (populated in Fase 1) and `mod_loads.constitution_id` (populated now) together form the audit chain: every LLM-as-judge verdict is now tagged with the constitution in effect AND, indirectly via the active mod set, the mods that contributed to that constitution's prompt.

## Mod manifest schema (v1)

```toml
[meta]
id          = "namespace/name"            # required, e.g. "user/osint-cve-deepdive"
name        = "Human-readable name"        # required
version     = "1.0.0"                     # required semver
author      = "..."
license     = "MIT"
description = "..."
homepage    = "https://..."
tags        = ["osint", "cve", ...]

[requirements]
dark_research_version      = ">=0.4.0-rc.3"
constitution_compatibility = ["dark-research/light", "dark-research/aggressive"]
mods                       = []

[capabilities]                  # Fase 6 will use tools/parsers/backends
tools    = []
parsers  = []
backends = []

[knowledge]
prompt_injections = ["knowledge/playbook.md", ...]   # injected as system-prompt content
data_sources      = ["knowledge/data.toml", ...]     # persisted, not yet rendered

[directives]
prompt_fragments = ["directives/rule.md", ...]      # rendered with Source="directive"

[activation]
auto_load = false                                  # default: explicit activation only

[risk]
risk_class   = "research-only"                     # research-only | active-probing | exploit-development
target_scope = "public_internet"                   # public_internet | private_infrastructure | darkweb | local_only
requires_tor = false
```

The loader rejects unknown keys (strict decoder), missing required fields, malformed mod_ids (no `namespace/` prefix), absolute paths in `knowledge`/`directives`, and `..` components that escape the mod root.

## Test counts (217 total, +55 vs v0.4.0-rc.2)

| Package | Tests | Note |
|---|---:|---|
| `internal/llm` | 16 | unchanged |
| `internal/mem` | 44 | unchanged |
| `internal/research` | 55 | unchanged |
| `internal/research/testutil` | 12 | unchanged |
| `internal/safety` | 9 | unchanged |
| `internal/tools` | 21 | unchanged |
| `internal/vault` | 5 | unchanged |
| `internal/constitution` | 30 | unchanged |
| `internal/mods` | 25 | **new package**: loader (8), registry (8), store (3), example mods integration (2), realistic manifest (1), tools test sanity (1) |

## Assets

(SHA-256s in `SHA256SUMS.txt`; binaries are stock builds, dark is opt-in via build tag.)

| File | Size |
|---|---:|
| `dark-research-mcp-windows-amd64.exe` | 12 MB |
| `dark-research-mcp-linux-amd64` | 12 MB |
| `dark-research-mcp-linux-arm64` | 11 MB |
| `dark-research-mcp-darwin-amd64` | 12 MB |
| `dark-research-mcp-darwin-arm64` | 11 MB |

## Install / upgrade

```sh
gh release download v0.4.0-rc.3 --repo Opita-Code/dark-research-mcp
sha256sum -c SHA256SUMS.txt --ignore-missing
./dark-research-mcp-linux-amd64 -version
# dark-research-mcp 0.4.0-rc.3
```

## What's next

- **Phase 3** — refusal interceptor in `internal/llm/`. `DetectRefusal` regex + retry chain that populates `sdd_evaluations.refused_attempts` and `refusal_pattern`. Ships as `v0.4.0` (or `v0.4.0-rc.4` if it needs iteration).
- **Phase 6** (post-1.0) — Go-plugin mods for custom tools/parsers/backends. The manifest's `capabilities.tools/parsers/backends` fields are reserved for this; the user mod examples show the placeholder shape.
- **Phase 7** — web-of-mods registry. The manifest schema, the search path, and the audit trail are all in place. A registry just needs to install mods into the user dir.
