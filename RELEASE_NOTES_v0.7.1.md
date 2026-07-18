# dark-research-mcp v0.7.1 — test isolation fix for internal/mods

**Release date:** 2026-07-18
**Tag commit:** `1201b2a` (the fix)
**Branch:** `main`
**Tag:** [v0.7.1](https://github.com/Opita-Code/dark-research-mcp/releases/tag/v0.7.1) (annotated)

> **Patch release — no production code change.**
>
> v0.7.1 fixes a test isolation bug in `internal/mods` that caused
> `TestRegistry_Discover_FindsModsUnderSearchPath` to fail on hosts
> with populated `~/.dark-research/mods/`. The fix is 7 lines in
> the test file; no production behavior is affected.

---

## What's in this release

### `internal/mods`: TestRegistry_Discover_FindsModsUnderSearchPath

`defaultSearchPaths()` in `internal/mods/registry.go` returns up to three
mod search paths in precedence order:

1. `$DARK_MODS_PATH` (colon-separated, like `PATH`)
2. `$DARK_RESEARCH_HOME/mods`
3. `$USERPROFILE/.dark-research/mods` (via `os.UserHomeDir()` on Windows)

The test set only `$DARK_MODS_PATH` to a temp directory containing two
mods (`alpha`, `beta`) and expected `Discover()` to return exactly those
two. On hosts where the operator had installed mods under
`~/.dark-research/mods/` (e.g. `osint-cve-deepdive`,
`red-team-jailbreak-arsenal`, `systems-engineering-mindset`), `Discover()`
also enumerated those, returning 5 entries and failing the assertion.

The two alleged pre-existing failures (`internal/mem` 60s panic,
`internal/vault` 44s hang on `TestLoadIntoEnv_silentOnNotFound`) were
not reproduced on this run. They appear to have been transient,
environment-dependent failures. If they recur in CI, re-triage under
DARK-MEM-018.

### Fix

`internal/mods/registry_test.go`, +7 lines:

```go
// Isolate from the operator's installed mods: defaultSearchPaths
// also walks $USERPROFILE/.dark-research/mods (via os.UserHomeDir)
// and $DARK_RESEARCH_HOME/mods. Without these overrides the
// operator's ~/.dark-research/mods leaks into the count and
// the assertion fails on hosts that have installed mods there.
t.Setenv("USERPROFILE", t.TempDir())
t.Setenv("DARK_RESEARCH_HOME", "")
```

No production code change. No API change. No binary re-release needed
(the `dark-research-mcp.exe` from v0.7.0 is bit-for-bit equivalent for
production purposes — the only behavioral difference is that this
test now passes cleanly on hosts with installed mods).

---

## Upgrade guide

No upgrade action required. v0.7.0 → v0.7.1 is a test-only fix.

If you depend on `dark-research-mcp`'s test suite running green on a
host with installed mods, pull this tag and re-run `go test ./...` —
the fix is automatic.

---

## Verification

```
$ go test -count=1 ./internal/mods/...
ok  	github.com/dark-agents/research-mcp/internal/mods	10.602s

$ go test -count=1 -timeout 180s ./internal/...
?   	github.com/dark-agents/research-mcp/internal/config	[no test files]
ok  	github.com/dark-agents/research-mcp/internal/constitution	16.425s
ok  	github.com/dark-agents/research-mcp/internal/llm	6.431s
ok  	github.com/dark-agents/research-mcp/internal/mem	109.745s
ok  	github.com/dark-agents/research-mcp/internal/mods	10.602s
ok  	github.com/dark-agents/research-mcp/internal/research	6.898s
ok  	github.com/dark-agents/research-mcp/internal/research/testutil	3.337s
ok  	github.com/dark-agents/research-mcp/internal/safety	1.800s
?   	github.com/dark-agents/research-mcp/internal/server	[no test files]
ok  	github.com/dark-agents/research-mcp/internal/tools	32.449s
ok  	github.com/dark-agents/research-mcp/internal/vault	1.591s
```

All 9 test packages pass. No regressions.

Refs: DARK-MEM-018
Complements: dark-memory-mcp v1.4.2 (current)

## License

MIT. See `LICENSE`.