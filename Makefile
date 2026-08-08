# Makefile for dark-research-mcp.
#
# Every release build goes through `make release`, which calls
# scripts/inject-version.sh to resolve the canonical version from the
# git tag and inject it via `-ldflags "-X ...internal/version.buildVersion=<v>"`.
# This mirrors the dark-memory-mcp release pattern (single source of
# version truth; no hardcoded version strings in cmd/).
#
# On Windows hosts without bash, run scripts/inject-version.sh from
# git-bash, or set DARK_VERSION explicitly. The Makefile assumes a
# POSIX shell (git-bash, WSL, or Linux/macOS).

# Resolve the canonical -ldflags expression. Output is something like
# "-X github.com/dark-agents/research-mcp/internal/version.buildVersion=0.9.0"
# or "-X ...buildVersion=0.9.0-3-gabc1234" for commits past a tag.
ifeq ($(OS),Windows_NT)
    # git-bash on Windows: scripts/inject-version.sh is the canonical
    # path. The Makefile invokes it through bash explicitly.
    INJECT_VERSION := bash scripts/inject-version.sh
    MKDIR_BIN := mkdir -p
    RM_BIN := rm -f
else
    INJECT_VERSION := ./scripts/inject-version.sh
    MKDIR_BIN := mkdir -p
    RM_BIN := rm -f
endif

VERSION_LDFLAGS := $(shell $(INJECT_VERSION))

# Where the built binary lands.
BIN_DIR := bin

# The single server binary.
BIN := $(BIN_DIR)/dark-research-mcp

# Default target: show the available targets.
.PHONY: help
help:
	@echo "dark-research-mcp Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  make build       Build the binary into $(BIN_DIR)/ (dev mode)"
	@echo "  make release     Build the binary with the canonical git tag injected"
	@echo "  make drift-check Run drift checks (version, git status, CHANGELOG, vet, tests)"
	@echo "  make test        Run go test ./... (unit suite)"
	@echo "  make mutation    Run gremlins mutation pass on the core packages"
	@echo "  make clean       Remove $(BIN_DIR)/"
	@echo "  make version     Print the resolved version (raw)"
	@echo "  make version-json Print the version fingerprint (JSON)"
	@echo "  make tag         Print the latest git tag (for CI version pinning)"
	@echo ""
	@echo "Override the version explicitly:  DARK_VERSION=0.9.0-rc.1 make release"

# Build (dev mode). Uses the resolver's debug.ReadBuildInfo() path; the
# version printed by `dark-research-mcp -version` will reflect the
# module version or "dev" if built from a non-tagged commit.
.PHONY: build
build: $(BIN_DIR)
	cd cmd/dark-research-mcp && go build -o ../../$(BIN)/ .

# Release build. The version resolver is injected with the canonical
# git tag via scripts/inject-version.sh. This is the only path that
# produces a versioned production binary.
.PHONY: release
release: $(BIN_DIR)
	@echo "Injecting version: $(VERSION_LDFLAGS)"
	cd cmd/dark-research-mcp && go build -ldflags "$(VERSION_LDFLAGS)" -o ../../$(BIN)/ .
	@echo "Built:"
	@ls -lh $(BIN)

# Drift check: a battery of pre-commit / pre-push gates.
#   1. Working tree is clean (tags and CHANGELOG must be in the same
#      commit; a dirty tree at release time is a smell).
#   2. HEAD is at a git tag.
#   3. CHANGELOG.md has the matching `## [<tag>]` entry.
#   4. Internal version unit tests pass.
#   5. `go vet ./...` is clean.
#   6. Unit suite passes.
# Exits non-zero on any failure. Run before `git push` and before
# cutting a new tag.
.PHONY: drift-check
drift-check:
	@echo "--- drift-check ---"
	@echo "1. Working tree status:"
	@git status --short --branch || (echo "drift-check: not a git repo" && exit 1)
	@if [ -n "$$(git status --porcelain)" ]; then \
	    echo "drift-check: FAIL: working tree is dirty" >&2; \
	    exit 1; \
	fi
	@echo "  ok (clean)"
	@echo ""
	@echo "2. HEAD is at a tag:"
	@TAG=$$(git describe --tags --exact-match HEAD 2>/dev/null || echo ""); \
	if [ -z "$$TAG" ]; then \
	    echo "drift-check: FAIL: HEAD is not at any tag" >&2; \
	    exit 1; \
	fi; \
	echo "  tag: $$TAG"
	@echo ""
	@echo "3. CHANGELOG.md has matching entry:"
	@TAG=$$(git describe --tags --exact-match HEAD); \
	VERSION=$${TAG#v}; \
	if ! grep -q "^## \[$$VERSION\]" CHANGELOG.md; then \
	    echo "drift-check: FAIL: CHANGELOG.md missing entry for $$VERSION" >&2; \
	    exit 1; \
	fi; \
	echo "  CHANGELOG.md has [$$VERSION] entry"
	@echo ""
	@echo "4. internal/version unit tests:"
	go test -count=1 ./internal/version/... || (echo "drift-check: FAIL: version tests" && exit 1)
	@echo ""
	@echo "5. go vet:"
	go vet ./... || (echo "drift-check: FAIL: go vet" && exit 1)
	@echo ""
	@echo "drift-check: ALL GREEN"

# Tests.
.PHONY: test
test:
	go test ./...

# Mutation pass (gremlins v0.6.0 — the canonical tool; go-mutesting is
# retired). Runs on the core packages; see scripts/qa-gates.sh for the
# full gate surface.
.PHONY: mutation
mutation:
	bash scripts/qa-gates.sh mutation-quick

# Cleanup.
.PHONY: clean
clean:
	$(RM_BIN) $(BIN)
	@rmdir $(BIN_DIR) 2>/dev/null || true

# Print the resolved version (matches what the resolver would inject).
.PHONY: version
version:
	@bash scripts/inject-version.sh --raw
	@echo ""

# Print the JSON fingerprint (used by CI to stamp release artifacts).
.PHONY: version-json
version-json:
	@bash scripts/inject-version.sh --json

# Print the latest tag (for CI pinning).
.PHONY: tag
tag:
	@git describe --tags --abbrev=0

# Directory target.
$(BIN_DIR):
	$(MKDIR_BIN) $(BIN_DIR)
