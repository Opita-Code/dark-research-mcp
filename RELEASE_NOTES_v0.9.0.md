# dark-research-mcp v0.9.0

**Release date:** 2026-08-08
**Tag:** [v0.9.0](https://github.com/Opita-Code/dark-research-mcp/releases/tag/v0.9.0)
**Branch:** `main`
**Supersedes:** v0.8.1 (persistence-aware recall + LLM synthesis)

> **Open-source release + release infrastructure hardening.**
>
> v0.9.0 is the first release prepared with the dark-memory-mcp release
> pattern: single-source version (`internal/version` + ldflags injection),
> `scripts/inject-version.sh`, a `Makefile` with `release` / `drift-check`,
> and a Keep-a-Changelog `CHANGELOG.md`. It also ships the migration of
> the mutation-testing toolchain to gremlins v0.6.0 (go-mutesting retired)
> and the safety layer at **100% mutation efficacy**.

---

## What's in this release

### Release infrastructure (new)

- **`internal/version`** — single source of version truth. Resolution
  chain: `-ldflags` (canonical `make release`) → `debug.ReadBuildInfo()`
  (`go install`) → `"dev"` sentinel with `IsDev`. The old hardcoded
  `var version = "0.8.0"` (which drifted from the v0.8.1 tag) is gone.
- **`scripts/inject-version.sh`** — resolves `git describe --tags
  --always --dirty` into a `-ldflags` expression. `--raw`, `--json`,
  `--strict`, `DARK_VERSION` override.
- **`Makefile`** — `make build|release|drift-check|test|mutation|clean|
  version|version-json|tag`. `drift-check` gates: clean tree, HEAD at a
  tag, matching CHANGELOG entry, version tests, vet.
- **`CHANGELOG.md`** — Keep a Changelog format, SemVer.

### Mutation testing (toolchain migration)

- go-mutesting **retired** (no Go modules support — every score was
  invalid; mutates the real working tree). Migrated to **gremlins
  v0.6.0** (native `go/packages`, temp-copy mutants, coverage-guided).
- `internal/safety` mutation pass: **100% efficacy / 100% mutator
  coverage** (65 killed / 0 lived / 0 timed out / 0 not covered).
- `scripts/qa-gates.sh` migrated; `.gremlins.yaml` added (viper-nested
  `unleash.` keys — see config note in `docs/EQUIVALENT_MUTANTS.md`).

### Fixed

- `Defense.Stats()` panic on zero-value `Defense` (slice-bounds on `""`
  + nil `Limiter` deref). Guarded; pinned by
  `TestDefense_StatsZeroValueDoesNotPanic`.
- Mutation-test gaps closed: `TestInputValidator_MaxArgsBoundaryExact`,
  spread-window anomaly tests.
- gofmt debt: 67 files reformatted; `gofmt -l .` clean.

## Upgrade guide

From v0.8.x there is **no breaking wire change** — the MCP surface and
the cx.v3 `coexistence_group` contract are unchanged.

```bash
# Build with the canonical version injected
make release            # or: DARK_VERSION=0.9.0 make release
./bin/dark-research-mcp -version   # → dark-research-mcp 0.9.0
```

## What this release is NOT

- Not a wire-contract change (no tool surface or coexistence_group
  change vs v0.8.x).
- Not the OSINT content itself — research/... fixture-based tests
  (ahmia/gdelt) still need VCR fixtures on this checkout.

## License

MIT — see [LICENSE](LICENSE).
