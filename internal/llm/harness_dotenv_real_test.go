// Diagnostic tests that read the operator's REAL environment to
// confirm the harness dotenv loader is wired correctly. These tests
// always pass (or skip if no harness env is reachable); they exist
// to print evidence during `go test -v` runs.
package llm

import (
	"sync"
	"testing"
)

// TestLoadHarnessDotenv_RealRegistry verifies the loader finds the
// operator's user-level env via HKCU\Environment. Diagnostic: it
// always PASSES (just logs what's found).
func TestLoadHarnessDotenv_RealRegistry(t *testing.T) {
	dotenv := LoadHarnessDotenv()
	t.Logf("harness dotenv found %d keys:", len(dotenv))
	for _, k := range []string{"ANTHROPIC_API_KEY", "MINIMAX_API_KEY", "OPENAI_API_KEY", "DARK_SCRAPPER_URL", "SDD_LLM_API_KEY", "SDD_LLM_BASE_URL", "DARK_FEDERATION_PEER_DSN"} {
		v, ok := dotenv[k]
		if ok {
			masked := "<empty>"
			if len(v) > 4 {
				masked = v[:4] + "***"
			}
			t.Logf("  %s = %s (from dotenv)", k, masked)
		} else {
			t.Logf("  %s = <not in dotenv>", k)
		}
	}
}

// TestNewFromEnv_ScrapperDown_LoadsHarnessDotenv is a DIAGNOSTIC test.
// It reads the operator's real harness env (HKCU\Environment on Windows
// or $HOME/.env on Unix) and prints whether the fallback chain armed.
// It does NOT assert: that would require a controlled harness env which
// is the operator's own state and varies between local dev machines,
// CI runners, and container builds. The CI-runnable assertion version
// lives in harness_dotenv_test.go as
// TestNewFromEnv_ScrapperActive_ArmsHarnessFallback.
//
// This test exists so `go test -v` on an operator's local box prints
// evidence that the harness dotenv loader sees what the operator
// expects. Operators should NOT fail the build when running this on
// CI; the assertion below is intentionally absent.
func TestNewFromEnv_ScrapperDown_LoadsHarnessDotenv(t *testing.T) {
	t.Setenv("DARK_SCRAPPER_URL", "http://127.0.0.1:1")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("MINIMAX_API_KEY", "")
	t.Setenv("SDD_LLM_API_KEY", "")
	t.Setenv("SDD_LLM_PROVIDER", "")

	dotenvOnce = sync.Once{}
	dotenvCache = nil
	t.Cleanup(func() {
		dotenvOnce = sync.Once{}
		dotenvCache = nil
	})

	c := NewFromEnv()
	if c == nil {
		t.Logf("DIAGNOSTIC: no harness dotenv available in this environment; skipping fallback-chain evidence")
		return
	}
	if c.HarnessDotenvKey == "" {
		t.Logf("DIAGNOSTIC: harness fallback NOT armed (operator env has no MINIMAX_API_KEY/ANTHROPIC_API_KEY/SDD_LLM_API_KEY/OPENAI_API_KEY)")
		return
	}
	t.Logf("DIAGNOSTIC: fallback chain armed: provider=%s, key=%s***", c.HarnessDotenvProvider, c.HarnessDotenvKey[:min(4, len(c.HarnessDotenvKey))])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
