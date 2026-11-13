# Changelog

All notable changes to dark-research-mcp are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased] — live-backend SOTA audit (2026-11-13)

Every research backend was probed against its **live** upstream and the
implementation was corrected where the shipped code no longer matched the
upstream contract. The previous parser tests asserted payloads that no
upstream actually returns (they were written from assumption, never
recorded), which is how the drift went unnoticed.

### Fixed — backends that never returned usable results

- **OpenAlex** — sent `?q=`, but the API only accepts `search=` for
  free-text work (`q` filters by id/doi). Every call returned HTTP 400.
- **arXiv** — sent `?q=`; the API expects `search_query=all:<terms>`.
  Every call returned HTTP 400 with an empty Atom feed.
- **npm registry** — sent `?q=`; the search endpoint requires `text=`
  and answered `{"code":"ERR_TEXT_MISSING"}`.
- **RIPE stat** — `data.records` is an array of arrays of **objects**
  (`{"key":...,"value":...}`), not arrays of strings. The parser always
  failed with "cannot unmarshal object into .data.records.0.0".
- **Wayback CDX** — `output=json` answers with a JSON **array whose first
  row is a column header**, not JSON-lines. The line-based parser
  extracted zero items from every real response. The bug was masked by a
  fixture whose body was the empty array `[]`.
- **GDELT DOC 2.0** — `format=json` answers with an **object**
  (`{"articles":[...]}`), not a bare array. The parser always failed with
  "cannot unmarshal array into Go value of type struct{...}". The
  rate limit was also raised from 500 ms to 5.5 s to match the upstream's
  documented "one request every 5 seconds" throttle.
- **Ahmia** — the search endpoint is protected by a **rolling token**:
  both the query-parameter name and the token value rotate every minute
  (derived from a server-side salt, so they cannot be computed). A
  request without a valid token is redirected to the homepage, which the
  old parser read as "zero results". The backend now scrapes the token
  from the homepage search form and issues the search with it, and the
  parser unwraps the `onion-redirect` link to the real `.onion` URL.
  The old regex also required the title anchor to be a direct child of
  `<li>`; it is nested inside an `<h4>`.
- **AlienVault OTX** — `/api/v1/search/limited` is no longer served
  (HTTP 404). The backend now uses the supported
  `/api/v1/indicators/{type}/{value}/general` endpoint and parses the
  indicator record (reputation, validation, malware families).
- **abuse.ch URLhaus** — the API now requires an **`Auth-Key` header**
  and the host lookup is an **HTTP POST** with a form-encoded body
  (`host=<value>`), not a GET with the host in the path. The field is
  `date_added`, not `dateadded`.
- **SearXNG** — the instance answers **HTML** unless `format=json` is
  requested explicitly. Public instances are additionally behind
  anti-bot walls that answer HTTP 200 with a challenge page; the parser
  now reports that explicitly instead of a confusing JSON error, and
  the instance is overridable via `SEARXNG_INSTANCES`.
- **Cloudflare DoH** — requires `Accept: application/dns-json`; with the
  router's generic Accept header the endpoint answered HTTP 400.
- **Semantic Scholar** — authenticates with the **`x-api-key`** header,
  not `Authorization: Bearer`. Switched to the bulk-search endpoint,
  which also exposes `publicationDate`, `year` and `citationCount`.

### Fixed — routing

- **Multi-backend merge no longer starves lower-confidence backends.**
  The merged list was sorted by confidence and then truncated to
  `MaxItems`, so a higher-confidence backend (arXiv 0.95) filled every
  slot and a lower-confidence one (OpenAlex 0.92) became invisible even
  though its request succeeded. The merge now **interleaves** the
  backends round-robin, so every contributing source is represented.
- **POST support and per-backend headers.** `Backend` gained `Body`,
  `Headers` and `AuthHeader`, and the router rebuilds the request per
  retry attempt (a request body is consumed on send). The auth header
  is now configurable per backend instead of hardcoding Bearer with a
  Brave special case.
- **Retry backoff is configurable.** New `Backend.RetryBackoffMs`
  (default 1000 ms). crt.sh, which answers 502 frequently under load,
  now backs off 3 s → 6 s → 12 s instead of 1 s → 2 s → 4 s.

### Added

- **New `news` backend: Hacker News (Algolia Search API).** Free, no
  auth, and it gives the news intent a working source when GDELT
  throttles. GDELT enforces ~1 request every 5 seconds and answers
  HTTP 429 on shared egress IPs, which made the news intent
  unusable on those networks; with the fallback, the intent still
  returns results and the GDELT failure is reported transparently in
  `errors`.
- **Backends can opt out of queries they cannot serve.** New
  `Backend.Skip` predicate, honoured by the router. The Wayback CDX
  API archives *URLs*, not topics, so a topical keyword query used to
  be padded with unrelated captures; the backend now only runs for
  URL- or domain-shaped queries.
- `scripts/record-fixtures.go` — refreshes the checked-in fixture
  corpus from the live APIs using the same transport the tests replay.
- `scripts/verify-live.go` — read-only health check: probes every
  backend against its live upstream and reports items-per-backend.
- `internal/research/router_contract_test.go` — asserts the URLhaus
  POST + `Auth-Key` contract and the `Skip` behaviour against a
  local httptest server.
- `internal/research/testutil`: fixture paths now normalize Ahmia's
  rotating token, encode spaces as `+` and strip `:`, which is
  reserved on NTFS and previously truncated the arXiv fixture to 0 bytes.
- `Router.AllowLoopback` — relaxes the SSRF guard for tests that
  point a backend at an httptest server. Production wiring leaves it
  false.

### Changed

- The parser tests now use payloads **recorded from the live APIs**
  rather than hand-written approximations, and each carries a comment
  pointing at the upstream contract it encodes.

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
