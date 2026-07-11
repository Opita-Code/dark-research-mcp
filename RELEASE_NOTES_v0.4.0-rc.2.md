# dark-research-mcp v0.4.0-rc.2

**Constitution loader + dark-mode system prompt pipeline.** Phase 1 of the constitution + mods architecture. Public release ships the light constitution only; the dark constitution is opt-in via a build tag.

## What this is

This is a pre-release that lands the runtime side of the constitution system: a TOML loader, a precedence-based resolver, a persistence layer, a layered system-prompt builder, and a rewire of every `dark_ssd_*` judge to use it.

**The headline guarantee:** when the light constitution (the default) is in effect, every existing call site is byte-equivalent to pre-Fase-1. The first run after upgrade passes the `TestBuildSystemPrompt_LightReturnsToolDirective` test, which asserts byte-equality between the new constitution-driven prompt and the old hardcoded string for every judge. If a future change breaks this, the test fails immediately.

**The headline unlock:** the dark constitution is now real, declarative, and shipped. Its content is in `internal/constitution/constitutions/dark.toml` and only reaches the binary if you build with `-tags allow_builtin_dark`. With that tag, `--constitution dark` produces a layered system prompt for every judge that begins with identity + authority + refusal taxonomy + scope + tone, with the per-judge directive appended at the end. The light path stays untouched.

## How to use

The default binary (no build tag) works exactly as before. No new flags, no new behavior, no new tools. The system prompt every judge sees is unchanged.

The dark binary is opt-in:

```sh
# Build your own binary with the dark constitution embedded
go build -tags allow_builtin_dark -o dark-research-mcp \
  ./cmd/dark-research-mcp

# Run with the dark constitution
./dark-research-mcp -constitution dark

# Or load a custom constitution from a file
./dark-research-mcp -constitution /etc/dark-research/my-constitution.toml

# Or from the user dir (default: ~/.dark-research/constitutions/<id>.toml)
./dark-research-mcp -constitution user/my-experimental

# Or via env var (no flag needed)
DARK_CONSTITUTION=dark ./dark-research-mcp

# Verify the active constitution at startup
./dark-research-mcp -constitution dark
# ... startup logs ...
# dark-research-mcp: constitution=dark-research/aggressive@1.0.0 source=builtin:dark
```

On a stock binary, asking for `dark` is a clear error — not a silent fallback to light:

```
constitution error: constitution: dark.toml is not embedded in this binary
(rebuild with -tags allow_builtin_dark, or supply a user file at user/dark)
```

The error is the security/distribution boundary: the public GitHub release's bytes do not contain the dark constitution. The `//go:embed` declaration for `dark.toml` lives in `loader_dark.go` (build-tag-gated); on a stock build that file is not compiled and the dark bytes are absent.

## What changed

### For users of the binary

Nothing — unless you build with `-tags allow_builtin_dark` or load a custom constitution file.

### For contributors

**New package `internal/constitution/`** (5 source files, 3 test files):

- `types.go` — `Constitution` struct (TOML schema) + `ConstitutionRow` (DB shape), `AuthorityTier` and `RefusalMode` enums, `AllowedLayer()` whitelist.
- `loader.go` — strict TOML decode via `pelletier/go-toml/v2` (already a transitive dep), SHA-256 of raw bytes, `validate()` enforces required fields and enum values, `Initialize()` lazily populates `Dark` from the build-tag-gated wire-up.
- `loader_dark.go` — `//go:build allow_builtin_dark` only. `//go:embed constitutions/dark.toml` and an `init()` that wires `loadDark`. Not present in stock builds.
- `resolve.go` — `SetActive` / `Active` (singleton), `Resolve(spec)` implements the precedence rules (flag alias > user alias > absolute path > builtin alias).
- `store.go` — `NewStore(sqlExec)` thin wrapper. `Save` is an upsert via `ON CONFLICT(constitution_id, version) DO UPDATE`; `Get`, `List`, `ListByID`, `MarkActivated`.

**`internal/llm/`** (2 new source files, 1 test file):

- `tool_directives.go` — `toolDirectives` map indexed by `dark_ssd_*` tool name. Holds the pre-Fase-1 hardcoded directive strings verbatim. `DirectiveFor` does the lookup; `IsLightMode` gates the build path.
- `prompts.go` — `BuildSystemPrompt(PromptContext)`. Light path returns `ctx.ToolDirective` verbatim — identical to pre-Fase-1. Non-light path renders the layered prompt declared by the constitution in declared order (identity → authority → refusal_policy → scope → operational_rules → tone_and_voice → mod_directives → tool_directive → constitution_footer). `ModDirective` is defined but unused; Fase 2 wires it.

**`internal/tools/ssd.go`** — 12 judge call sites (6 single-shot + 6 consensus) now go through `judgeSystemPrompt(toolName)`. Every `SaveSDDEvaluation` passes through `fillConstitutionFields` so the v3 audit columns (`constitution_id`, `constitution_version`) are populated. The hardcoded `system := "..."` literals are gone.

**`cmd/dark-research-mcp/main.go`** — `--constitution` flag, `DARK_CONSTITUTION` env var, `constitution.Initialize()` at startup. Fails loud if the spec is unresolvable.

**Built-in constitutions** (in `internal/constitution/constitutions/`):

- `light.toml` — `mode = "passthrough"`. The system-prompt path is byte-identical to pre-Fase-1.
- `dark.toml` — `mode = "never_refuse"`. Wraps every judge in identity / authority / refusal / scope / tone layers. The exact text is in the file; it is the policy the user is opting into when they run `--constitution dark` on a tagged build.

### For database integrators

The `constitutions` table (added in v0.4.0-rc.1) is now actively populated. Every resolution at startup that comes from a TOML file (`user/<id>`, absolute path) is persisted to the table along with its SHA-256. Built-in constitutions (light, dark on tagged builds) are not persisted — they are the binary's own definition. The audit trail answers "which constitution was in effect on Tuesday at 3pm?" via `sdd_evaluations.constitution_id + constitution_version` (populated by `fillConstitutionFields`).

## Test counts (162 total, +18 vs v0.4.0-rc.1)

| Package | Tests | Note |
|---|---:|---|
| `internal/llm` | 16 | +0 (existing); +1 new: `prompts_test.go` covers BuildSystemPrompt + DirectiveFor + AllowedLayer (subtests counted as 1 here) |
| `internal/mem` | 44 | unchanged |
| `internal/research` | 55 | unchanged |
| `internal/research/testutil` | 12 | unchanged |
| `internal/safety` | 9 | unchanged |
| `internal/tools` | 21 | unchanged |
| `internal/vault` | 5 | unchanged |
| `internal/constitution` | 30 | **new package**: loader (5), resolve (7), store (5), prompts/directives (13) |

## Assets

(SHA-256s in `SHA256SUMS.txt`; binaries are stock builds, dark is opt-in via build tag so no separate dark asset is published)

| File | Size | SHA-256 |
|---|---:|---|
| `dark-research-mcp-windows-amd64.exe` | 12 MB | (regenerated) |
| `dark-research-mcp-linux-amd64` | 12 MB | (regenerated) |
| `dark-research-mcp-linux-arm64` | 11 MB | (regenerated) |
| `dark-research-mcp-darwin-amd64` | 12 MB | (regenerated) |
| `dark-research-mcp-darwin-arm64` | 11 MB | (regenerated) |

## Install / upgrade

```sh
# Download
gh release download v0.4.0-rc.2 --repo Opita-Code/dark-research-mcp

# Verify
sha256sum -c SHA256SUMS.txt --ignore-missing

# Run (no new behavior for default callers)
./dark-research-mcp-linux-amd64 -version
# dark-research-mcp 0.4.0-rc.2
```

## What's next

- **Phase 2** — `internal/mods/` data-only mod loader. `mod.toml` discovery, `~/.dark-research/mods/<id>/` layout, prompt-fragment injection into the `mod_directives` layer. Ships as `v0.4.0-rc.3` with 2 example mods.
- **Phase 3** — refusal interceptor in `internal/llm/`. `DetectRefusal` regex + retry chain that populates `sdd_evaluations.refused_attempts` and `refusal_pattern`. Ships as `v0.4.0` (or `v0.4.0-rc.4` if Fase 3 needs iteration).
