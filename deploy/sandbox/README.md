# Sandbox Deployment Guide

This document describes how to deploy `dark-research-mcp` in a sandboxed container for research use. It implements the deployment requirements in `docs/security/threat-model.md`.

## Why a sandbox

The defense layer in v0.4.1+ (L1, L2, L5, L6, L7, L8, L9) protects the binary from being weaponized via adversarial content. But a defense-in-depth posture also requires:

- **Network isolation** — even if a prompt injection makes the binary call an attacker URL, the network layer blocks it.
- **Filesystem isolation** — the binary can't write outside its workspace.
- **Resource limits** — a runaway loop can't exhaust the host's memory.
- **Privilege restriction** — the binary has only the Linux capabilities it needs (none, in our case).

## What's in this directory

- `Dockerfile` — multi-stage build. The runtime image is distroless (`gcr.io/distroless/static-debian12:nonroot`): no shell, no package manager, no utilities. The binary is the only thing in the image.
- `docker-compose.yml` — runtime config: read-only root, tmpfs for /tmp and /run, dropped capabilities, no-new-privileges, memory and CPU limits, custom network.
- `iptables-rules.sh` — egress restriction. Allows outbound only to the LLM provider (api.minimax.io by default). Run inside the container at startup.
- `workspace/` — empty directory. Mount your project files here.
- `secrets/llm-key.txt` — the API key file. NEVER commit this.

## Build

```bash
# Stock binary (light constitution default)
docker build -t dark-research-mcp:0.5.0-sandbox \
  --build-arg VERSION=0.5.0 \
  -f deploy/sandbox/Dockerfile .

# Research binary (dark constitution included)
docker build -t dark-research-mcp:0.5.0-research \
  --build-arg VERSION=0.5.0 \
  --build-arg TAGS=allow_builtin_dark \
  -f deploy/sandbox/Dockerfile .
```

The research image is **opt-in** — you must explicitly build with the tag. The default image is the safe one.

## Run

### With docker-compose (recommended)

```bash
cd deploy/sandbox

# Create the secrets file (NEVER commit it).
echo "sk-cp-YOUR-KEY-HERE" > secrets/llm-key.txt
chmod 600 secrets/llm-key.txt

# Start the sandboxed binary.
docker compose up
```

The binary is now running in a hardened container. The orchestrator (Pi, opencode, Claude Code) connects to it via the MCP stdio transport. The orchestrator runs OUTSIDE the container.

### Standalone

```bash
docker run --rm -i \
  --network dark-research-llm-only \
  --read-only \
  --tmpfs /tmp:size=100m,noexec,nosuid \
  --tmpfs /run:size=10m,noexec,nosuid \
  --security-opt no-new-privileges \
  --cap-drop ALL \
  --cap-add NET_ADMIN \
  --memory=512m \
  --cpus=1.0 \
  -v ./workspace:/workspace:rw \
  -e SDD_LLM_BASE_URL=https://api.minimax.io/anthropic \
  -e SDD_LLM_MODEL=MiniMax-M3 \
  -e SDD_LLM_API_KEY_FILE=/run/secrets/llm-key \
  -v /path/to/llm-key.txt:/run/secrets/llm-key:ro \
  --entrypoint /bin/sh \
  dark-research-mcp:0.5.0-sandbox \
  -c "iptables-rules.sh /usr/local/bin/dark-research-mcp --constitution dark"
```

## Network topology

```
+---------------------------------+
|  Orchestrator host              |
|  (Pi, opencode, Claude Code)    |
|  - has full network access       |
|  - runs the user's agent loop    |
+--------+------------------------+
         | MCP stdio (stdin/stdout)
         v
+---------------------------------+
|  dark-research-mcp container    |
|  - read-only root                |
|  - /workspace rw                 |
|  - /tmp tmpfs noexec             |
|  - all caps dropped              |
|  - NET_ADMIN only for iptables  |
|  - iptables: only api.minimax.io |
|  - memory 512m, cpu 1.0          |
+---------------------------------+
```

The orchestrator has full network access (it needs it to fetch web content, talk to other APIs, etc.). The dark-research-mcp container has ONLY api.minimax.io egress. A prompt injection that makes the binary try to POST to an attacker URL fails at the iptables level.

## What's NOT in the sandbox (intentionally)

- **The orchestrator.** It runs on the host with full privileges. The threat model documents this and recommends using a vetted orchestrator (Pi with the HITL extension, opencode, Claude Code).
- **The user's API key vault.** The key is mounted as a file in `/run/secrets/`. If the orchestrator host is compromised, the key is exposed. For high-security deployments, use a vault proxy that returns short-lived tokens.
- **The dark-research-mcp DB.** The DB is in `/workspace` (read-write). A compromise of the container could tamper with the audit log. For high-security deployments, mount a read-only audit mirror and replicate audit rows.

## Audit

The defense layer in v0.4.1+ logs every canary event, injection marker, and anomaly to stderr. In the sandbox, stderr flows to the container's logs. The operator should:

```bash
docker logs dark-research-mcp-sandbox 2>&1 | grep "safety:"
```

Look for:
- `safety: input rejected` — a hard reject (overlong, canary, too many args)
- `safety: canary leak attempt` — constitution extraction attempt
- `safety: CRITICAL canary leaked in tool=` — LLM-judge was compromised
- `safety: rate limit hit` — runaway orchestrator or extraction attempt
- `safety: anomaly detected: kind=...` — heuristic anomaly

Plus the SQLite `sdd_evaluations` table for the full audit log.

## License

Same as dark-research-mcp: MIT. The Dockerfile and docker-compose.yml are part of the project source.
