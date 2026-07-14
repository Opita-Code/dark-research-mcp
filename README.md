<div align="center">

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                                                                              │
│   ██████╗  ██████╗██████╗ ██████╗     ███╗   ███╗ ██████╗██████╗             │
│  ██╔═══██╗██╔════╝██╔══██╗██╔══██╗    ████╗ ████║██╔════╝██╔══██╗            │
│  ██║   ██║██║     ██║  ██║██████╔╝    ██╔████╔██║██║     ██████╔╝            │
│  ██║   ██║██║     ██║  ██║██╔══██╗    ██║╚██╔╝██║██║     ██╔═══╝             │
│  ╚██████╔╝╚██████╗██████╔╝██║  ██║    ██║ ╚═╝ ██║╚██████╗██║                 │
│   ╚═════╝  ╚═════╝╚═════╝ ╚═╝  ╚═╝    ╚═╝     ╚═╝ ╚═════╝╚═╝                 │
│                                                                              │
│                       Opita Code Dark Research MCP                           │
│                                                                              │
│          Research • Threat Intelligence • Automation • MCP Server            │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

**El servidor MCP que une investigación, prompting y validación con IA — en español.**

[![MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go 1.25+](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![MCP tools](https://img.shields.io/badge/MCP-57%20tools-blueviolet)](ARCHITECTURE.md)
[![Tests](https://img.shields.io/badge/tests-156%20passing-brightgreen)](.github/workflows/go-test.yml)
[![Backends](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/Opita-Code/dark-research-mcp/main/status.json)](BACKEND_STATUS.md)
[![CI](https://github.com/Opita-Code/dark-research-mcp/workflows/Go%20test/badge.svg)](.github/workflows/go-test.yml)

[¿Qué hace?](#qué-hace) · [¿Para quién?](#para-quién) · [Quickstart](#quickstart) · [Arquitectura](#arquitectura) · [Papers](#papers-y-mentalidad)

</div>

---

## ¿Qué hace?

**dark-research-mcp** es un servidor MCP escrito en Go que entrega a tu agente IA **57 herramientas especializadas** agrupadas en tres oficios:

1. **🔍 Investigación (OSINT)** — 15 herramientas que enrutan consultas a backends nicho (OSV.dev, OpenAlex, RIPE, crt.sh, abuse.ch, DuckDuckGo, GDELT, Wayback, Ahmia, HIBP, ip-api, GitHub, crates.io, npm) con fallback automático.
2. **🌊 Vibe-flow** — 22 herramientas para gestionar el ciclo completo de producción asistida por IA: spec (create/update/delete/render) → artifact (log/update/delete) → drift → reconcile → publish, con brand y compliance como reference data.
3. **⚖️ Dark-ssd (LLM-as-judge)** — 8 jueces LLM: brand fit, compliance jurisdiccional, drift spec-vs-artifact, grounding de claims OSINT, **PII detection (GDPR/CCPA)**, **prompt-injection scan** (security gate antes de pasar texto no confiable al agente), y **consensus** (multi-sample judging para verdicts de alto riesgo).

Una sola base SQLite (`dark.db`) compartida con `dark-eval`. Una sola API. Un solo binario (~17 MB). **Sin magia: con código que puedes leer y modificar.**

> 🇨🇴 *Construido en Colombia como parte del ecosistema [Opita Code](https://opitacode.com). Software práctico para investigación real, no para verse bonito en una presentación.*

---

## Para quién

| Si eres… | Te interesa porque… |
|---|---|
| 🔬 **Investigador** | Persiste runs OSINT, recuerda hallazgos, evita re-fetching. Cross-link entre items y CVEs/ataques/papers. |
| ✍️ **Prompt engineer** | El LLM-as-judge te da un panel reproducible: brand_match, compliance_check, grounding_check, **pii_detect**, **prompt_injection_scan** — cada uno con verdict + confidence + reasoning persistido. |
| 🌊 **Vibe-coder** | El pipeline `spec → artifact → drift → reconcile` cierra el loop. Para de regenerar el mismo bug cada vez. |
| 🛡️ **Red-teamer** | Mismo `dark.db` que `dark-eval`. Cruza findings de evaluación con research OSINT y audit trail de prompts. |
| 🏛️ **Compliance officer** | `dark_ssd_compliance_check` aplica el EU AI Act 2026-08-02, FTC, US-CA SB-1001. `dark_ssd_pii_detect` escanea GDPR Art. 4 / CCPA antes de publicar. Cada verdict se audita. |

---

## Quickstart

```bash
# 1. Clona y compila
git clone https://github.com/Opita-Code/dark-research-mcp.git
cd dark-research-mcp
go build -o dark-research-mcp ./cmd/dark-research-mcp

# 2. Configura (la API key va en tu vault local, no en variables de entorno planas)
export DARK_DB="$LOCALAPPDATA/dark-agents/dark.db"
export SDD_LLM_API_KEY="$(powershell -Command 'Import-Module dark-agents-vault.psm1; (Get-DarkAgentSecret MINIMAX_API_KEY)')"

# 3. Primera consulta — un CVE
./dark-research-mcp <<'EOF'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"hi","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"dark_research_cve","arguments":{"query":"CVE-2024-3094"}}}
EOF
```

Salida esperada (truncada):

```json
{
  "items": [{
    "title": "CVE-2024-3094",
    "url": "https://osv.dev/vulnerability/CVE-2024-3094",
    "snippet": "xz backdoor — malicious code in liblzma…",
    "source": "osv.dev",
    "confidence": 0.95
  }],
  "backend_used": "osv",
  "took_ms": 250
}
```

---

## El vibe-flow loop (la parte interesante)

El problema #1 sin resolver en 2026 AI-assisted development es el **spec-drift**: el agente genera algo, lo publica, y nunca reconcilia si lo que generó realmente cumple lo que el spec pedía.

**dark-research-mcp** cierra ese loop con persistencia + LLM-as-judge:

```
                    ┌──────────────────────────────────────────┐
                    │  1. Registrar brand guide                │
                    │     dark_research_brand_register(…)      │
                    │                                          │
                    │  2. Registrar jurisdicción               │
                    │     dark_research_compliance_register(…) │
                    │     (EU AI Act 2026-08-02: $51,744/viol) │
                    │                                          │
                    │  3. Crear spec                           │
                    │     dark_research_spec_create(…)         │
                    │                                          │
                    │  4. Generar el artifact                  │
                    │     (tu modelo / servicio preferido)     │
                    │                                          │
                    │  5. Loggear artifact                     │
                    │     dark_research_artifact_log(…)        │
                    │                                          │
                    │  6. LLM-as-judge: brand_fit              │
                    │     dark_ssd_brand_match(content, brand) │
                    │                                          │
                    │  7. LLM-as-judge: compliance             │
                    │     dark_ssd_compliance_check(content,   │
                    │                                 "EU")    │
                    │                                          │
                    │  8. LLM-as-judge: drift                  │
                    │     dark_ssd_drift_judge(artifact_id)    │
                    │                                          │
                    │  9. Loggear verdict                      │
                    │     dark_research_drift_log(verdict,     │
                    │          judge_reasoning)                │
                    │                                          │
                    │  10. Human gate si algo falló            │
                    └──────────────────────────────────────────┘
```

Cada LLM-as-judge persiste su verdict en `sdd_evaluations` con `prompt_version` + `model`. **Reproducible, auditable, mejorable con el tiempo** (calibration loop).

---

## Los 7 casos (C1..C7)

| Caso | Dominio | Riesgo compliance |
|---|---|---|
| **C1** code | funciones, scripts, refactors | bajo |
| **C2** text | emails, posts, docs | medio |
| **C3** image | hero shots, thumbnails | bajo |
| **C4** video | demos, ads | **alto** (EU AI Act) |
| **C5** audio | narración, podcasts | **alto** (EU AI Act) |
| **C6** multi-modal | Instagram ad: imagen + copy + CTA | **alto** (EU AI Act + FTC) |
| **C7** mixed | "ship this launch" | depende |

---

## Arquitectura

```
┌─────────────────────────────────────────────────────────────────┐
│  Tu agente (opencode, Claude Code, lo que sea)                  │
│                                                                 │
│  stdio MCP                                                      │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
        ┌────────────────────────────────────┐
        │   dark-research-mcp.exe            │
        │                                    │
        │   ┌──────────────────────────┐     │
        │   │  53 MCP tools            │     │
        │   │  ├ OSINT (15)            │     │
        │   │  ├ memory (6)            │     │
        │   │  ├ vibe-flow CRUD (15)   │     │
        │   │  ├ dark-ssd LLM-judge(5) │     │
        │   │  └ standalone (4)        │     │
        │   └──────────────────────────┘     │
        │                                    │
        │   ┌──────────────────────────┐     │
        │   │  internal/               │     │
        │   │  ├ llm (Anthropic-compat)│◄──── SDD_LLM_API_KEY
        │   │  ├ mem (SQLite + mig.)   │◄──── DARK_DB
        │   │  ├ research (16 backends)│     │
        │   │  │  └ testutil (VCR)     │◄──── fixtures/
        │   │  ├ safety (SSRF guard)   │     │
        │   │  └ vault (cross-platform)│     │
        │   └──────────────────────────┘     │
        └────────────────┬───────────────────┘
                         │
                         ▼
              ┌─────────────────────┐
              │  dark.db (SQLite)   │
              │                     │
              │  research_runs      │
              │  research_items     │
              │  research_links     │
              │  vibe_specs         │
              │  vibe_brands        │
              │  vibe_compliance    │
              │  vibe_artifacts     │
              │  vibe_drift_reports │
              │  sdd_evaluations    │
              │                     │
              │  + dark-eval tables │
              │    (findings, etc.) │
              └─────────────────────┘
```

Detalles en [`ARCHITECTURE.md`](ARCHITECTURE.md).

---

## Configuración

| Variable | Default | Propósito |
|---|---|---|
| `DARK_DB` | `%LOCALAPPDATA%\dark-agents\dark.db` | Path al SQLite |
| `SDD_LLM_API_KEY` | — | Auth LLM (fallback: `MINIMAX_API_KEY`) |
| `SDD_LLM_BASE_URL` | `https://api.minimax.io/anthropic` | Endpoint Anthropic-compatible |
| `SDD_LLM_MODEL` | `MiniMax-M3` | Model id |
| `DARK_SSD_CACHE_PATH` | `<DARK_DB_DIR>/llm-cache.json` | Cache LLM con TTL (vacío = disabled) |
| `BRAVE_API_KEY` | — | Opcional, fallback web search |
| `GITHUB_TOKEN` | — | Opcional, GitHub code search |
| `HIBP_API_KEY` | — | Opcional, email breach lookup |

Flags CLI: `--db`, `--cache`, `--cache-ttl`, `--log-level`, `--config`.

---

## Papers y mentalidad

Este proyecto se apoya en cuatro líneas de pensamiento explícitas:

1. **SSOT (Single Source of Truth)** — un solo `dark.db`, una sola API, una sola versión de la verdad. Si ves datos contradictorios en algún lado, el bug está en la consulta, no en el modelo.
2. **Closed-loop validation** — cada output creativo pasa por LLM-as-judge + persistence + reconcile. Sin esto, vibe-coding es un casino.
3. **Token economy (Atlan 2026)** — el cache LLM + los list endpoints filtrables son el primer paso para que `dark-ssd` no te queme el presupuesto en audit. Más detalles en el skill `vibe-flow`.
4. **Open-spec, open-source, open-data** — todo el código es legible. Las versiones de prompts se persisten en `sdd_evaluations.prompt_version` para que puedas reproducir cada verdict.

---

## Tests, lint, build

```bash
go vet ./...
go build ./...
go test -race ./...
```

156 tests pasando. CI corre vet/build/test (`-race`) en Go 1.25 + 1.26.

```
internal/llm                 16 tests   (8 client + 8 cache)
internal/mem                 38 tests   (CRUD + migrations + lists + ssd)
internal/research            55 tests   (15 classifier + 26 parser unit + 14 VCR router)
internal/research/testutil   12 tests   (RecordingTransport record/replay/scrub)
internal/safety               9 tests   (URL validation, SSRF guard)
internal/tools               21 tests   (catalog + artifact_download + consensus + e2e)
internal/vault                5 tests   (cross-platform interface)
```

### Backend status monitoring

Los 16 backends OSINT no son nuestro código — son servicios externos
que pueden cambiar de API o caerse. Por eso tenemos dos capas de
observabilidad, no solo una:

1. **VCR fixtures** (`internal/research/testutil/` + `fixtures/`) — los
   tests del parser y del router corren contra respuestas grabadas
   reales. CI no depende de la red, y un cambio de formato en un
   backend falla el test en segundos, no en producción.
2. **Live status probe** (`scripts/osint-status.sh`) — corre en cada PR
   (`osint-smoke` job) y semanalmente (`osint-status` workflow).
   Sondea los 16 backends en paralelo, escribe
   [`BACKEND_STATUS.md`](BACKEND_STATUS.md) con HTTP code + latencia
   por backend, y auto-commit'a los cambios. Outages aparecen visibles
   sin abrir un issue.

Para refrescar fixtures cuando un backend cambia su API:

```sh
RECORD_FIXTURES=1 go test -count=1 -run 'TestRouter_' ./internal/research/
git diff fixtures/   # revisar el diff antes de commit
```

---

## Status

- ✅ **v0.3.0** — dark_ssd_consensus (multi-sample judging) + dark_research_artifact_download (canonical fetch pattern) + tier-2 dark_mem_export_run/diff; 57 tools, 100 tests
- ✅ **v0.2.0** — CRUD completion (update/delete on 5 tables), spec_render, pii_detect + prompt_injection_scan (security gates); 53 tools, 80 tests
- ✅ **v0.1.0** — initial open-source release (45 tools, 72 tests, CI, MIT)
- 🆕 **unreleased** — VCR fixture transport (`internal/research/testutil/`) + 40 new tests (parser unit + VCR router) covering 13 backends with recorded responses; live status probe (`scripts/osint-status.sh`) + weekly `osint-status` workflow; **156 tests**
- 🚧 Add `go-keyring` impl for Linux/macOS vault
- 🚧 Spec diff library (structured change detection)
- 🚧 Cross-platform release artifacts in CI

Ver [`ARCHITECTURE.md`](ARCHITECTURE.md) para el roadmap técnico completo.

---

## Contribuir

PRs bienvenidos. Por favor:

1. `go test ./...` antes de pushear
2. Si añades un backend OSINT: implementa `research.Backend` interface, agrega a `DefaultRegistry()` con peso razonable
3. Si añades un LLM-as-judge: persiste siempre el `prompt_version` para reproducibilidad
4. Si añades una migración: append a `AllMigrations`, nunca edites una pasada

---

## Licencia

[MIT](LICENSE). Úsalo, modifícalo, distribúyelo. Si construyes algo bueno cuéntanos.

---

<div align="center">

Construido con 🇨🇴 desde Neiva, Huila, Colombia por [Opita Code](https://opitacode.com).

*"No construimos software para que se vea bonito en una presentación. Lo construimos para que trabaje contigo todos los días."*

[opitacode.com](https://opitacode.com) · [vibe.opitacode.com](https://vibe.opitacode.com) · [github.com/Opita-Code](https://github.com/Opita-Code)

</div>
