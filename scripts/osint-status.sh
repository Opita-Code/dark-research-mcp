#!/usr/bin/env bash
# scripts/osint-status.sh
#
# Probes each registered OSINT backend with a lightweight GET request
# and emits a markdown status table. Used by .github/workflows/osint-status.yml
# on a weekly schedule, but also runnable locally for ad-hoc checks.
#
# Usage:
#   ./scripts/osint-status.sh                # markdown to stdout
#   ./scripts/osint-status.sh --json         # JSON to stdout
#   ./scripts/osint-status.sh --out FILE     # write markdown to FILE
#
# Exit codes:
#   0 = all checks passed (status 2xx or 3xx)
#   1 = one or more checks failed (status >= 400 or curl error)
#   2 = usage error

set -uo pipefail

MODE="md"
OUT=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --json) MODE="json" ;;
    --out)  OUT="$2"; shift ;;
    --help) sed -n '2,18p' "$0"; exit 0 ;;
    *)      echo "unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

# Each line: "name|url"
CHECKS=(
  "OSV (CVE)|https://api.osv.dev/v1/vulns/CVE-2024-3094"
  "OpenAlex|https://api.openalex.org/works?search=go&per_page=1"
  "arXiv|http://export.arxiv.org/api/query?search_query=all:test&max_results=1"
  "crates.io|https://crates.io/api/v1/crates/tokio"
  "npm|https://registry.npmjs.com/-/v1/search?text=react&size=1"
  "RDAP|https://rdap.org/domain/github.com"
  "Cloudflare DoH|https://cloudflare-dns.com/dns-query?name=example.com&type=A"
  "Google DoH|https://dns.google/resolve?name=example.com&type=A"
  "crt.sh|https://crt.sh/?q=github.com&output=json"
  "ip-api.com|http://ip-api.com/json/8.8.8.8"
  "RIPE stat|https://stat.ripe.net/data/whois/data.json?resource=8.8.8.8&type=inetnum"
  "URLhaus|https://urlhaus-api.abuse.ch/v1/host/example.com"
  "Ahmia|https://ahmia.fi/search/?q=test"
  "OSM Nominatim|https://nominatim.openstreetmap.org/search?q=Tokyo&format=json&limit=1"
  "GDELT|https://api.gdeltproject.org/api/v2/doc/doc?query=test&mode=ArtList&maxrecords=1&format=json"
  "Wayback CDX|https://web.archive.org/cdx/search/cdx?url=example.com&output=json&limit=1"
)

UA="dark-research-mcp-status/1.0 (+https://github.com/Opita-Code/dark-research-mcp)"
TIMEOUT=10
TIMESTAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# Temp files for parallel-ish results.
RESULTS=$(mktemp)
trap 'rm -f "$RESULTS"' EXIT

probe() {
  local name="$1" url="$2"
  local code latency start end
  local -a extra_headers=(-H "Accept: application/json, text/html;q=0.9")

  # Cloudflare DoH requires the JSON content-type specifically.
  if [[ "$url" == *"cloudflare-dns.com"* ]]; then
    extra_headers=(-H "Accept: application/dns-json")
  fi

  start=$(date +%s%3N 2>/dev/null || date +%s)
  # -L: follow redirects (Ahmia returns 302; Nominatim too).
  # -s: silent, we only want the code.
  # -o /dev/null: discard body (we read code separately).
  # -w: write code to stdout.
  code=$(curl -s -L -o /dev/null -w "%{http_code}" \
    --max-time "$TIMEOUT" \
    -A "$UA" \
    "${extra_headers[@]}" \
    "$url" 2>/dev/null) || code="000"
  end=$(date +%s%3N 2>/dev/null || date +%s)
  latency=$((end - start))

  # Classify the response.
  local status
  if [[ -z "$code" || "$code" == "000" ]]; then
    status="timeout"
  elif [[ "$code" =~ ^2 ]]; then
    status="ok"
  elif [[ "$code" =~ ^3 ]]; then
    status="redirect"
  elif [[ "$code" == "401" || "$code" == "403" ]]; then
    status="auth_required"
  else
    status="fail"
  fi

  # Write atomically (one line per probe; subshells append).
  printf '%s|%s|%s|%s|%s\n' "$name" "$url" "$code" "$latency" "$status" >> "$RESULTS"
}

# Probe in parallel. 16 checks at 5s each = 80s serial, ~10s parallel.
# Use a small concurrency cap to be polite to backends.
MAX_PARALLEL=4
pids=()
for entry in "${CHECKS[@]}"; do
  IFS='|' read -r name url <<< "$entry"
  probe "$name" "$url" &
  pids+=($!)
  # Throttle: wait when we hit the cap.
  if (( ${#pids[@]} >= MAX_PARALLEL )); then
    wait "${pids[0]}"
    pids=("${pids[@]:1}")
  fi
done
# Drain remaining.
wait

# Determine overall result. auth_required and redirect are
# non-failures (expected behavior, not outages).
fail_count=$(awk -F'|' '$5=="fail" || $5=="timeout"' "$RESULTS" | wc -l | tr -d ' ')
total=$(wc -l < "$RESULTS" | tr -d ' ')

emit_markdown() {
  cat <<EOF
# OSINT backend status

Last checked: \`${TIMESTAMP}\` UTC

| Backend | Status | HTTP | Latency | Endpoint |
|---------|:------:|:----:|--------:|----------|
EOF
  awk -F'|' '{
    if ($5 == "ok") icon = "✓"
    else if ($5 == "auth_required") icon = "🔑"
    else if ($5 == "redirect") icon = "↪"
    else if ($5 == "timeout") icon = "⏱"
    else icon = "✗"
    printf "| %s | %s | %s | %sms | `%s` |\n", $1, icon, $3, $4, $2
  }' "$RESULTS"

  echo ""
  echo "**Result**: ${fail_count} failing / ${total} total."
  echo ""
  echo "Legend: ✓ ok | ↪ redirect | 🔑 auth required (expected) | ⏱ timeout | ✗ HTTP error"
  echo ""
  if [[ "$fail_count" -eq 0 ]]; then
    echo "_Generated automatically; do not edit._"
  else
    echo "_Generated automatically; do not edit. ${fail_count} backend(s) need attention._"
  fi
}

emit_json() {
  printf '{\n'
  printf '  "timestamp": "%s",\n' "$TIMESTAMP"
  printf '  "total": %d,\n' "$total"
  printf '  "failing": %d,\n' "$fail_count"
  printf '  "checks": [\n'
  local first=1
  while IFS='|' read -r name url code latency status; do
    if [[ "$first" -eq 0 ]]; then printf ',\n'; fi
    first=0
    printf '    {"name": "%s", "url": "%s", "code": "%s", "latency_ms": %s, "status": "%s"}' \
      "$name" "$url" "$code" "$latency" "$status"
  done < "$RESULTS"
  printf '\n  ]\n'
  printf '}\n'
}

if [[ "$MODE" == "json" ]]; then
  out=$(emit_json)
else
  out=$(emit_markdown)
fi

if [[ -n "$OUT" ]]; then
  printf '%s\n' "$out" > "$OUT"
else
  printf '%s\n' "$out"
fi

if [[ "$fail_count" -gt 0 ]]; then
  exit 1
fi
exit 0
