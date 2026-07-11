#!/usr/bin/env bash
# scripts/refresh-fixtures.sh
#
# Refresh VCR fixtures for a single OSINT backend. Useful when you
# already know which backend changed (e.g. you saw an alert from
# BACKEND_STATUS.md) and want to refresh just that one's recorded
# response, not the full suite.
#
# Usage:
#   ./scripts/refresh-fixtures.sh osv           # single backend
#   ./scripts/refresh-fixtures.sh all           # every TestRouter_*
#   ./scripts/refresh-fixtures.sh --dry-run osv # show what would run
#   ./scripts/refresh-fixtures.sh --list        # show all known backends
#
# Each test in internal/research/router_fixtures_test.go follows the
# naming TestRouter_<backend>, so we can pattern-match on the name
# without hardcoding the full list in two places. --list reflects the
# current set: grep "^func TestRouter_" router_fixtures_test.go.
#
# The script sets RECORD_FIXTURES=1 (which the testutil transport
# honors) and writes the new response to fixtures/<host>/<path>.http.
# It does NOT commit: review the diff in git before committing.

set -uo pipefail

PKG="./internal/research"
TEST_FILE="${PKG}/router_fixtures_test.go"

# Map of "logical name" → "TestRouter_<go-style-name>".
# Most tests use a lowercase, hyphen-free name; this map only needs
# entries where the test function name diverges from the natural form.
declare -A ALIAS=(
  [osv]="TestRouter_OSV"
  [openalex]="TestRouter_OpenAlex"
  [cratesio]="TestRouter_cratesio"
  [npm]="TestRouter_npm"
  [rdap]="TestRouter_RDAP"
  [doh]="TestRouter_DoH"
  [cloudflare-doh]="TestRouter_DoH"
  [google-doh]="TestRouter_DoH"
  [crtsh]="TestRouter_crtsh"
  [ipapi]="TestRouter_ipapi"
  [ip-api]="TestRouter_ipapi"
  [ripe]="TestRouter_ripe"
  [ahmia]="TestRouter_ahmia"
  [nominatim]="TestRouter_nominatim"
  [osm-nominatim]="TestRouter_nominatim"
  [gdelt]="TestRouter_gdelt"
  [wayback]="TestRouter_ripe"  # wayback has no dedicated router test; rip
)

DRY_RUN=0
LIST=0
TARGETS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    --list)    LIST=1 ;;
    -h|--help)
      sed -n '2,22p' "$0"
      exit 0
      ;;
    all)
      # Every TestRouter_* in the test file.
      mapfile -t TARGETS < <(grep -oE '^func (TestRouter_[A-Za-z0-9_]+)' "$TEST_FILE" | awk '{print $2}')
      ;;
    *)
      TARGETS+=("$1")
      ;;
  esac
  shift
done

if [[ "$LIST" -eq 1 ]]; then
  echo "Known router tests:"
  grep -oE '^func (TestRouter_[A-Za-z0-9_]+)' "$TEST_FILE" | awk '{print "  " $2}' | sort -u
  echo ""
  echo "Accepted short names:"
  echo "  osv openalex cratesio npm rdap doh crtsh ipapi ripe ahmia nominatim gdelt"
  exit 0
fi

if [[ ${#TARGETS[@]} -eq 0 ]]; then
  echo "refresh-fixtures: no targets given. Use --list to see options." >&2
  echo "refresh-fixtures: or pass a short name (e.g. 'osv') or 'all'." >&2
  exit 2
fi

# Resolve short names to test function names.
RESOLVED=()
for t in "${TARGETS[@]}"; do
  if [[ "$t" =~ ^TestRouter_ ]]; then
    RESOLVED+=("$t")
  elif [[ -n "${ALIAS[$t]:-}" ]]; then
    RESOLVED+=("${ALIAS[$t]}")
  else
    # Try the natural form: lowercase, underscores. If it doesn't
    # match a known test, fail loudly.
    candidate="TestRouter_$(echo "$t" | tr '[:upper:]' '[:lower:]' | tr - _)"
    if grep -q "^func $candidate" "$TEST_FILE"; then
      RESOLVED+=("$candidate")
    else
      echo "refresh-fixtures: unknown target '$t'. Try --list." >&2
      exit 2
    fi
  fi
done

# Build the go test -run regex. We use alternation: (TestA|TestB|...)
REGEX="^(${RESOLVED[0]}"
for t in "${RESOLVED[@]:1}"; do
  REGEX="${REGEX}|${t}"
done
REGEX="${REGEX})\$"

CMD=(env RECORD_FIXTURES=1 go test -count=1 -timeout=60s -run "$REGEX" "$PKG")

echo "refresh-fixtures: ${#RESOLVED[@]} target(s)"
printf '  - %s\n' "${RESOLVED[@]}"
echo ""
echo "refresh-fixtures: command:"
printf '  %q ' "${CMD[@]}"
echo ""
echo ""

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "(dry run; not executing)"
  exit 0
fi

# Run. We let go test's exit code propagate so CI / pre-commit
# hooks can catch refresh failures.
"${CMD[@]}"
status=$?

if [[ "$status" -eq 0 ]]; then
  echo ""
  echo "refresh-fixtures: done. Review the diff before committing:"
  echo "  git diff fixtures/"
  echo "  git add fixtures/"
  echo "  git commit -m 'test(fixtures): refresh ${RESOLVED[*]} response'"
fi

exit "$status"
