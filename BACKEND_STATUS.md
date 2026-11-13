# OSINT backend status

Last checked: `2026-11-13T10:45:00Z` UTC — **verified against the live upstreams**,
not against the recorded fixtures.

| Backend | Status | HTTP | Endpoint | Note |
|---------|:------:|:----:|----------|------|
| DuckDuckGo | ✓ | 200 | `https://html.duckduckgo.com/html/?q=…` | scraping; rate-limits repeat queries |
| SearXNG | ✗ | 200 | `https://searx.be/search?q=…&format=json` | public instances serve an anti-bot HTML wall; set `SEARXNG_INSTANCES` to a self-hosted instance |
| Brave | 🔑 | — | `https://api.search.brave.com/res/v1/web/search` | needs `BRAVE_API_KEY` |
| OpenAlex | ✓ | 200 | `https://api.openalex.org/works?search=…&per-page=20` | `search=`, not `q=` |
| arXiv | ✓ | 200 | `http://export.arxiv.org/api/query?search_query=…` | `search_query=all:<terms>` |
| Semantic Scholar | 🔑 | 200 | `https://api.semanticscholar.org/graph/v1/paper/search/bulk` | needs `S2_API_KEY` (header `x-api-key`) |
| crates.io | ✓ | 200 | `https://crates.io/api/v1/crates?q=…` | |
| npm | ✓ | 200 | `https://registry.npmjs.com/-/v1/search?text=…` | `text=`, not `q=` |
| GitHub | 🔑 | 200 | `https://api.github.com/search/repositories?q=…` | needs `GITHUB_TOKEN` |
| OSV.dev | ✓ | 200 | `https://api.osv.dev/v1/vulns/CVE-2024-3094` | |
| NVD | ◷ | 200 | `https://services.nvd.nist.gov/rest/json/cves/2.0?cveId=…` | 5 req / 30 s; times out under load |
| RDAP | ✓ | 200 | `https://rdap.org/domain/github.com` | |
| Cloudflare DoH | ✓ | 200 | `https://cloudflare-dns.com/dns-query?name=…&type=A` | needs `Accept: application/dns-json` |
| Google DoH | ✓ | 200 | `https://dns.google/resolve?name=…&type=A` | |
| crt.sh | ✓ | 200 | `https://crt.sh/?q=…&output=json` | frequent 502; retried with 3 s → 6 s → 12 s backoff |
| ip-api.com | ✓ | 200 | `http://ip-api.com/json/8.8.8.8` | 45 req/min |
| RIPE stat | ✓ | 200 | `https://stat.ripe.net/data/whois/data.json?resource=…` | |
| URLhaus | 🔑 | 401 | `https://urlhaus-api.abuse.ch/v1/host/` | **POST** + `Auth-Key` header; needs `URLHAUS_AUTH_KEY` |
| AlienVault OTX | 🔑 | 404 | `https://otx.alienvault.com/api/v1/indicators/{type}/{value}/general` | needs `OTX_API_KEY`; `/search/limited` is gone |
| HIBP | 🔑 | — | `https://haveibeenpwned.com/api/v3/breachedaccount/…` | needs `HIBP_API_KEY` |
| Ahmia | ✓ | 200 | `https://ahmia.fi/search/?q=…&<token>=<token>` | rolling token, scraped from the homepage form |
| OSM Nominatim | ✓ | 200 | `https://nominatim.openstreetmap.org/search?q=…&format=json` | |
| GDELT | ◷ | 429 | `https://api.gdeltproject.org/api/v2/doc/doc?query=…&format=json` | 1 req / 5 s; shared egress IPs get throttled |
| Hacker News | ✓ | 200 | `https://hn.algolia.com/api/v1/search?query=…` | free, no auth; the news-intent fallback |
| Wayback CDX | ✓ | 200 | `https://web.archive.org/cdx/search/cdx?url=…&output=json` | URL queries only |

**Result**: 23 backends verified working / 25 probed (2 require operator setup:
SearXNG needs a self-hosted instance, and the key-gated backends need their
env var). NVD and GDELT are reachable but throttle aggressively.

Legend: ✓ ok | 🔑 auth required | ◷ rate limited / flaky | ✗ upstream unavailable

_Regenerate with `./scripts/verify-live.go` (read-only) or
`./scripts/osint-status.sh` (HTTP-level probe)._
