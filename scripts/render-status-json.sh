#!/usr/bin/env bash
# scripts/render-status-json.sh
#
# Reads BACKEND_STATUS.md (the canonical report from osint-status.sh)
# and emits a shields.io endpoint JSON describing the overall health.
# The osint-status workflow commits this file as status.json at the
# repo root, and the README points at it via:
#
#   https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/Opita-Code/dark-research-mcp/main/status.json
#
# shields.io endpoint schema (schemaVersion 1):
#
#   {
#     "schemaVersion": 1,
#     "label": "backends",
#     "message": "13/16 ok",
#     "color": "green"
#   }
#
# Color policy:
#   green  - all backends ok
#   yellow - 1..2 backends with non-fatal issues (auth, slow)
#   red    - 3+ backends failing, OR >50% failing
#
# Usage:
#   ./scripts/render-status-json.sh < input.md > status.json
#   ./scripts/render-status-json.sh --in BACKEND_STATUS.md --out status.json

set -uo pipefail

IN="BACKEND_STATUS.md"
OUT="status.json"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --in)  IN="$2"; shift ;;
    --out) OUT="$2"; shift ;;
    *)     echo "unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

if [[ ! -f "$IN" ]]; then
  echo "render-status-json: input not found: $IN" >&2
  exit 1
fi

# Parse the markdown table for backends + status.
# Each row in the table is pipe-delimited. With awk -F'|' the fields
# are: $1="" (before first pipe), $2=" Backend ", $3=" Status ", ...
# We skip the header and divider rows by matching the literal
# "Backend" header and the "-----" divider.
#
# Status glyphs (set by osint-status.sh):
#   ✓ ok     🔑 auth_required   ↪ redirect   ⏱ timeout   ✗ fail
# We count ok/redirect/auth_required as healthy, timeout/fail as
# unhealthy. The shields.io badge color is derived from those counts.

# Match the status glyph at $3, after trimming whitespace.
parse_count() {
  local glyph="$1"
  LC_ALL=C.UTF-8 awk -F'|' -v g="$glyph" '
    /^\| / && $2 !~ /Backend/ && $2 !~ /^[\-[:space:]]*$/ {
      s = $3
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", s)
      if (s == g) c++
    }
    END { print c+0 }
  ' "$IN"
}

ok_count=$(parse_count '✓')
auth_count=$(parse_count '🔑')
redirect_count=$(parse_count '↪')
timeout_count=$(parse_count '⏱')
fail_count=$(parse_count '✗')

total=$((ok_count + auth_count + redirect_count + timeout_count + fail_count))
healthy=$((ok_count + auth_count + redirect_count))
unhealthy=$((timeout_count + fail_count))

# Determine color.
color="green"
message="${healthy}/${total} ok"
if [[ "$unhealthy" -ge 3 ]]; then
  color="red"
  message="${unhealthy}/${total} failing"
elif [[ "$unhealthy" -ge 1 ]]; then
  color="yellow"
  message="${healthy}/${total} ok (${unhealthy} issues)"
fi

# Pull the timestamp from the markdown header.
timestamp=$(awk -F'`' '/^Last checked:/ { gsub(/^[^`]*`/, ""); gsub(/`.*$/, ""); print; exit }' "$IN")
[[ -z "$timestamp" ]] && timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# Emit JSON. We use printf with hard-coded keys to avoid pulling in jq.
cat > "$OUT" <<EOF
{
  "schemaVersion": 1,
  "label": "backends",
  "message": "${message}",
  "color": "${color}",
  "namedLogo": "darkreader",
  "cacheSeconds": 3600
}
EOF

# Append a second field set for a richer alt-label badge in the README,
# separated by a fenced block we can grep.
cat >> "$OUT" <<EOF

<!--
  Raw counts (for tooling that wants them):
  ok=${ok_count} auth=${auth_count} redirect=${redirect_count} timeout=${timeout_count} fail=${fail_count} total=${total} last_checked=${timestamp}
-->
EOF

echo "render-status-json: wrote $OUT (ok=$ok_count auth=$auth_count redirect=$redirect_count timeout=$timeout_count fail=$fail_count total=$total color=$color)" >&2
