# dark-research-mcp

OSINT research MCP server in Go. Intent-based routing: each search
intent (web, academic, code, cve, ip, ...) routes to its niche
backend, with automatic fallback. Part of the dark-agents umbrella.

## Why Go (not Rust)

The original research chose Rust + rmcp. We pivoted to Go because:

1. **Existing toolchain** — `dark-eval` and `dark-agents-v2` are Go.
2. **Code reuse** — `internal/dark/mem/`, `internal/dark/profiles/`,
   `internal/dark/audit/`, `internal/dark/router/` are all Go and
   directly importable via a `replace` directive.
3. **Single ecosystem** — one toolchain, one set of deps, one CI.
4. **MCP maturity** — `github.com/mark3labs/mcp-go` covers stdio +
   JSON-RPC + tools.

What stays from the research (language-agnostic):

- The full tool catalog
- The MCP / dark-mem / dark-sdd ADRs
- The threat model in `findings/07-anti-prompt-injection.md`

What changes:

- `rmcp` → `mark3labs/mcp-go`
- `reqwest` → `net/http`
- `ammonia` → custom HTML stripper (v0.1); `bluemonday` later
- `arti` (embedded Tor) → SOCKS5 proxy via `cfg.tor.socks5_url`

## Architecture

See `docs/research-architecture.md` for the design.

13 intents: `web`, `academic`, `code`, `cve`, `domain`, `dns`, `cert`,
`ip`, `threat`, `email`, `dark`, `geo`, `news`.

Each intent has a backend registry ordered by `Weight`. The primary
backend is open-source / free. Fallbacks may be free-with-key (Brave,
GitHub) or paid (rare, last resort).

Routing:
- `dark_research(query, intent?)` — meta-tool, auto-classifies
- `dark_research_<intent>(query)` — explicit, skips classification

Heuristic classifier detects intent from query shape (CVE IDs, DOIs,
.onion, IPs, domains, GitHub URLs, etc.). No LLM cost.

## Build

```sh
go build ./cmd/dark-research-mcp
```

## Run (standalone smoke test)

```sh
# Initialize
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}' | ./dark-research-mcp.exe
echo '{"jsonrpc":"2.0","method":"notifications/initialized"}'

# List tools
echo '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | ./dark-research-mcp.exe

# Route a query (classifier picks intent)
echo '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"dark_research","arguments":{"query":"CVE-2024-3094"}}}' | ./dark-research-mcp.exe
```

## Register with opencode

Already registered in `~/.config/opencode/opencode.jsonc`. The agent
`dark-agents` (in `~/.config/opencode/agent/`) sees these tools
alongside `web_fetch`, `url_extract_components`, `text_anonymize`.

## Status

- [x] Project bootstrap (go.mod, internal/{config,safety,server,tools})
- [x] v0.1 tools (web_search, web_fetch, url_extract_components, text_anonymize)
- [x] `go build` succeeds, `go test` clean
- [x] Intent-based router with heuristic classifier
- [x] All 13 intents exposed as MCP tools
- [x] `dark_research_multi` for parallel multi-intent queries with dedup + confidence sort
- [x] Per-result quality metadata: confidence, freshness_at, lang
- [x] OSV.dev for CVE lookup (real data, CVE-2024-3094, CVE-2023-44487)
- [x] OpenAlex for academic search
- [x] ip-api.com for IP geolocation
- [x] DuckDuckGo HTML for web
- [ ] dark-mem integration (persist research, dedup across runs, recall)
- [ ] dark-sdd integration (LLM-as-judge for grounding verification)
- [ ] Email username enumeration (holehe-style, no API key)
- [ ] Multi-source aggregation (consensus, disagreement surfacing)
- [ ] Cache layer (avoid re-fetching identical queries)