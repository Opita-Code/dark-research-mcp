# Harness Compatibility — dark-research-mcp v0.6.0+

dark-research-mcp speaks MCP-over-stdio, so it works with every AI
coding harness that supports the protocol. This page lists the 5
most popular harnesses as of July 2026: OpenCode, Claude Code,
Cursor, Aider, and Cline. Source: votes + research_items in
`dark.db` from `dark_mem_recall_research` queries against
`mcp integration claude code`, `mcp cursor`, etc.

## TL;DR

Every harness uses the same MCP stdio pattern: the harness spawns
the binary directly, JSON-RPC frames flow over stdin/stdout, no
extra wrapping script. The binary itself loads credentials from
the `dark-agents-v2/*` vault at startup if the harness didn't
pre-populate the env — see `feat(vault): LoadIntoEnv` and the
`internal/vault/vault.go` LoadIntoEnv doc comment for the policy.

```bash
# The single binary works for every harness. No PowerShell wrapper.
C:/Users/Nico/Documents/dark-research-mcp/dark-research-mcp.exe
```

The 57 MCP tools surface as `dark_research_*`, `dark_mem_*`,
`dark_ssd_*`, and friends under each harness's tool namespace.

## OpenCode (already documented, here for symmetry)

Add to `~/.config/opencode/opencode.jsonc`:

```jsonc
{
  "mcp": {
    "dark-research": {
      "type": "local",
      "command": [
        "C:/Users/Nico/Documents/dark-research-mcp/dark-research-mcp.exe"
      ],
      "enabled": true
    }
  }
}
```

Restart `opencode`. The 57 MCP tools appear under the
`dark_research_*`, `dark_mem_*`, `dark_ssd_*` namespaces.

## Claude Code

`claude mcp add` is the official CLI (see
[docs.claude.com/en/docs/claude-code/mcp](https://docs.claude.com/en/docs/claude-code/mcp)):

```bash
claude mcp add --transport stdio dark-research -- \
  C:/Users/Nico/Documents/dark-research-mcp/dark-research-mcp.exe
```

Project-scoped via `.mcp.json`:

```json
{
  "mcpServers": {
    "dark-research": {
      "type": "stdio",
      "command": "C:/Users/Nico/Documents/dark-research-mcp/dark-research-mcp.exe",
      "args": [],
      "env": {}
    }
  }
}
```

Verify with `claude mcp get dark-research` (Claude Code reads and
applies the entry after the workspace trust dialog accepts it).

## Cursor

Cursor > Settings > MCP > Add new global MCP server. Set:

- **Name**: `dark-research`
- **Type**: `command` (stdio)
- **Command**: `C:/Users/Nico/Documents/dark-research-mcp/dark-research-mcp.exe`
- **Args / Env**: empty (vault auto-loads keys)

Restart Cursor. Tools appear in agent mode.

## Aider

Aider gained MCP Code Mode support in 2026-04 — see the
"How I Cut Aider's Token Bill 80%" writeup linked in
`dark_mem_recall_research(query="Aider MCP Code Mode")`. Configure
via `--mcp-config`:

```yaml
# ~/.aider.mcp.yml
mcp_servers:
  dark-research:
    command: C:/Users/Nico/Documents/dark-research-mcp/dark-research-mcp.exe
    type: stdio
```

Then run:

```bash
aider --mcp-config ~/.aider.mcp.yml --model sonnet
```

## Cline (VS Code extension)

Cline > MCP Servers > "Add a custom MCP server". Paste:

```json
{
  "mcpServers": {
    "dark-research": {
      "command": "C:/Users/Nico/Documents/dark-research-mcp/dark-research-mcp.exe",
      "args": [],
      "env": {}
    }
  }
}
```

Save. The 57 tools appear in Cline's tool palette. Cline routes the
stdio streams through its own server-side process manager; no extra
config needed.

## Required environment variables

| Variable | When | Source |
|---|---|---|
| `DARK_DB` | always | default `%LOCALAPPDATA%\dark-agents\dark.db` |
| `SDD_LLM_API_KEY` | only for the 8 dark-ssd judges | vault auto-load OR env |
| `MINIMAX_API_KEY` | fallback for `SDD_LLM_API_KEY` | vault auto-load OR env |
| `BRAVE_API_KEY` | only for `web_search` | vault auto-load OR env |

Vault auto-load means: run `Use-DarkAgentSecrets` once in your shell,
or save the key once via `Save-DarkAgentSecret -Name MINIMAX_API_KEY
-Secret sk-...`. The MCP binary reads from the Windows Credential
Manager at startup via `internal/vault/vault_windows.go`.

On non-Windows platforms where the dark-agents vault has no backend
yet (see `vault_other.go`), the binary still boots — see "LLM-less
mode" below.

## LLM-less mode

Without an LLM key, **the binary still boots and serves every
tool call** — see `feat(tools): graceful degradation when no LLM
is configured`.

Of the 57 tools:

- **22 work full-strength** — the 13 OSINT backends
  (`dark_research_web`, `dark_research_cve`, `dark_research_ip`,
  etc.), the vibe-flow CRUD family
  (`dark_research_spec_create`, `dark_research_brand_register`,
  `dark_research_compliance_register`, `dark_research_artifact_log`,
  `dark_research_drift_log`), the read-only `dark_mem_*` family,
  and the standalone `web_search` / `web_fetch` / `url_extract_components`
  / `text_anonymize` tools.
- **8 return degraded verdicts** — the dark-ssd LLM-as-judge
  family: `dark_ssd_brand_match`, `dark_ssd_compliance_check`,
  `dark_ssd_drift_judge`, `dark_ssd_grounding_check`,
  `dark_ssd_pii_detect`, `dark_ssd_prompt_injection_scan`,
  `dark_ssd_consensus`, `dark_ssd_list_evaluations`.

The degraded verdicts have the same JSON shape as the regular
verdicts (downstream consumers don't need a special case) but
carry:

- `match: 0`, `compliant: false`, `verdict: "needs_human"`,
  `grounded: false`, `pii_found: false`, `injection_found: false`
- `issues: ["no_llm_configured"]` for the grading-style tools
- `reasoning: "dark-ssd: LLM not configured (SDD_LLM_API_KEY /
  MINIMAX_API_KEY unset); agent must judge"`
- `model: "no_llm_configured"` sentinel
- Persisted in `sdd_evaluations` with
  `refusal_pattern="no_llm_configured"` and `refused_attempts=1`
  so the audit trail is unmistakable

The agent receives a verdict-shaped response and can escalate to
its own LLM-as-judge reasoning instead of crashing on a tool error.

## Compatibility matrix

| Harness | stdio | vault auto-load | LLM-less mode | Verified by |
|---|---|---|---|---|
| OpenCode | yes | yes (via `plugin/shell.env`) | yes (graceful) | spec 134 commit D |
| Claude Code | yes | yes (env passthrough) | yes (graceful) | spec 134 commit D |
| Cursor | yes | yes (env passthrough) | yes (graceful) | spec 134 commit D |
| Aider | yes (MCP Code Mode) | yes | yes (graceful) | spec 134 commit D |
| Cline | yes | yes | yes (graceful) | spec 134 commit D |

## Adding a new harness

If the harness speaks MCP over stdio or streamable-http, the
binary works unchanged. The default stdio transport is correct for
~95% of desktop / TUI harnesses; HTTP-mode harnesses need a
reverse-proxy layer (out of scope for this doc, see the Model
Context Protocol spec on [transports](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports)).

If the harness is missing from this list, open a PR adding its
install snippet here. The order is by commit frequency in
`dark.db`, not by editorial preference.

---

*"No construimos software para que se vea bonito en una presentación. Lo construimos para que trabaje contigo todos los días."* — Opita Code