// Package server wires the MCP server, registers tools, and serves over stdio.
//
// Logging MUST go to stderr: the stdio transport uses stdout for JSON-RPC
// frames, so writing log lines there corrupts the protocol.
package server

import (
	"context"
	"fmt"

	"github.com/dark-agents/research-mcp/internal/config"
	"github.com/dark-agents/research-mcp/internal/mem"
	"github.com/dark-agents/research-mcp/internal/mods"
	"github.com/dark-agents/research-mcp/internal/tools"
	"github.com/mark3labs/mcp-go/server"
)

// version is set at link time via:
//
//	go build -ldflags "-X main.version=0.8.0" ./cmd/dark-research-mcp
//
// The default "dev" is what you get from a plain `go build` for local
// development. CI release builds always set the real version.
var version = "0.8.0"

// New constructs the MCP server with all registered tools. The
// mods registry is optional (nil = no mods; the dark_ssd_*
// judges receive an empty ActiveMods list).
//
// v0.8.0 — cx.v3 conformance (BRIDGE_AND_COEXISTENCE.md v2.0.0 §3.2).
// dark-research-mcp is demoted from sibling surface to tool backing.
// The initialize response (via MCP `instructions` channel — mcp-go's
// Implementation struct v0.56.0 doesn't carry custom fields, see the
// BRIDGE doc for the upstream-tracking rationale) now declares:
//
//	coexistence_group=dark-agents/research
//	policy_gateway=false
//
// Harnesses that detect another dark-agents peer (dark-memory-mcp)
// with `policy_gateway=true` should route dark-* calls through the
// gateway for persona shaping, capability checks, and drift-at-write.
// Direct dark_research_* calls continue to work in legacy fallback mode.
func New(cfg config.Config, store *mem.Store, session string, modsReg *mods.Registry) (*server.MCPServer, error) {
	s := server.NewMCPServer(
		"dark-research-mcp",
		version,
		server.WithToolCapabilities(true),
		server.WithInstructions(BuildInstructions(version)),
	)

	if err := tools.Register(s, cfg, tools.Deps{Mem: store, Session: session, Mods: modsReg}); err != nil {
		return nil, fmt.Errorf("register tools: %w", err)
	}

	return s, nil
}

// BuildInstructions returns the MCP `instructions` string baked into the
// initialize response. Exported (not unexported) so server_test.go can
// assert the cx.v3 metadata without reflection. mcp-go v0.56.0's
// MCPServer struct holds `instructions` as an unexported field; the
// only public surface for the wire format is the function this builds.
//
// The string is the conformance contract per BRIDGE_AND_COEXISTENCE.md
// v2.0.0 §2.1 + §3.2:
//
//	coexistence_group=dark-agents/research
//	policy_gateway=false
//
// Tests in server_test.go assert these substrings.
func BuildInstructions(version string) string {
	return fmt.Sprintf(
		"dark-research-mcp server. coexistence_group=dark-agents/research policy_gateway=false (spec 164 bridge.2 cx.v3). OSINT backing: 13 intents (web, academic, code, cve, domain, dns, cert, ip, threat, email, dark, geo, news) + multi + router. dark_mem_* namespace is a frozen shim; use dark_memory_* from dark-memory-mcp. Version=%s.",
		version,
	)
}

// Serve runs the server on stdio until ctx is cancelled or stdin closes.
func Serve(ctx context.Context, s *server.MCPServer) error {
	return server.ServeStdio(s)
}