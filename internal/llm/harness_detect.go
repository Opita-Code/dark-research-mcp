// Package llm: harness detection.
//
// DetectHarness reads env-var markers the parent AI-coding harness
// sets when it spawns the MCP server and returns the matching
// Harness enum value. Used at startup for logging (so the operator
// sees which harness the binary is serving) and as a hook for
// harness-specific configuration in the dark-agents-v2 fork.
//
// Detection is best-effort: when no marker matches, the binary
// runs identically (HarnessUnknown is treated as "no harness
// detected, default config"). False positives are not catastrophic;
// the LLM client and tool registry don't change behaviour based on
// the detected harness.
package llm

import (
	"os"
	"strings"
)

// Harness identifies which AI coding agent is invoking the MCP
// binary. Detection is best-effort: a value of HarnessUnknown means
// no marker matched, and the binary uses default behaviour.
type Harness int

const (
	HarnessUnknown Harness = iota
	HarnessOpenCode
	HarnessClaudeCode
	HarnessCursor
	HarnessAider
	HarnessCline
)

// String returns the canonical lower-snake name of the harness, used
// in startup logs and audit trails.
func (h Harness) String() string {
	switch h {
	case HarnessOpenCode:
		return "opencode"
	case HarnessClaudeCode:
		return "claude_code"
	case HarnessCursor:
		return "cursor"
	case HarnessAider:
		return "aider"
	case HarnessCline:
		return "cline"
	default:
		return "unknown"
	}
}

// EnvVars returns the env-var names this harness typically sets
// when spawning the MCP server. Useful for the dark-agents-v2
// fork's "what does Claude Code expect me to read" introspection.
func (h Harness) EnvVars() []string {
	switch h {
	case HarnessClaudeCode:
		return []string{"CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SSE_PORT", "ANTHROPIC_API_KEY"}
	case HarnessOpenCode:
		return []string{"OPENCODE_DEFAULT_MODEL", "OPENCODE", "DARK_DB"}
	case HarnessCursor:
		return []string{"CURSOR_TRACE_ID", "CURSOR_CHANNEL", "VSCODE_PID"}
	case HarnessAider:
		return []string{"AIDER_MODEL", "AIDER_*"}
	case HarnessCline:
		return []string{"CLINE_ENTRYPOINT", "CLINE_VERSION"}
	default:
		return nil
	}
}

// DetectHarness reads env-var markers the parent harness sets and
// returns the matching Harness. Order matters: Claude Code sets
// multiple distinct vars, OpenCode sets one, etc. The first match
// wins, so put the most-specific detectors first.
//
// False-positive cost is low (the only consumer is logging +
// dark-agents fork-specific config). False-negative cost is also
// low (HarnessUnknown defaults to the same behaviour).
func DetectHarness() Harness {
	// Claude Code: any CLAUDE_CODE_* marker is sufficient.
	// ENTRYPOINT is the canonical marker set by the official
	// `claude mcp serve` flow (see docs.claude.com/en/docs/claude-code/mcp);
	// SSE_PORT is set when running via the HTTP transport. We accept
	// either so detection works across transport modes.
	if os.Getenv("CLAUDE_CODE_ENTRYPOINT") != "" || os.Getenv("CLAUDE_CODE_SSE_PORT") != "" {
		return HarnessClaudeCode
	}

	// Aider: AIDER_MODEL is the canonical config var. Aider doesn't
	// spawn an MCP server directly yet (2026-07 status: MCP Code
	// Mode is gated behind an opt-in flag), but when it does the
	// AIDER_* namespace is the convention.
	if v := os.Getenv("AIDER_MODEL"); v != "" {
		return HarnessAider
	}

	// OpenCode: OPENCODE_DEFAULT_MODEL or OPENCODE markers.
	if os.Getenv("OPENCODE_DEFAULT_MODEL") != "" || os.Getenv("OPENCODE") != "" {
		return HarnessOpenCode
	}

	// Cline (VS Code extension): sets CLINE_* when running the MCP
	// server. Detection order: Cline-specific vars beat Cursor's
	// VS Code vars because both run inside VS Code.
	if os.Getenv("CLINE_ENTRYPOINT") != "" || strings.HasPrefix(os.Getenv("CLINE_VERSION"), "v") {
		return HarnessCline
	}

	// Cursor (VS Code fork): sets CURSOR_TRACE_ID and friends.
	// VSCODE_PID is generic; we use the Cursor-specific vars.
	if os.Getenv("CURSOR_TRACE_ID") != "" || os.Getenv("CURSOR_CHANNEL") != "" {
		return HarnessCursor
	}

	return HarnessUnknown
}