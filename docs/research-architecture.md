# dark-research architecture

Intent-based OSINT routing. Each search intent has a niche; the right
backend for the niche is used, with fallbacks.

## Design principles

1. **No monopoly**. Different intents → different backends. The LLM
   agent picks per query.
2. **Open-source first**. Primary backend per intent is open-source
   (no API key, no paywall). Fallbacks may be free-with-key (Brave,
   GitHub) or paid (rare, last resort).
3. **Fallback on failure**. If the primary returns 5xx, rate-limit, or
   times out, try the next. Record the chain so the agent knows what
   happened.
4. **Normalized output**. Every backend returns the same shape; the
   LLM doesn't have to learn 13 different schemas.
5. **Rate-limit aware**. Each backend has a minimum interval; the
   router sleeps if called too soon.

## Intents

| Intent | Niche |
|---|---|
| `web` | General web search (DuckDuckGo, Brave, SearXNG) |
| `academic` | Papers, preprints, citations (OpenAlex, arXiv, Semantic Scholar) |
| `code` | Source code, repos, packages (GitHub, crates.io, npm, SourceGraph) |
| `cve` | Vulnerability advisories (OSV.dev, NVD, GHSA) |
| `domain` | WHOIS / RDAP (IANA bootstrap, whois.iana.org) |
| `dns` | DNS records (Cloudflare/Google/Quad9 DoH) |
| `cert` | Certificate transparency (crt.sh, Censys) |
| `ip` | IP geolocation / ASN (ip-api.com, RIPE, ipinfo.io) |
| `threat` | Threat intel / IOCs (abuse.ch, AlienVault OTX, GreyNoise) |
| `email` | Email / username (HIBP, holehe, Sherlock) |
| `dark` | Dark web (Ahmia, Tor via SOCKS5) |
| `geo` | Geospatial (OSM Nominatim, Overpass) |
| `news` | News / events (GDELT, Wayback Machine) |

## Backends per intent

```go
var backends = map[Intent][]Backend{
    IntentWeb: {
        {Name: "duckduckgo", URL: "...", Free: true, OpenSource: true, Weight: 1},
        {Name: "searxng",    URL: "...", Free: true, OpenSource: true, Weight: 2},
        {Name: "brave",      URL: "...", Auth: "BRAVE_API_KEY", Weight: 3},
    },
    IntentAcademic: {
        {Name: "openalex",        URL: "...", Free: true, OpenSource: true, Weight: 1},
        {Name: "arxiv",           URL: "...", Free: true, OpenSource: true, Weight: 2},
        {Name: "semanticscholar", URL: "...", Auth: "S2_API_KEY", Weight: 3},
    },
    // ... etc
}
```

We try backends in `Weight` order. If a backend's `Auth` env var is
missing, it's skipped (no error). If a backend returns 5xx/timeout, the
next is tried.

## Router

`dark_research(query, intent?)` is the meta-tool. If `intent` is given,
use it. Otherwise classify via heuristic.

### Classifier (heuristic, no LLM cost)

| Signal | Intent |
|---|---|
| `CVE-YYYY-NNNN` | cve |
| `10.NNNN/...` (DOI) | academic |
| `arxiv.org` or `arXiv:NNNN.NNNNN` | academic |
| `@something` (looks like email) | email |
| `*.onion` | dark |
| IPv4 / IPv6 literal | ip |
| FQDN (e.g. `foo.com`) | domain |
| `github.com`, `gitlab.com`, `crates.io`, `npmjs.com` | code |
| Keywords: "news", "today", "latest", "yesterday" | news |
| Keywords: "where is", "coordinates", "lat/lon" | geo |
| Keywords: "CVE", "vulnerability", "exploit", "IOC" | cve |
| (default) | web |

The classifier is a pure function; tests cover each branch.

## Output shape

```json
{
  "intent": "cve",
  "query": "CVE-2024-1234",
  "backend_used": "osv.dev",
  "backends_tried": ["osv.dev"],
  "took_ms": 312,
  "errors": [],
  "results": [
    {
      "title": "GHSA-xxxx-yyyy-zzzz",
      "url": "https://osv.dev/vulnerability/GHSA-...",
      "snippet": "Cross-site scripting in foo bar baz...",
      "score": 1.0,
      "source": "osv.dev",
      "fetched_at": "2026-07-10T20:00:00Z",
      "raw": { /* backend-specific extras, optional */ }
    }
  ]
}
```

## What the LLM sees

Tools registered with the MCP server:

- `dark_research(query, intent?)` — router
- `dark_research_web(query, ...)`
- `dark_research_academic(query, ...)`
- `dark_research_code(query, ...)`
- `dark_research_cve(query)`
- `dark_research_domain(domain)`
- `dark_research_dns(domain, types?)`
- `dark_research_cert(domain)`
- `dark_research_ip(ip)`
- `dark_research_threat(query, source?)`
- `dark_research_email(email)` — breach lookup + username enum
- `dark_research_dark(query)`
- `dark_research_geo(query)`
- `dark_research_news(query)`
- `web_fetch(url, max_length?, raw?)` — unchanged
- `url_extract_components(url)` — unchanged
- `text_anonymize(text, entity_types?)` — unchanged

## Rate limiting

Each backend has `RateLimitMs` (min interval). The router keeps an
in-memory `map[string]time.Time` of last call. If called within the
interval, sleep until allowed.

v0.1: in-memory only (per process). v0.2: persistent in SQLite.

## Caching

v0.1: no cache. v0.2: SQLite cache keyed by `(query, backend)` with
TTL (configurable per backend).

## Threat model

Same as before — every fetch goes through `safety.ValidateURL`. Every
returned result is wrapped with `safety.WrapUntrusted` so the LLM sees
trust boundaries.

## v0.1 scope (this iteration)

- Foundation: `internal/research/` (intent, classifier, backends, router)
- 4 specialized tools: `dark_research_web`, `dark_research_academic`,
  `dark_research_cve`, `dark_research_ip`
- Meta-tool: `dark_research(query, intent?)`
- Heuristic classifier with tests
- Smoke test per backend

v0.2: domain, dns, cert, threat, email, dark, geo, news
v0.3: caching + persistent rate-limit
v0.4: dark-mem integration (remember findings)
v0.5: dark-sdd integration (validate grounding)