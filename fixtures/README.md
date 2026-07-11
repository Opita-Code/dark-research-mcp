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
api.openalex.org/works__q-transformer+attention+mechanism.http
crt.sh/index__q-github.com_output-json.http
```

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
