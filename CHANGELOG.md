# Changelog

All notable changes to dark-research-mcp are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [0.9.0] — 2026-08-08

### Release infrastructure (new — mirrors dark-memory-mcp pattern)

- **Single source of version truth.** New `internal/version` package
  resolves the version at runtime: `-ldflags` injection (canonical
  `make release` path) → `debug.ReadBuildInfo()` (for `go install`) →
  `"dev"` fallback with `IsDev` flag. The old hardcoded
  `var version = "0.8.0"` in `cmd/dark-research-mcp/main.go` — which
  drifted from the git tag (binary reported 0.8.0 while the repo was
  tagged v0.8.1) — is gone.
- **`scripts/inject-version.sh`** resolves the canonical version from
  `git describe --tags --always --dirty` and emits the `-ldflags`
  expression. Supports `--raw`, `--json`, `--strict`, `DARK_VERSION`
  override. Same contract as dark-memory's injector.
- **`Makefile`** with `build`, `release`, `drift-check`, `test`,
  `mutation`, `clean`, `version`, `version-json`, `tag` targets.
  `drift-check` enforces: clean tree, HEAD at a tag, matching
  CHANGELOG entry, version tests, vet.
- **User-Agent versioning.** `internal/research/router.go` now stamps
  the outbound `User-Agent` from `internal/version` (was a mutable
  package var set by main).

### Mutation testing (go-mutesting → gremlins v0.6.0)

- **go-mutesting is RETIRED** (per dark-testing skill v1.1.0): it does
  not support Go modules (`go/build` + deprecated loader), so every
  go-mutesting score was invalid; it also mutated the real working
  tree and leaked mutant residue on abort. Migrated to **gremlins
  v0.6.0** (native `go/packages`, temp-copy mutants, coverage-guided).
- Removed `.go-mutesting.yml`, `.go-mutesting.blacklist`,
  `scripts/mut-test-short.sh`; added `.gremlins.yaml` (note: keys are
  viper-nested under `unleash.` — a top-level `timeout-coefficient` is
  silently ignored).
- **`internal/safety` mutation score: 100% efficacy / 100% mutator
  coverage** (65 killed, 0 lived, 0 timed out, 0 not covered).
  Equivalents documented in `docs/EQUIVALENT_MUTANTS.md`.
- `scripts/qa-gates.sh` migrated to gremlins for `gate2` /
  `mutation-quick` / `mutation` targets.

### Fixed

- **`Defense.Stats()` panic on zero-value `Defense`.** The
  truncation `d.Canary.String()[:24]` sliced an empty string and
  `d.Limiter.Count()` dereferenced nil on a zero-value facade. Now
  guarded (`len(canary) > 24`, `Limiter != nil`); pinned by
  `TestDefense_StatsZeroValueDoesNotPanic`.
- **Mutation-test gaps in `internal/safety` closed with tests:**
  - `TestInputValidator_MaxArgsBoundaryExact` (kills the
    `len(args) > MaxArgsPerCall` → `>=` boundary mutant).
  - `TestAnomalyDetector_RefusalWindowKeepsSpreadRefusals`,
    `TestAnomalyDetector_RefusalWindowPrunesStraddling`,
    `TestAnomalyDetector_ToolRunawayWindowKeepsSpreadCalls` (kill the
    zero-window `60*time.Second` arithmetic mutants).
- **gofmt debt.** 67 files reformatted (doc-comment tabs, trailing
  newlines); `gofmt -l .` is now clean.

### Added

- `internal/version` package + unit tests (ldflags priority, buildinfo
  fallback, dev sentinel, memoization, JSON shape).
- `internal/safety/facade_test.go` — facade coverage (wiring, Stats,
  canary-leak thresholds).

### Notes

- Pre-existing failures unrelated to this release (documented in
  `scripts/qa-gates.sh`): `internal/research` TestRouter_ahmia +
  TestRouter_gdelt fail on this checkout (missing VCR fixtures);
  `internal/vault` PowerShell backend test is slow/flaky on first run.
  Core packages (safety, server, vault, llm, mods, version) pass.

## [0.8.1] — 2026-07-27

Persistence-aware recall + optional LLM synthesis. Closes the v0.8.0
deferrals. Full notes in `RELEASE_NOTES_v0.8.1.md`.

### Added
- Persistence-aware recall (`internal/research/cache.go`): router
  consults `research_runs` + `research_items` before fanning out to
  backends; within-TTL runs short-circuit.
- Optional LLM synthesis (`internal/research/synthesize.go`): one
  paragraph analyst summary for corroborating multi-backend findings
  (opt-in via `research.enable_synthesize=true`).
- Schema migration v4: `research_items.dedup_key`.

## [0.8.0] — 2026-07-27

cx.v3 conformance + multi-backend merge + dedup. Full notes in
`RELEASE_NOTES_v0.8.0.md`.

### Changed
- Stop-at-first-success replaced by multi-backend merge + cross-backend
  dedup in `Route()`.
- cx.v3 conformance per `BRIDGE_AND_COEXISTENCE.md` v2.0.0 §3.2.

## [0.7.1] — 2026-07-18

Stability + harness fixes. Full notes in `RELEASE_NOTES_v0.7.1.md`.

### Fixed
- `fix(llm): harness .env fallback so the LLM never gets stuck (#3)`.
- `fix(mods): isolate TestRegistry_Discover from operator's installed mods`.

## [0.7.0] — 2026-07-15

Deprecation grace window for the memory + vibe-flow + SSD layer. Full
notes in `RELEASE_NOTES_v0.7.0.md`.

### Changed
- 38 deprecation shims mirroring dark-memory's old names (`dark_mem_*`,
  `dark_research_spec_*`, `dark_ssd_*`); each returns
  `{deprecated: true, successor: "dark-memory-mcp", ...}`.
- Removed in dark-research v0.8.0.

## [0.5.0] — 2026-07-14

Bug-hunt 2026-07-14 critical + high fixes (BUG-001..006, 013).

## [0.4.1] — 2026-07-14

Post-rc stabilization for the vault-autoload path.

## [0.4.0-rc.5] — 2026-07-13

Release candidate series (rc.1..rc.5) for the v0.4.0 vault + LLM
harness layer. Full notes in `RELEASE_NOTES_v0.4.0-rc.*.md`.
