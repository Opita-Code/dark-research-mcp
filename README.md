<div align="center">

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                                                                              │
│   ██████╗  ██████╗██████╗ ██████╗     ███╗   ███╗ ██████╗██████╗             │
│  ██╔═══██╗██╔════╝██╔══██╗██╔══██╗    ████╗ ████║██╔════╝██╔══██╗            │
│  ██║   ██║██║     ██║  ██║██████╔╝    ██╔████╔██║██║     ██████╔╝            │
│  ██║   ██║██║     ██║  ██║██╔══██╗    ██║╚██╔╝███║██║     ██╔═══╝             │
│  ╚██████╔╝╚██████╗██████╔╝██║  ██║    ██║ ╚═╝ ██║╚██████╗██║                 │
│   ╚═════╝  ╚═════╝╚═════╝ ╚═╝  ╚═╝    ╚═╝     ╚═╝ ╚═════╝╚═╝                 │
│                                                                              │
│                        Opita Code Dark Research MCP                          │
│                                                                              │
│        Research Backends • Threat Intelligence • LLM-as-Judge • MCP           │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

**El servidor MCP de OSINT, prompting y validación con IA — en español.**

[![MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go 1.25+](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![MCP tools](https://img.shields.io/badge/MCP-57%20tools-blueviolet)](ARCHITECTURE.md)
[![Tests](https://img.shields.io/badge/tests-156%20passing-brightgreen)](.github/workflows/go-test.yml)
[![Backends](https://img.shields.io/badge/backends-13%20clearnet%20OSINT-blue)](internal/research)

[¿Qué hace?](#qué-hace) · [Estado y deprecation](#estado) · [Quickstart](#quickstart) · [Arquitectura](#arquitectura) · [Papers](#papers-y-mentalidad)

</div>

---

## ⚠️ Estado y deprecation (importante)

**`dark-research-mcp` está en proceso de consolidation hacia [`dark-memory-mcp`](https://github.com/Opita-Code/dark-memory-mcp).** A partir de **v0.7.0** (2026-07-04), este servidor expone las 38 herramientas duplicadas como **deprecation shims** que delegan al namespace `RESEARCH` de `dark-memory-mcp`. Los backends OSINT y los jueces LLM siguen viviendo aquí — son código de investigación activa — pero la superficie MCP unificada es `dark-memory-mcp` v2.x.

| Si vienes por… | Vas a… |
|---|---|
| Backends OSINT (OSV.dev, OpenAlex, RIPE, crt.sh, abuse.ch, DuckDuckGo, GDELT, Wayback, Ahmia, HIBP, ip-api, GitHub, crates.io, npm) | Seguir usando `dark_research_*` aquí, o migrar a `dark_memory_research_*` (namespace RESEARCH unificado). |
| LLM-as-judge (brand_fit, compliance, drift, grounding, pii_detect, prompt_injection_scan, consensus) | Migrar a `dark_memory_judge(eval_type=…)` (namespace JUDGE unificado). |
| Vibe-flow CRUD (spec / artifact / drift / brand / compliance / publish) | Migrar a `dark_memory_vibe_*` (namespace VIBE unificado). |
| Un solo binario con la API consolidada | Usar [`dark-memory-mcp`](https://github.com/Opita-Code/dark-memory-mcp) v2.2.0+. |

Los nombres `dark_*` en este repo siguen funcionando (shims emiten `X-Deprecation` headers y `dark_research_*` legacy names desde
`internal/tools/deprecation.go`); pero la superficie canónica vive en `dark-memory-mcp`.

---

## ¿Qué hace?

`dark-research-mcp` entrega a tu agente IA **57 herramientas especializadas** agrupadas en tres oficios:

1. **Investigación (OSINT)** — 13 backends clearnet con fallback automático (intent router auto-clasifica la query). Ver [internal/research/router.go](internal/research/router.go).
2. **Vibe-flow CRUD** — `spec → artifact → drift → reconcile → publish` con brand & compliance como reference data.
3. **Dark-ssd (LLM-as-judge)** — 8 jueces: brand fit, compliance jurisdiccional, drift, grounding OSINT, **PII detection (GDPR/CCPA)**, **prompt-injection scan** (security gate), y **consensus** multi-sample para verdicts de alto riesgo.

Una sola base SQLite (`dark.db`) compartida con `dark-memory-mcp`. Una sola API. Un solo binario. **Sin magia: con código que puedes leer y modificar.**

> 🇨🇴 *Construido en Neiva, Huila, Colombia como parte del ecosistema [Opita Code](https://www.opitacode.com). Software práctico para investigación real, no para verse bonito en una presentación.*

---

## Para quién

| Si eres… | Te interesa porque… |
|---|---|
| Investigador | Persiste runs OSINT, evita re-fetching. Cross-link entre items y CVEs. |
| Prompt engineer | LLM-as-judge reproducible: cada verdict + confidence + reasoning se persiste. |
| Vibe-coder | El pipeline `spec → artifact → drift` cierra el loop. |
| Red-teamer | Cruza findings con research OSINT y audit trail de prompts. |
| Compliance officer | `pii_detect` + `compliance_check` con EU AI Act, FTC, CCPA. |

---

## Quickstart

```bash
# Clonar
git clone https://github.com/Opita-Code/dark-research-mcp.git
cd dark-research-mcp

# Build
go build -o bin/dark-research-mcp.exe ./cmd/dark-research-mcp

# Sanity
./bin/dark-research-mcp.exe --version
# dark-research-mcp v0.7.1
```

Configurar vault / API keys (ver `internal/vault/`):

```bash
export DARK_LLM_API_KEY=...
export DARK_HIBP_API_KEY=...          # opcional, solo para email breach lookup
export DARK_GITHUB_TOKEN=...           # opcional, GitHub code search rate boost
```

Wire en OpenCode (`~/.config/opencode/opencode.json`):

```jsonc
{
  "mcp": {
    "dark-research": {
      "type": "stdio",
      "command": "C:/Users/you/path/dark-research-mcp.exe"
    }
  }
}
```

---

## Arquitectura

Single source of truth: [`ARCHITECTURE.md`](ARCHITECTURE.md).

```
dark-research-mcp/
  cmd/dark-research-mcp/main.go    entry point
  internal/
    config/                        YAML / env / flag configuration
    llm/                           MiniMax-M3 client (Anthropic-compatible)
    mem/                           SQLite persistence + migrations
    research/                      13 OSINT backends + intent router
    tools/                         57 MCP tool adapters + deprecation shims
    vault/                         secret envelope + auto-load
    safety/                        prompt-injection + PII scrubbers
```

**Shared substrate:** este servidor lee/escribe las mismas tablas que
`dark-memory-mcp` cuando corre junto a él (`research_runs`,
`research_items`, `vibe_specs`, `vibe_artifacts`, etc.). El schema
version y la convivencia están coordinados vía `internal/mem/migrate.go`.

---

## Papers y mentalidad

- **OSINT intent routing**: el router clasifica la query por tokens (CVE IDs, DOIs, .onion, IPs, GitHub URLs) y elige el backend correcto. Sin LLM en el loop.
- **Deprecation como contrato**: cuando un nombre se mueve, su shim emite `X-Deprecation: dark-memory-mcp/v2` + un cuerpo `notes` con el canónico. Tools no se rompen; la migración es gradual.
- **Prompt-injection como gate**: cualquier texto que cruza el boundary OSINT pasa por `safety/scrub_injection_candidates` antes de tocar al agente.

---

## Tests + verificación

```bash
go test -count=1 ./internal/...
```

156 assertions across 9 packages. Ver
[`.github/workflows/go-test.yml`](.github/workflows/go-test.yml) para
la matrix completa y los criterios de merge.

---

## Releases

- **v0.7.1** (2026-07-18) — [release notes](RELEASE_NOTES_v0.7.1.md) — test-isolation fix (no production change)
- **v0.7.0** (2026-07-04) — deprecation shim on 38 duplicate tools → consolidate to `dark-memory-mcp`
- **v0.6.0** (2026-06) — multi-harness compatible + vault auto-load + graceful degradation

> **Nota:** la historia entre v0.5.0 y v0.7.0 está en commits
> individuales — `RELEASE_NOTES_v0.7.0.md` y `RELEASE_NOTES_v0.7.1.md`
> son los únicos files de release-notes bajo control. Los demas
> pasos están en `git log`.

---

## License

MIT. Ver [`LICENSE`](LICENSE).

---

## Contribuir

PRs bienvenidos. Cambios no triviales abren issue primero. El CI corre
`go test ./...` + lint + un smoke test del binario (ver
[`.github/workflows/`](.github/workflows/)). Ver
[`CONTRIBUTING.md`](CONTRIBUTING.md) para convenciones.

Repo coordinado con [`dark-memory-mcp`](https://github.com/Opita-Code/dark-memory-mcp)
como su par estable; cambios que crucen ambos repos se mencionan en
ambos changelogs.
