#!/usr/bin/env bash
# dark-research-mcp QA gates (dark-testing skill).
#
# Usage:
#   bash scripts/qa-gates.sh gate1     # pre-commit: fmt, vet, build, test-short
#   bash scripts/qa-gates.sh gate2     # pre-push:   gate1 + coverage + mutation:quick
#   bash scripts/qa-gates.sh mutation  # full mutation pass (slow)
#
# No Makefile/Taskfile in this repo (Windows box without make/task), so
# the gates live here.
#
# v1.1.0 migration (2026-08-07): mutation tooling moved from go-mutesting
# (RETIRED — no Go modules support; results were INVALID; mutates the real
# working tree) to gremlins v0.6.0 (native go/packages, temp-copy mutants,
# coverage-guided). `.go-mutesting.yml` + `scripts/mut-test-short.sh` are
# dead; config lives in `.gremlins.yaml` (keys viper-nested under `unleash.`).
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# Grep for go-mutesting binary so gate2 fails loudly if someone re-added it.
if ! command -v gremlins >/dev/null 2>&1; then
  echo "gremlins not found. Install: go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0"
  exit 2
fi

gate1() {
  echo "== gate1: gofmt =="
  local fmt_out
  fmt_out="$(gofmt -l .)"
  if [ -n "$fmt_out" ]; then
    echo "gofmt: unformatted files:"; echo "$fmt_out"; return 1
  fi
  echo "== gate1: go vet =="
  go vet ./... || return 1
  echo "== gate1: go build =="
  go build ./... || return 1
  echo "== gate1: go test -short =="
  go test -short -count=1 ./... || return 1
  echo "gate1 OK"
}

# Packages whose unit suite is fast AND green (fixtures present).
# research/... is excluded: TestRouter_ahmia + TestRouter_gdelt fail on
# this checkout (missing VCR fixtures), which would pollute the mutation
# score (every mutant looks killed). mem/... is excluded from quick runs:
# 138s per mutant * N mutants is hours.
MUTATE_QUICK_PKGS=(./internal/safety/... ./internal/server/... ./internal/llm/... ./internal/vault/... ./internal/mods/...)

mutation_quick() {
  echo "== gate2: gremlins (quick) =="
  gremlins unleash --workers 4 --threshold-efficacy 60 --threshold-mcover 60 "${MUTATE_QUICK_PKGS[@]}"
}

gate2() {
  gate1 || return 1
  echo "== gate2: coverage =="
  go test -short -count=1 -coverprofile=coverage.out -covermode=count ./internal/... || return 1
  go tool cover -func=coverage.out | tail -1
  mutation_quick || return 1
  echo "gate2 OK"
}

mutation_full() {
  echo "== mutation: full internal/... pass =="
  echo "NOTE: research/... has 2 fixture failures; mem/... is ~138s/run. Expect slow."
  gremlins unleash --workers 4 ./internal/...
}

case "${1:-gate1}" in
  gate1) gate1 ;;
  gate2) gate2 ;;
  mutation) mutation_full ;;
  mutation-quick) mutation_quick ;;
  *) echo "usage: $0 [gate1|gate2|mutation|mutation-quick]"; exit 2 ;;
esac
