// Package llm is a minimal Anthropic-compatible LLM client used by the
// dark-ssd (LLM-as-judge) tools in dark-research-mcp.
//
// The client is configured entirely from env vars. If no key is set,
// NewFromEnv returns nil and the tools that depend on it return a
// descriptive error so the agent can fall back to its own LLM.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Client is a minimal Anthropic-compatible chat client.
type Client struct {
	BaseURL string // e.g. https://api.minimax.io/anthropic
	APIKey  string
	Model   string // e.g. MiniMax-M3
	HTTP    *http.Client
	Cache   *Cache // optional file-backed JSON cache; nil = no caching
}

// Env vars (with fallbacks):
//   SDD_LLM_API_KEY    or    MINIMAX_API_KEY   (required)
//   SDD_LLM_BASE_URL   (default https://api.minimax.io/anthropic)
//   SDD_LLM_MODEL      (default MiniMax-M3)
func NewFromEnv() *Client {
	key := firstEnv("SDD_LLM_API_KEY", "MINIMAX_API_KEY")
	if key == "" {
		return nil
	}
	base := os.Getenv("SDD_LLM_BASE_URL")
	if base == "" {
		base = "https://api.minimax.io/anthropic"
	}
	model := os.Getenv("SDD_LLM_MODEL")
	if model == "" {
		model = "MiniMax-M3"
	}
	return &Client{
		BaseURL: strings.TrimRight(base, "/"),
		APIKey:  key,
		Model:   model,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// Message is a single message in the conversation.
type Message struct {
	Role    string // "user" or "assistant"
	Content string
}

// Complete sends system + user to the LLM and returns the assistant text.
// Uses Anthropic's /v1/messages endpoint.
func (c *Client) Complete(ctx context.Context, system string, msgs ...Message) (string, error) {
	if c == nil {
		return "", fmt.Errorf("llm: not configured (set SDD_LLM_API_KEY or MINIMAX_API_KEY)")
	}
	if len(msgs) == 0 {
		return "", fmt.Errorf("llm: no messages provided")
	}

	body := map[string]any{
		"model":      c.Model,
		"max_tokens": 2048,
		"system":     system,
		"messages":   toAnthropicMessages(msgs),
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	url := c.BaseURL + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm: http %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("llm: parse response: %w", err)
	}

	var sb strings.Builder
	for _, c := range parsed.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String(), nil
}

// CompleteJSON calls Complete and parses the response as JSON. The system
// prompt should instruct the model to return JSON only. If Client.Cache
// is set, the call is cached (FNV-1a key over model+system+user) with
// TTL=cache.ttl.
func (c *Client) CompleteJSON(ctx context.Context, system, user string, v any) error {
	text, err := c.CompleteCached(ctx, c.Cache, system, Message{Role: "user", Content: user})
	if err != nil {
		return err
	}
	text = stripCodeFences(text)
	return json.Unmarshal([]byte(text), v)
}

func stripCodeFences(s string) string {
	// Some models wrap JSON in ```json ... ```. Strip the fences.
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// find first newline, last ```
		firstNL := strings.Index(s, "\n")
		if firstNL > 0 {
			s = s[firstNL+1:]
		}
		if strings.HasSuffix(s, "```") {
			s = s[:len(s)-3]
		}
	}
	return strings.TrimSpace(s)
}

// StripCodeFencesForTools is the exported version of
// stripCodeFences, used by the tools package which does not
// import internal/unexported symbols. Behavior is byte-identical
// to stripCodeFences; the alias exists to avoid duplicating the
// implementation in two places.
func StripCodeFencesForTools(s string) string {
	return stripCodeFences(s)
}

func toAnthropicMessages(msgs []Message) []map[string]string {
	out := make([]map[string]string, len(msgs))
	for i, m := range msgs {
		out[i] = map[string]string{"role": m.Role, "content": m.Content}
	}
	return out
}
