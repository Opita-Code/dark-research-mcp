package llm

import (
	"testing"
)

// TestDetectHarness covers the env-var markers each harness sets.
// Markers are intentionally short (one or two vars per harness) so
// the test table reads cleanly. Each case sets a single marker and
// asserts the corresponding Harness enum value.
func TestDetectHarness(t *testing.T) {
	// Use a helper that unsets every known marker, then sets only
	// the ones the case wants — matches the isolation pattern in
	// resetLLMEnv (client_test.go) but for harness markers.
	clearHarnessEnv(t)

	cases := []struct {
		name    string
		setEnv  map[string]string
		want    Harness
	}{
		{
			name:   "claude_code_via_CLAUDE_CODE_ENTRYPOINT",
			setEnv: map[string]string{"CLAUDE_CODE_ENTRYPOINT": "claude"},
			want:   HarnessClaudeCode,
		},
		{
			name:   "claude_code_via_CLAUDE_CODE_SSE_PORT",
			setEnv: map[string]string{"CLAUDE_CODE_SSE_PORT": "8080"},
			want:   HarnessClaudeCode,
		},
		{
			name:   "opencode_via_OPENCODE_DEFAULT_MODEL",
			setEnv: map[string]string{"OPENCODE_DEFAULT_MODEL": "anthropic/claude-sonnet"},
			want:   HarnessOpenCode,
		},
		{
			name:   "opencode_via_OPENCODE",
			setEnv: map[string]string{"OPENCODE": "1"},
			want:   HarnessOpenCode,
		},
		{
			name:   "aider_via_AIDER_MODEL",
			setEnv: map[string]string{"AIDER_MODEL": "sonnet"},
			want:   HarnessAider,
		},
		{
			name:   "cline_via_CLINE_ENTRYPOINT",
			setEnv: map[string]string{"CLINE_ENTRYPOINT": "cline-mcp"},
			want:   HarnessCline,
		},
		{
			name:   "cursor_via_CURSOR_TRACE_ID",
			setEnv: map[string]string{"CURSOR_TRACE_ID": "abc123"},
			want:   HarnessCursor,
		},
		{
			name:   "cursor_via_CURSOR_CHANNEL",
			setEnv: map[string]string{"CURSOR_CHANNEL": "stable"},
			want:   HarnessCursor,
		},
		{
			name:   "no_markers",
			setEnv: nil,
			want:   HarnessUnknown,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clearHarnessEnv(t)
			for k, v := range c.setEnv {
				t.Setenv(k, v)
			}
			if got := DetectHarness(); got != c.want {
				t.Errorf("DetectHarness() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestHarness_String(t *testing.T) {
	cases := []struct {
		h    Harness
		want string
	}{
		{HarnessOpenCode, "opencode"},
		{HarnessClaudeCode, "claude_code"},
		{HarnessCursor, "cursor"},
		{HarnessAider, "aider"},
		{HarnessCline, "cline"},
		{HarnessUnknown, "unknown"},
	}
	for _, c := range cases {
		if got := c.h.String(); got != c.want {
			t.Errorf("Harness(%d).String() = %q, want %q", c.h, got, c.want)
		}
	}
}

func TestHarness_EnvVars(t *testing.T) {
	// Smoke test: each non-unknown harness should expose at least one
	// env var name; HarnessUnknown exposes none.
	for _, h := range []Harness{HarnessOpenCode, HarnessClaudeCode, HarnessCursor, HarnessAider, HarnessCline} {
		if vars := h.EnvVars(); len(vars) == 0 {
			t.Errorf("%s.EnvVars() returned empty list", h)
		}
	}
	if vars := HarnessUnknown.EnvVars(); vars != nil {
		t.Errorf("HarnessUnknown.EnvVars() = %v, want nil", vars)
	}
}

// clearHarnessEnv unsets every env-var marker DetectHarness reads.
// Same isolation pattern as resetLLMEnv in client_test.go.
func clearHarnessEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"CLAUDE_CODE_ENTRYPOINT",
		"CLAUDE_CODE_SSE_PORT",
		"ANTHROPIC_API_KEY",
		"OPENCODE_DEFAULT_MODEL",
		"OPENCODE",
		"DARK_DB",
		"CURSOR_TRACE_ID",
		"CURSOR_CHANNEL",
		"VSCODE_PID",
		"AIDER_MODEL",
		"CLINE_ENTRYPOINT",
		"CLINE_VERSION",
	} {
		t.Setenv(k, "")
	}
}