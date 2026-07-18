package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestComplete_success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("expected /v1/messages, got %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("missing or wrong api key: %s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"hello world"}],"stop_reason":"end_turn"}`))
	}))
	defer server.Close()

	c := &Client{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "test-model",
		HTTP:    &http.Client{Timeout: 5 * time.Second},
	}
	got, err := c.Complete(context.Background(), "you are a test", Message{Role: "user", Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestComplete_non2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	c := &Client{BaseURL: server.URL, APIKey: "x", Model: "y", HTTP: server.Client()}
	_, err := c.Complete(context.Background(), "sys", Message{Role: "user", Content: "x"})
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention 401: %v", err)
	}
}

func TestComplete_nilClient(t *testing.T) {
	var c *Client
	_, err := c.Complete(context.Background(), "sys", Message{Role: "user", Content: "x"})
	if err == nil {
		t.Fatal("expected error on nil client")
	}
}

func TestCompleteJSON_parsesWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// LLM sometimes wraps JSON in ```json ... ```. Simulate that.
		// The text field contains a real ```json\n...\n``` wrapper.
		body := "{\"content\":[{\"type\":\"text\",\"text\":\"```json\\n{\\\"verdict\\\":\\\"aligned\\\",\\\"confidence\\\":0.9}\\n```\"}],\"stop_reason\":\"end_turn\"}"
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	c := &Client{BaseURL: server.URL, APIKey: "x", Model: "y", HTTP: server.Client()}
	var result struct {
		Verdict    string  `json:"verdict"`
		Confidence float64 `json:"confidence"`
	}
	if err := c.CompleteJSON(context.Background(), "sys", "u", &result); err != nil {
		t.Fatal(err)
	}
	if result.Verdict != "aligned" || result.Confidence != 0.9 {
		t.Errorf("got %+v", result)
	}
}

func TestCompleteJSON_invalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"not json"}],"stop_reason":"end_turn"}`))
	}))
	defer server.Close()

	c := &Client{BaseURL: server.URL, APIKey: "x", Model: "y", HTTP: server.Client()}
	var v map[string]any
	err := c.CompleteJSON(context.Background(), "sys", "u", &v)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

// TestNewFromEnv_* assume "direct mode" (no dark-scrapper daemon in the
// loop) and a deterministic provider chain. The helper
// resetLLMEnv (defined in dark_scrapper_test.go) clears every env
// var NewFromEnv reads via os.Unsetenv — strictly stronger than
// t.Setenv(key, "") which leaves the variable "set but empty" — and
// lets each test opt in to specific vars via the set map. Same
// isolation pattern used by TestDarkScrapper_NewFromEnv_*.
//
// Without this isolation, a developer running `go test ./...` from a
// shell with Use-DarkAgentSecrets loaded sees spurious failures
// because ANTHROPIC_API_KEY and ANTHROPIC_BASE_URL leak in from the
// parent env, even after t.Setenv(key, "") on the keys this test
// directly checks. The post-BUG-001 NewFromEnv recognizes
// DARK_SCRAPPER_URL as a daemon-mode signal (bug-hunt 2026-07-14
// BUG-001, commit 4dd0045), but the leak also reaches the key chain
// on lines 107–109 of internal/llm/client.go.
func TestNewFromEnv_returnsNilWithoutKey(t *testing.T) {
	resetLLMEnv(t, nil)
	if c := NewFromEnv(); c != nil {
		t.Errorf("expected nil, got %+v", c)
	}
}

func TestNewFromEnv_usesKey(t *testing.T) {
	resetLLMEnv(t, map[string]string{"SDD_LLM_API_KEY": "from-sdd"})
	c := NewFromEnv()
	if c == nil {
		t.Fatal("expected client")
	}
	if c.APIKey != "from-sdd" {
		t.Errorf("expected from-sdd, got %s", c.APIKey)
	}
	if c.BaseURL != "https://api.minimax.io/anthropic" {
		t.Errorf("default base url wrong: %s", c.BaseURL)
	}
	if c.Model != "MiniMax-M3" {
		t.Errorf("default model wrong: %s", c.Model)
	}
}

func TestNewFromEnv_fallsBackToMinimax(t *testing.T) {
	resetLLMEnv(t, map[string]string{"MINIMAX_API_KEY": "from-minimax"})
	c := NewFromEnv()
	if c == nil || c.APIKey != "from-minimax" {
		t.Errorf("expected fallback to minimax, got %+v", c)
	}
}