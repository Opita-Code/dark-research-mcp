# fixtures/

Recorded HTTP responses for the VCR-style tests in
`internal/research/router_fixtures_test.go`. The `RecordingTransport`
in `internal/research/testutil/fixture_transport.go` replays these
files verbatim during tests, so the suite is hermetic and CI does
not depend on any external service.

## Layout

Files are keyed by the originating host and path, with the query
string appended as a suffix. Examples:

```
api.osv.dev/v1/vulns/CVE-2024-3094.http
api.openalex.org/works__search-transformer+attention+mechanism_per-page-20.http
crt.sh/index__q-github.com_output-json.http
```

The suffix mirrors the request URL exactly, so it also documents
which upstream contract each fixture encodes — e.g. `search=` for
OpenAlex, `text=` for npm, `search_query=` for arXiv.

Two parts of the suffix are normalised, because they would otherwise
change on every recording and make the fixture unreachable on the
next replay:

- **Ahmia's rolling token.** Ahmia's search endpoint requires a
  token whose parameter name *and* value rotate every minute. Both
  are rewritten to `TOKEN=TOKEN`, so the fixture path is stable.
- **Reserved characters.** `:` is reserved on NTFS and would truncate
  the written filename (arXiv's `search_query=all:…`); spaces are
  encoded as `+`.

Each file is a small HTTP envelope:

```
HTTP/1.1 200 OK
Content-Type: application/json

{ ... actual response body ... }
```

## When to refresh

A fixture should be refreshed when a backend changes its response
format. Symptoms: `internal/research/parser_test.go` keeps passing
(unit-level), but the corresponding `TestRouter_*` in
`router_fixtures_test.go` starts failing in CI.

To refresh locally:

```sh
# 1. Re-record. This overwrites fixtures in-place.
cd /path/to/dark-research-mcp
RECORD_FIXTURES=1 go test -count=1 -run 'TestRouter_' ./internal/research/

# 2. Inspect the diff. The change should match the upstream
#    backend's documented schema. If the diff is huge, the
#    backend probably did a breaking change and we need a
#    parser update, not just a fixture refresh.
git diff fixtures/

# 3. Commit and push.
git add fixtures/
git commit -m "test(fixtures): refresh <backend> response after schema change"
```

To refresh every backend from the live APIs at once (including the
ones without a dedicated `TestRouter_*`), use the recorder, which
drives the router itself:

```sh
go run ./scripts/record-fixtures.go
```

and to check that every backend is reachable *right now*:

```sh
go run ./scripts/verify-live.go
```

## A fixture must not be empty

An empty (or "no match") fixture is worse than no fixture at all:
every parser returns zero items on one, so a broken parser still
"passes". That is exactly how the Wayback CDX parser shipped broken
while its test was green — the fixture body was `[]`.

If a backend genuinely has no data for a query, pick a different
query, or delete the fixture so the test fails loudly.

## What is and is not in this directory

- **Yes**: HTTP status, headers, body for the 16 backends probed
  by the test suite.
- **No**: secrets, API keys, PII. The fixtures are public test
  data; nothing here should be private.

If you accidentally record a response that includes an `Authorization`
header or a session cookie, the `HeaderScrub` list in
`router_fixtures_test.go` should have stripped it. If you spot
something that should not be there, delete the file, scrub the
recording config, and re-record.
