---
layout: default
title: OSINT backend status
---

# OSINT backend status

_Refreshed automatically by the osint-status workflow._
_Source of truth: [BACKEND_STATUS.md](https://github.com/Opita-Code/dark-research-mcp/blob/main/BACKEND_STATUS.md)._


Last checked: `2026-07-11T19:26:54Z` UTC

| Backend | Status | HTTP | Latency | Endpoint |
|---------|:------:|:----:|--------:|----------|
| arXiv | ✓ | 200 | 164ms | `http://export.arxiv.org/api/query?search_query=all:test&max_results=1` |
| OSV (CVE) | ✓ | 200 | 636ms | `https://api.osv.dev/v1/vulns/CVE-2024-3094` |
| crates.io | ✓ | 200 | 976ms | `https://crates.io/api/v1/crates/tokio` |
| npm | ✓ | 200 | 418ms | `https://registry.npmjs.com/-/v1/search?text=react&size=1` |
| OpenAlex | ✓ | 200 | 1111ms | `https://api.openalex.org/works?search=go&per_page=1` |
| Cloudflare DoH | ✓ | 200 | 379ms | `https://cloudflare-dns.com/dns-query?name=example.com&type=A` |
| Google DoH | ✓ | 200 | 463ms | `https://dns.google/resolve?name=example.com&type=A` |
| crt.sh | ✗ | 502 | 1014ms | `https://crt.sh/?q=github.com&output=json` |
| RDAP | ✓ | 200 | 1293ms | `https://rdap.org/domain/github.com` |
| ip-api.com | ✓ | 200 | 300ms | `http://ip-api.com/json/8.8.8.8` |
| OSM Nominatim | ✓ | 200 | 329ms | `https://nominatim.openstreetmap.org/search?q=Tokyo&format=json&limit=1` |
| URLhaus | 🔑 | 401 | 817ms | `https://urlhaus-api.abuse.ch/v1/host/example.com` |
| Ahmia | ✓ | 200 | 803ms | `https://ahmia.fi/search/?q=test` |
| RIPE stat | ✓ | 200 | 867ms | `https://stat.ripe.net/data/whois/data.json?resource=8.8.8.8&type=inetnum` |
| Wayback CDX | ✓ | 200 | 3568ms | `https://web.archive.org/cdx/search/cdx?url=example.com&output=json&limit=1` |
| GDELT | ⏱ | 000 | 10084ms | `https://api.gdeltproject.org/api/v2/doc/doc?query=test&mode=ArtList&maxrecords=1&format=json` |

**Result**: 2 failing / 16 total.

Legend: ✓ ok | ↪ redirect | 🔑 auth required (expected) | ⏱ timeout | ✗ HTTP error

_Generated automatically; do not edit. 2 backend(s) need attention._
