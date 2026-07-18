# dark-research-mcp (clean install at C:\Users\Nico\dark-research-mcp)

Clean, single-binary MCP server for OSINT + vibe-flow CRUD + LLM-as-judge, wired
into OpenCode 1.18.1 with a custom primary agent `dark-research` and a read-only
OSINT subagent `scour`.

This directory is intentionally isolated from `C:\Users\Nico\Documents\dark-research-mcp\`,
which contains prototype artifacts (dark-recall, probe-daemon, vibe-studio-prototype,
embed-setup, dark-mem honeycomb, etc.) that were discarded. Those artifacts are
NOT referenced from any opencode config and are NOT loaded by this MCP.

## Layout

    C:\Users\Nico\dark-research-mcp\
    +-- dark-research-mcp.exe         # binary, v0.5.0 base + feat/vault-autoload + cherry-pick ffb6041
    +-- opencode-with-vault.ps1       # launcher: loads vault then execs opencode
    +-- oc.bat                        # shim: invoke as `oc` from any shell
    +-- README.md                     # this file
    +-- internal/                     # source (Go)
    +-- cmd/                          # command entry points (source)
    +-- ...

## Launching

Default (vault loaded, dark-research agent as default):
    oc
    # equivalent to: pwsh -File C:\Users\Nico\dark-research-mcp\opencode-with-vault.ps1

Use the OSINT subagent:
    oc --agent scour

Verify MCP connectivity inside an opencode session:
    > /mcp
    dark-research  C:/Users\Nico\dark-research-mcp/dark-research-mcp.exe   connected

## How it routes LLM calls (R9 fallback rule)

The MCP binary, when started with MINIMAX_API_KEY / SDD_LLM_API_KEY in env, calls
https://api.minimax.io/anthropic directly. When started WITHOUT these keys but with
DARK_SCRAPPER_URL set, it falls back to the dark-scrapper daemon on 127.0.0.1:8901.
On this clean install that daemon is NOT running, so all LLM-backed dark_ssd_*
calls would fail with "connection refused" if the vault is not loaded.

`oc.bat` and `opencode-with-vault.ps1` ensure the vault is loaded BEFORE opencode
spawns the MCP server, so secrets flow down via process inheritance. Never invoke
`opencode` directly without the vault wrapper if you want dark_ssd_* judges to work.

## Building from source

Requires Go 1.25+.

    git clone --branch feat/vault-autoload https://github.com/Opita-Code/dark-research-mcp.git .
    git cherry-pick ffb6041    # feat(llm): detect parent harness via env-var markers
    go build -o dark-research-mcp.exe ./cmd/dark-research-mcp

## Provenance

    v0.5.0 (tag)               <- upstream Opita-Code/dark-research-mcp
    feat/vault-autoload branch <- upstream pre-v0.6.0 candidate
    ffb6041 cherry-pick        <- upstream dark-agents/v0.6.0-fork (Nico local)

## License

MIT (Opita Code). See LICENSE in the upstream repo.