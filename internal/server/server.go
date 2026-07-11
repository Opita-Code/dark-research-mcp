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
	"github.com/dark-agents/research-mcp/internal/tools"
	"github.com/mark3labs/mcp-go/server"
)

// New constructs the MCP server with all registered tools.
func New(cfg config.Config, store *mem.Store, session string) (*server.MCPServer, error) {
	s := server.NewMCPServer(
		"dark-research-mcp",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	if err := tools.Register(s, cfg, tools.Deps{Mem: store, Session: session}); err != nil {
		return nil, fmt.Errorf("register tools: %w", err)
	}

	return s, nil
}

// Serve runs the server on stdio until ctx is cancelled or stdin closes.
func Serve(ctx context.Context, s *server.MCPServer) error {
	return server.ServeStdio(s)
}