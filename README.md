# dark-research-mcp v0.9.0 — OSINT backing for the dark-agents ecosystem

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                                                                              │
│   ██████╗  ██████╗██████╗ ██████╗     ███╗   ███╗ ██████╗██████╗             │
│  ██╔═══██╗██╔════╝██╔══██╗██╔══██╗    ████╗ ████║██╔════╝██╔══██╗            │
│  ██║   ██╗██║     ██║  ██║██████╔╝    ██╔████╔██║██║     ██████╔╝            │
│  ██║   ██║██║     ██║  ██║██╔══██╗    ██║╚██╔╝██║██║     ██╔═══╝             │
│  ╚██████╔╝╚██████╗██████╔╝██║  ██║    ██║ ╚═╝ ██║╚██████╗██║                 │
│   ╚═════╝  ╚═════╝╚═════╝ ╚═╝  ╚═╝    ╚═╝     ╚═╝ ╚═════╝╚═╝                 │
│                                                                              │
│                       Opita Code Dark Research MCP v0.9.0                    │
│                                                                              │
│          OSINT backing • 13 intents • cx.v3 conformance • MIT                │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

**El servidor MCP de OSINT que trabaja detrás del gateway dark-memory.**

[![MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go 1.25+](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![CI](https://github.com/Opita-Code/dark-research-mcp/actions/workflows/go-test.yml/badge.svg)](https://github.com/Opita-Code/dark-research-mcp/actions/workflows/go-test.yml)
[![Version](https://img.shields.io/badge/version-0.9.0-blue)](CHANGELOG.md)
[![Coexistence](https://img.shields.io/badge/cx.v3-dark--agents%2Fresearch-blueviolet)](https://github.com/Opita-Code/dark-memory-mcp/blob/main/vibe-flow/main/BRIDGE_AND_COEXISTENCE.md)
[![Backing](https://img.shields.io/badge/policy_gateway-false-lightgrey)](https://github.com/Opita-Code/dark-memory-mcp/blob/main/vibe-flow/main/BRIDGE_AND_COEXISTENCE.md)

[¿Qué hace?](#qué-hace) · [Arquitectura](#arquitectura-cx.v3) · [Quickstart](#quickstart) · [Specs](#specs) · [Migración](#migración-desde-v0.7.x)

---

## ¿Qué hace?

**dark-research-mcp** es un servidor MCP escrito en Go que entrega a tu agente IA
**19 herramientas de OSINT** activas (1 router + 13 intents + 1 multi + 4 standalone),
más 38 shims `dark_mem_*` congelados que apuntan al gateway dark-memory.

**v0.8.0 = conformance cx.v3.** El cambio es metadata-only: el binario ahora declara
`coexistence_group=dark-agents/research` y `policy_gateway=false`, según
`BRIDGE_AND_COEXISTENCE.md` v2.0.0 §3.2. Las 19 herramientas activas funcionan idéntico
que en v0.7.x.

### 13 intents OSINT

| Intent | Backends | Caso de uso |
|---|---|---|
| `web` | DuckDuckGo HTML → SearXNG → Brave | blog posts, news, landing pages |
| `academic` | OpenAlex → arXiv → Semantic Scholar | papers peer-reviewed, preprints, DOIs |
| `code` | crates.io → npm → GitHub | library discovery, GitHub repos |
| `cve` | OSV.dev → NVD | vulnerabilidades con CVSS |
| `domain` | RDAP | WHOIS / handles / registrar |
| `dns` | Cloudflare DoH → Google DoH | A/AAAA/MX/TXT records |
| `cert` | crt.sh | cert transparency log |
| `ip` | ip-api.com → RIPE stat | geolocation + ASN |
| `threat` | URLhaus → AlienVault OTX | known-bad URLs/malware |
| `email` | HIBP / LeakCheck | breach lookups |
| `dark` | Ahmia.fi (.onion index) | dark web search |
| `geo` | OpenStreetMap Nominatim | geocoding |
| `news` | GDELT → Wayback CDX | news articles + archived pages |

Plus `dark_research_multi` (parallel fanout across intents), `web_search`,
`web_fetch`, `url_extract_components`, `text_anonymize`.

---

## Arquitectura cx.v3

```
┌────────────────────────────────────────────────────────────────────────┐
│                          opencode (harness)                            │
│                                                                        │
│   - Detecta policy_gateway=true en dark-memory                         │
│   - Detecta policy_gateway=false en dark-research                      │
│   - Enruta dark_* calls a través del gateway                           │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │
                    ┌───────────────┴───────────────┐
                    ▼                               ▼
       ┌────────────────────────┐      ┌────────────────────────┐
       │   dark-memory-mcp      │      │   dark-research-mcp    │
       │                        │      │                        │
       │ policy_gateway=true    │      │ policy_gateway=false   │
       │ coexistence_group=     │      │ coexistence_group=     │
       │   dark-agents/memory   │      │   dark-agents/research │
       │                        │      │                        │
       │   - vibe-loop          │      │   - 13 OSINT intents   │
       │   - agent_memory       │ ────▶│   - multi-backend      │
       │   - drift_judge        │      │     merge + dedup      │
       │   - session lifecycle  │      │   - persistence-aware  │
       │   - 34 canonical tools │      │     recall             │
       └────────────────────────┘      │   - 19 active tools    │
                                        └────────────────────────┘
```

**El gateway (dark-memory) dicta el vibe-loop, el contexto, y los patrones.** dark-research
sirve como **backing** que provee capacidades OSINT cuando el gateway las compone para
responder al LLM. Ver [BRIDGE_AND_COEXISTENCE.md v2](https://github.com/Opita-Code/dark-memory-mcp/blob/main/vibe-flow/main/BRIDGE_AND_COEXISTENCE.md) §3 para el contrato completo.

### Persistencia compartida

Ambos servidores escriben al mismo `dark.db` (SQLite), en tablas distintas:

| Tabla | Owner | Uso |
|---|---|---|
| `research_runs`, `research_items` | dark-research (escribe) | dark-memory (lee para cross-link) |
| `vibe_specs`, `vibe_brands`, `vibe_compliance`, `vibe_artifacts`, `vibe_drift_reports` | dark-memory | dark-research (no lee; legacy shims) |
| `sessions`, `write_audit`, `agent_memory` | dark-memory | dark-research (no escribe) |
| `schema_migrations` | dark-memory (autoridad) | dark-research (read-only) |

---

## Quickstart

```bash
# 1. Compila
go build -ldflags "-X github.com/dark-agents/research-mcp/internal/version.buildVersion=0.9.0" -o dark-research-mcp.exe ./cmd/dark-research-mcp

# 2. Configura el DB (compartido con dark-memory)
export DARK_DB="$LOCALAPPDATA/dark-agents/dark.db"

# 3. Primera consulta
./dark-research-mcp.exe <<'EOF'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"hi","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"dark_research_cve","arguments":{"query":"CVE-2024-3094"}}}
EOF
```

Salida esperada:
```json
{
  "items": [{
    "title": "CVE-2024-3094",
    "url": "https://osv.dev/vulnerability/CVE-2024-3094",
    "snippet": "xz backdoor — malicious code in liblzma…",
    "source": "osv",
    "confidence": 0.95
  }],
  "backend_used": "osv",
  "took_ms": 250
}
```

Compatible con OpenCode, Claude Code, Cursor, Aider, Cline — todos los harnesses MCP-nativos.

---

## Specs

- **Normativo**: [`BRIDGE_AND_COEXISTENCE.md`](https://github.com/Opita-Code/dark-memory-mcp/blob/main/vibe-flow/main/BRIDGE_AND_COEXISTENCE.md) v2.0.0 (dark-memory-mcp repo)
- **Drift report**: [`DRIFT_BURST.md`](https://github.com/Opita-Code/dark-memory-mcp/blob/main/vibe-flow/main/DRIFT_BURST.md)
- **Changelog**: [`CHANGELOG.md`](CHANGELOG.md) (Keep a Changelog, SemVer)
- **Release**: `make release` (inyecta el tag vía `scripts/inject-version.sh`); `make drift-check` valida el estado antes de cortar tag
- **Release notes**: [`RELEASE_NOTES_v0.8.1.md`](RELEASE_NOTES_v0.8.1.md), [`RELEASE_NOTES_v0.8.0.md`](RELEASE_NOTES_v0.8.0.md)
- **Histórico**: [`RELEASE_NOTES_v0.7.1.md`](RELEASE_NOTES_v0.7.1.md), [`RELEASE_NOTES_v0.7.0.md`](RELEASE_NOTES_v0.7.0.md)

---

## Migración desde v0.7.x

**Cambio breaking (metadata-only).** v0.8.0 cambia el `coexistence_group` declarado en el
`initialize` response:

| Versión | `coexistence_group` | `policy_gateway` |
|---|---|---|
| v0.7.x | (omitido, o `dark-agents/memory` legacy) | (omitido) |
| v0.8.0 | **`dark-agents/research`** | **`false`** |

**Si tu harness inspecciona `coexistence_group`**:
- Actualiza el valor esperado de `dark-agents/memory` → `dark-agents/research`.
- Si tu harness hardcoded el viejo valor, va a entrar en cx.v2 legacy fallback mode
  (BRIDGE §5.4 test 7) — degrada gracefully pero pierdes el gateway routing.

**Si tu harness NO inspecciona `coexistence_group`**: zero migration cost. Las 19
herramientas activas funcionan idéntico.

### Para actualizar

```bash
git pull
go build -ldflags "-X github.com/dark-agents/research-mcp/internal/version.buildVersion=0.9.0" -o dark-research-mcp.exe ./cmd/dark-research-mcp
# Windows: reemplaza el .exe y reinicia opencode (inode lock)
```

### Timeline

| Fecha | Estado |
|---|---|
| 2026-07-19 | BRIDGE v2 publicado; cx.v3 effective |
| **2026-07-27** | **dark-research v0.8.0 ships cx.v3 metadata** |
| 2026-08-XX | dark-memory v2.1.3+ ships `policy_gateway=true` en paralelo |
| 2026-09-30 | Último día cx.v1/v2 dual-support |
| 2026-10-01 | cx.v1/v2 deprecated; harness logs warning si ve legacy metadata |

---

## Estado del binario

| Asset | Valor |
|---|---|
| Tag | v0.8.0 |
| Commit | (pending — ver RELEASE_NOTES_v0.8.0.md) |
| Branch | `main` |
| Binary size | ~17 MB |
| Coexistence | `cx.v3` (research backing) |
| Spec compliance | BRIDGE v2.0.0 §3.2 |

## Licencia

MIT (Opita Code). Ver [`LICENSE`](LICENSE).
