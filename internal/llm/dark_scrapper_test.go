package llm

import (
	"os"
	"strings"
	"testing"
)

// resetLLMEnv clears all env vars the LLM client reads, then optionally
// sets the provided ones. Returns a cleanup func.
func resetLLMEnv(t *testing.T, set map[string]string) {
	t.Helper()
	for _, k := range []string{
		"DARK_SCRAPPER_URL",
		"SDD_LLM_PROVIDER", "SDD_LLM_BASE_URL", "SDD_LLM_API_KEY", "SDD_LLM_MODEL",
		"MINIMAX_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL",
		"OPENAI_API_KEY", "OPENAI_BASE_URL",
	} {
		os.Unsetenv(k)
	}
	for k, v := range set {
		t.Setenv(k, v)
	}
}

func TestNewFromEnv_DarkScrapper(t *testing.T) {
	t.Run("anthropic_routes_to_daemon_messages", func(t *testing.T) {
		resetLLMEnv(t, map[string]string{
			"DARK_SCRAPPER_URL": "http://127.0.0.1:8901",
		})
		t.Logf("env after reset: DARK=%q SDD_PROVIDER=%q",
			os.Getenv("DARK_SCRAPPER_URL"), os.Getenv("SDD_LLM_PROVIDER"))
		// Re-implement provider detection in test to see what NewFromEnv
		// should have set it to.
		t.Logf("explicitBase: %v", os.Getenv("DARK_SCRAPPER_URL") != "")
		c := NewFromEnv()
		if c == nil {
			t.Fatal("NewFromEnv returned nil")
		}
		t.Logf("client: BaseURL=%q APIKey=%q Provider=%q", c.BaseURL, c.APIKey, c.Provider)
		// NewFromEnv stores the BARE daemon URL; completeAnthropic appends
		// "/v1/messages" at request time. Assert the bare URL plus the
		// effective request URL is correct.
		if c.BaseURL != "http://127.0.0.1:8901" {
			t.Errorf("BaseURL = %q, want %q (bare daemon URL; /v1/messages appended at request time)",
				c.BaseURL, "http://127.0.0.1:8901")
		}
		if c.APIKey != "ds-managed" {
			t.Errorf("APIKey = %q, want ds-managed", c.APIKey)
		}
		if c.Provider != ProviderAnthropic {
			t.Errorf("Provider = %q, want anthropic", c.Provider)
		}
	})

	t.Run("openai_routes_to_daemon_chat_completions", func(t *testing.T) {
		resetLLMEnv(t, map[string]string{
			"DARK_SCRAPPER_URL": "http://127.0.0.1:8901",
			"SDD_LLM_PROVIDER":  "openai",
		})
		c := NewFromEnv()
		if c == nil {
			t.Fatal("NewFromEnv(openai) returned nil")
		}
		// Same convention: base URL is bare, /v1/chat/completions appended
		// at request time in completeOpenAI.
		if c.BaseURL != "http://127.0.0.1:8901" {
			t.Errorf("BaseURL = %q, want %q (bare daemon URL; /v1/chat/completions appended at request time)",
				c.BaseURL, "http://127.0.0.1:8901")
		}
	})

	t.Run("trailing_slash_trimmed", func(t *testing.T) {
		resetLLMEnv(t, map[string]string{
			"DARK_SCRAPPER_URL": "http://127.0.0.1:8901/",
		})
		c := NewFromEnv()
		if c == nil {
			t.Fatalf("NewFromEnv returned nil; trailing slash regression")
		}
		if c.BaseURL != "http://127.0.0.1:8901" {
			t.Errorf("trailing slash not trimmed, BaseURL = %q", c.BaseURL)
		}
	})

	t.Run("daemon_overrides_explicit_base_url", func(t *testing.T) {
		// Even if SDD_LLM_BASE_URL is set, DARK_SCRAPPER_URL wins.
		resetLLMEnv(t, map[string]string{
			"DARK_SCRAPPER_URL": "http://127.0.0.1:8901",
			"SDD_LLM_BASE_URL":   "https://api.openai.com/v1",
		})
		c := NewFromEnv()
		if c == nil || !strings.HasPrefix(c.BaseURL, "http://127.0.0.1:8901") {
			t.Errorf("daemon should win over explicit base, got BaseURL = %q", c.BaseURL)
		}
	})

	t.Run("legacy_fallback_when_daemon_not_set", func(t *testing.T) {
		resetLLMEnv(t, map[string]string{
			"MINIMAX_API_KEY": "sk-test-legacy",
		})
		c := NewFromEnv()
		if c == nil {
			t.Fatal("legacy fallback returned nil")
		}
		if !strings.HasPrefix(c.BaseURL, "https://api.minimax.io/anthropic") {
			t.Errorf("legacy BaseURL = %q, want api.minimax.io/anthropic", c.BaseURL)
		}
		if c.APIKey != "sk-test-legacy" {
			t.Errorf("legacy APIKey = %q, want real key", c.APIKey)
		}
	})

	t.Run("unconfigured_returns_nil", func(t *testing.T) {
		resetLLMEnv(t, nil)
		c := NewFromEnv()
		if c != nil {
			t.Errorf("expected nil when unconfigured, got %+v", c)
		}
	})
}