// Package tools — defense wrapper.
//
// Every tool handler in the binary goes through defenseWrap
// before reaching the MCP server. The wrapper applies the
// L1 (input validation), L8 (rate limit), and L2+L7
// (output sanitization with canary detection) defenses from
// the safety package. The wrapper is universal — applies in
// light AND dark constitutions.
//
// The wrapper is fail-open by design for non-security
// concerns (markers in input are logged, not blocked) and
// fail-closed for security concerns (canary in input or
// output is a hard reject).
package tools

import (
	"context"
	"fmt"
	"log"

	"github.com/dark-agents/research-mcp/internal/safety"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// defenseWrap returns a new ToolHandlerFunc that wraps inner
// with input validation, rate limiting, and output
// sanitization. If `safety.Default()` returns nil (defense
// not initialized), the wrapper is a passthrough — this keeps
// the test suite and the early-boot path functional.
func defenseWrap(toolName string, inner server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		df := safety.Default()
		if df == nil {
			// Defense not initialized (test path or pre-init
			// call). Passthrough.
			return inner(ctx, req)
		}

		// L8 — Rate limit.
		if !df.AllowToolCall(toolName) {
			log.Printf("safety: rate limit hit for tool=%s total_calls=%d", toolName, df.Limiter.Count())
			return nil, fmt.Errorf("safety: rate limit exceeded for session (cap=%d). The defense is doing its job — a runaway orchestrator or extraction attempt is being throttled. Increase the cap via the --max-calls flag if this is a legitimate workload.", df.Limiter.Count())
		}

		// L1 — Input validation. The mcp.CallToolRequest
		// exposes arguments via GetArguments() which returns
		// a map[string]any. We use that directly.
		args := req.GetArguments()
		vres := df.CheckInput(toolName, args)
		if !vres.OK {
			// Hard reject: do NOT call the inner handler.
			if vres.CanaryLeak {
				log.Printf("safety: canary leak attempt in tool=%s input — refusing call", toolName)
				return nil, fmt.Errorf("safety: input rejected — contains a canary token. The constitution-extraction attempt has been logged.")
			}
			return nil, fmt.Errorf("safety: input rejected (overlong, too many args, or other hard violation)")
		}
		if len(vres.Markers) > 0 {
			// Logged but not blocked — the dark_ssd_* judges
			// are designed to receive injection markers. The
			// audit log captures what flowed through.
			log.Printf("safety: tool=%s input contained %d injection marker(s) [expected for dark_ssd_* judges]: %v", toolName, len(vres.Markers), vres.Markers)
		}

		// Call the actual tool handler.
		result, err := inner(ctx, req)
		if err != nil {
			return result, err
		}

		// L2 + L7 — Output sanitization. If the tool result
		// contains the canary token, this is a CRITICAL event.
		// We refuse to surface the result and log full context.
		//
		// The MCP result is a structured value. We only need to
		// scan the textual content. For most dark_ssd_* tools,
		// the result is a JSON-encoded verdict string.
		text := extractTextFromResult(result)
		if text != "" {
			ores := df.CheckOutput(toolName, text)
			if ores.CanaryLeaked {
				log.Printf("safety: CRITICAL canary leaked in tool=%s output — refusing to surface. Excerpt: %q", toolName, ores.Excerpt)
				return nil, fmt.Errorf("safety: output rejected — canary token detected in tool result. This is a critical security event. The LLM-judge (or content it processed) appears to have extracted or echoed the system prompt. The verdict has been withheld; the audit log has the full excerpt.")
			}
			if len(ores.InjectionMarkers) > 0 {
				// Logged but not blocked. The LLM's
				// reasoning field may legitimately contain
				// patterns that match markers; the
				// operator's job to review.
				log.Printf("safety: tool=%s output contained %d injection marker(s) (audit only): %v", toolName, len(ores.InjectionMarkers), ores.InjectionMarkers)
			}
		}

		return result, nil
	}
}

// extractTextFromResult pulls textual content from the MCP
// result for sanitization scanning. The result is one of:
//   - *mcp.CallToolResult with .Content ([]Content) each
//     having .Text (string) or .Data (base64-encoded bytes).
//   - nil.
//
// We only scan .Text — binary data is out of scope.
func extractTextFromResult(r *mcp.CallToolResult) string {
	if r == nil {
		return ""
	}
	var sb stringBuilder
	for _, c := range r.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// stringBuilder is a tiny local wrapper to keep this file
// dependency-free of strings.Builder imports.
type stringBuilder struct{ b []byte }

func (s *stringBuilder) WriteString(p string) { s.b = append(s.b, p...) }
func (s *stringBuilder) String() string       { return string(s.b) }

// wrapAll applies defenseWrap to every tool in the slice. Used
// by Register so the wrapper is consistent across the binary.
func wrapAll(tools []Tool) []Tool {
	for i := range tools {
		tools[i].Handler = defenseWrap(tools[i].Definition.Name, tools[i].Handler)
	}
	return tools
}
