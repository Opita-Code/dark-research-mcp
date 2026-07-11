# syntax=docker/dockerfile:1.7
# dark-research-mcp sandboxed runtime.
#
# This image runs the dark-research-mcp binary in a hardened
# container for research deployments. It implements the
# deployment requirements documented in
# docs/security/threat-model.md:
#
#   1. Network: egress restricted to the LLM provider only
#      (api.minimax.io by default; override DARK_LLM_HOST).
#   2. Filesystem: read-only except /workspace (the project
#      dir the orchestrator writes to) and /tmp.
#   3. No privileged, no docker socket, no new privileges.
#   4. All Linux capabilities dropped.
#   5. Resource limits (memory, CPU) applied at runtime via
#      docker run flags (see docker-compose.yml).
#
# Build:
#   docker build -t dark-research-mcp:0.5.0-sandbox \
#     --build-arg VERSION=0.5.0 \
#     -f deploy/sandbox/Dockerfile .
#
# Run (with docker-compose):
#   cd deploy/sandbox && docker compose up
#
# Run (standalone, with explicit network restriction):
#   docker run --rm \
#     --network dark-research-llm-only \
#     --read-only \
#     --tmpfs /tmp:size=100m,noexec,nosuid \
#     --tmpfs /run:size=10m,noexec,nosuid \
#     --security-opt no-new-privileges \
#     --cap-drop ALL \
#     --memory=512m \
#     --cpus=1.0 \
#     -v /path/to/workspace:/workspace:rw \
#     -e SDD_LLM_BASE_URL=https://api.minimax.io/anthropic \
#     -e SDD_LLM_MODEL=MiniMax-M3 \
#     -e SDD_LLM_API_KEY_FILE=/run/secrets/llm-key \
#     --mount type=bind,source=/path/to/key,target=/run/secrets/llm-key,ro \
#     dark-research-mcp:0.5.0-sandbox

ARG VERSION=dev

# Stage 1: build the dark-research-mcp binary.
# Uses a separate stage so the runtime image has no build toolchain.
FROM golang:1.25-alpine AS builder

ARG VERSION
ARG TAGS=""

WORKDIR /src

# Cache go.mod/go.sum first.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source.
COPY . .

# Build the tagged binary. TAGS="" is the stock binary (no
# dark content); TAGS="allow_builtin_dark" includes the
# dark constitution for research deployments.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -tags "${TAGS}" \
    -o /out/dark-research-mcp \
    ./cmd/dark-research-mcp

# Stage 2: minimal runtime image.
# Distroless-like: no shell, no package manager, no utilities
# beyond what the binary itself uses (it has none — pure Go
# stdlib + modernc.org/sqlite for the DB).
FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION
LABEL org.opencontainers.image.title="dark-research-mcp"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.source="https://github.com/Opita-Code/dark-research-mcp"
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.dark-agents.research="true"
LABEL org.dark-agents.threat-model="https://github.com/Opita-Code/dark-research-mcp/blob/main/docs/security/threat-model.md"

COPY --from=builder /out/dark-research-mcp /usr/local/bin/dark-research-mcp

# The nonroot user is uid 65532. We add a workspace dir owned
# by that user so the orchestrator (running as the same user)
# can read/write.
WORKDIR /workspace

# Default to the light constitution. The orchestrator
# (Pi) is expected to launch the binary with --constitution
# dark when needed. We do NOT default to dark because the
# defense layer (in v0.4.1+) requires the orchestrator
# setup to be vetted; the default is the safe one.
ENTRYPOINT ["/usr/local/bin/dark-research-mcp"]
CMD []
