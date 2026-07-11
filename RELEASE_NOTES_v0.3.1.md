# dark-research-mcp v0.3.1

**VCR fixtures + live OSINT backend status monitoring.** 57 MCP tools, 156 tests passing (+56 vs v0.3.0).

## What changed

### For users of the binary

Nothing breaking. The `--version` flag is new:

```sh
$ dark-research-mcp -version
dark-research-mcp 0.3.1
```

The User-Agent sent to backend APIs now reports the actual version
instead of a hard-coded "0.2", so backend operators see a clean
`dark-research-mcp/0.3.1` in their logs.

### For contributors

Two new infrastructure pieces, both designed to make backend
regressions visible within seconds instead of in production.

**1. VCR fixtures (`internal/research/testutil/` + `fixtures/`)**

The router and parser layers were previously untested. They are
now covered by 47 new tests that record real backend responses to
disk and replay them verbatim. The test suite is hermetic; CI no
longer depends on the network for routing/parsing correctness.

To refresh a single backend's fixtures when its API changes:

```sh
RECORD_FIXTURES=1 go test -count=1 -run 'TestRouter_<backend>' ./internal/research/
git diff fixtures/<backend>/
```

**2. Live status probe (`scripts/osint-status.sh`)**

Replaces the old on-PR smoke job (which only tested 1 backend and
swallowed every signal) with a parallel probe of all 16 backends.
The result is rendered in the GitHub Actions step summary on every
PR, and a dedicated weekly workflow auto-commits a refreshed
`BACKEND_STATUS.md` to `main`. Outages become visible data, not
silent breakage.

The current snapshot shows 13 backends healthy, 1 auth-required
(URLhaus now requires a key), 2 timeout (crt.sh, GDELT — both
genuinely slow).

## Test counts (156 total)

| Package | Tests | Note |
|---|---:|---|
| `internal/llm` | 16 | client + cache |
| `internal/mem` | 38 | CRUD + migrations + lists + ssd |
| `internal/research` | 55 | 15 classifier + 26 parser unit + 14 VCR router |
| `internal/research/testutil` | 12 | RecordingTransport (record/replay/scrub) |
| `internal/safety` | 9 | URL validation, SSRF guard |
| `internal/tools` | 21 | catalog + artifact_download + consensus + e2e |
| `internal/vault` | 5 | cross-platform interface |

## Assets

| File | Size | SHA-256 |
|---|---:|---|
| `dark-research-mcp-windows-amd64.exe` | 12 MB | `0a4f72ee…f150e` |
| `dark-research-mcp-linux-amd64` | 12 MB | `172a8f2b…71ded3` |
| `dark-research-mcp-linux-arm64` | 11 MB | `4a81256f…2b2c62d` |
| `dark-research-mcp-darwin-amd64` | 12 MB | `42b08490…39c30` |
| `dark-research-mcp-darwin-arm64` | 11 MB | `3872a3d5…156663` |

See `SHA256SUMS.txt` for the full list.

## Install / upgrade

```sh
# Download the binary for your platform
gh release download v0.3.1 --repo Opita-Code/dark-research-mcp

# Verify against SHA256SUMS.txt
sha256sum -c SHA256SUMS.txt --ignore-missing

# Run
./dark-research-mcp-linux-amd64 -version
# dark-research-mcp 0.3.1
```

## What's next

- Tiered gating on critical-backend outages (currently advisory)
- External status page (GitHub Pages, no third-party dependency)
- `scripts/refresh-fixtures.sh` for one-shot fixture refresh
