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

// TestNewFromEnv_ScrapperDown_LoadsHarnessDotenv verifies the
// canonical fix path: when DARK_SCRAPPER_URL is set (scrapper
// pattern) but the operator's process env has no API key, the
// resulting Client has HarnessDotenvKey populated from the
// harness's HKCU\Environment / $HOME/.env / project .env /
// opencode.jsonc chain.
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
		t.Skip("no harness dotenv available in this environment; not a failure, just missing fixture")
	}
	if c.HarnessDotenvKey == "" {
		t.Errorf("HarnessDotenvKey is empty; scrapper is active but no harness fallback key was loaded")
	}
	if c.HarnessDotenvProvider == "" {
		t.Errorf("HarnessDotenvProvider is empty")
	}
	t.Logf("fallback chain armed: provider=%s, key=%s***", c.HarnessDotenvProvider, c.HarnessDotenvKey[:min(4, len(c.HarnessDotenvKey))])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
