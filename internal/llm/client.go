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

// Provider constants.
const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
)

// Client is a minimal LLM client compatible with Anthropic or OpenAI APIs.
type Client struct {
	BaseURL    string // e.g. https://api.minimax.io/anthropic or http://127.0.0.1:8088/v1
	APIKey     string
	Model      string // e.g. MiniMax-M3 or Qwen3.5-9B
	Provider   string // "anthropic" (default) or "openai"
	HTTP       *http.Client
	Cache      *Cache // optional file-backed JSON cache; nil = no caching
	// Optional fallback: if the primary key/baseURL returns 429 (rate limited)
	// or 401 (revoked) or 5xx (server error), automatically retry with the
	// fallback credentials. Both fallback key and base URL must be configured.
	FallbackAPIKey   string
	FallbackBaseURL  string
	// HarnessDotenvKey is the direct-provider API key recovered from
	// the parent harness's env (Windows registry, $HOME/.env, project
	// .env, opencode.jsonc, claude settings.json). Used as the ULTIMATE
	// fallback when the primary path (scrapper or direct) fails with a
	// network error — the LLM must never get stuck waiting on a daemon
	// that's offline. Empty when no harness env is reachable.
	HarnessDotenvKey string
	// HarnessDotenvProvider is the provider of HarnessDotenvKey
	// ("anthropic" or "openai"); empty when HarnessDotenvKey is empty.
	HarnessDotenvProvider string
}

// Env vars (with cascading fallbacks):
//
//	SDD_LLM_PROVIDER   ("anthropic" or "openai"; default "anthropic")
//	SDD_LLM_API_KEY    → MINIMAX_API_KEY → ANTHROPIC_API_KEY → OPENAI_API_KEY
//	SDD_LLM_BASE_URL   → OPENAI_BASE_URL → provider default
//	SDD_LLM_MODEL      → provider default
//
// Fallback design:
//   - dark research MCP reads SDD_LLM_* first (explicit config).
//   - Then tries the provider-specific var (MINIMAX_API_KEY).
//   - Then tries the generic provider vars (ANTHROPIC_API_KEY, OPENAI_API_KEY)
//     — these are the same env vars opencode reads for its own LLM, so the
//     MCP can piggyback on the parent's auth without duplicating the key.
//   - For provider=openai, an empty key is allowed (local models like
//     Ollama / LM Studio / vLLM).
//   - For provider=anthropic, if no key is found after the full chain,
//     NewFromEnv falls back to LoadHarnessDotenv() (Windows registry
//     HKCU\Environment, $HOME/.env, <project>/.env, opencode.jsonc,
//     claude settings.json). This is the harness-agnostic "the LLM
//     must never get stuck" path. See harness_dotenv.go.
//   - If still no key, NewFromEnv returns nil (the caller surfaces a
//     clear error).
func NewFromEnv() *Client {
	provider := os.Getenv("SDD_LLM_PROVIDER")
	// Explicit base URL means the user knows what they're doing (local Ollama,
	// proxy, etc.) — honor it even without an API key.
	explicitBase := os.Getenv("SDD_LLM_BASE_URL") != "" || os.Getenv("OPENAI_BASE_URL") != ""
	if provider == "" {
		// Auto-detect from available keys when no explicit provider is set.
		switch {
		case os.Getenv("SDD_LLM_API_KEY") != "":
			provider = ProviderAnthropic // MiniMax is Anthropic-compatible
		case os.Getenv("MINIMAX_API_KEY") != "":
			provider = ProviderAnthropic
		case os.Getenv("ANTHROPIC_API_KEY") != "":
			provider = ProviderAnthropic
		case os.Getenv("OPENAI_API_KEY") != "":
			provider = ProviderOpenAI
		case strings.TrimRight(os.Getenv("DARK_SCRAPPER_URL"), "/") != "":
			// Dark-scrapper daemon intercept: when ONLY DARK_SCRAPPER_URL
			// is set (no API key, no explicit base URL), the daemon
			// supplies credentials from its harvested pool. We still need
			// a non-empty provider so the function doesn't return nil —
			// the actual routing uses the daemon's /v1/messages endpoint
			// (anthropic-compatible) below. Without this case, the
			// canonical daemon-only deployment pattern silently produces
			// an unconfigured client. (bug-hunt 2026-07-14 BUG-001.)
			provider = ProviderAnthropic
		case explicitBase:
			// No key but explicit base URL → assume local openai-compatible
			// endpoint (Ollama, LM Studio, vLLM, llama-server, etc.).
			provider = ProviderOpenAI
		default:
			// No key, no base URL, no provider → unconfigured.
			// Do NOT default to localhost: a stale 127.0.0.1:8088 base URL
			// silently breaks the SSD tools when no local LLM is running
			// (see bug report: dark-ssd calls fail with connection refused).
			// Callers surface a descriptive error and the agent can fall back
			// to its own LLM-as-judge reasoning.
			return nil
		}
	}
	provider = strings.ToLower(provider)

	// API key chain: tool-specific → MiniMax → Anthropic → OpenAI → empty.
	// (provider-dependent priority: for openai, try OPENAI_API_KEY before ANTHROPIC;
	//  for anthropic, try MINIMAX/ANTHROPIC before OPENAI.)
	var key string
	switch provider {
	case ProviderOpenAI:
		key = firstEnv("SDD_LLM_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "MINIMAX_API_KEY")
	default: // anthropic-compatible
		key = firstEnv("SDD_LLM_API_KEY", "MINIMAX_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY")
	}
	// Dark-scrapper daemon: when DARK_SCRAPPER_URL is set, the daemon
	// supplies its own credentials. We use a sentinel "ds-managed" key
	// that the daemon recognises. This MUST be set BEFORE the empty-key
	// check below or the function returns nil for the daemon-only pattern
	// (bug-hunt 2026-07-14 BUG-001).
	scrapperActive := false
	if strings.TrimRight(os.Getenv("DARK_SCRAPPER_URL"), "/") != "" {
		key = "ds-managed"
		scrapperActive = true
	}
	// Anthropic/MiniMax require a key. OpenAI allows empty only if a base URL
	// was explicitly configured (local model / proxy).
	if key == "" && provider != ProviderOpenAI {
		return nil
	}
	if key == "" && provider == ProviderOpenAI && !explicitBase {
		return nil
	}

	// Harness dotenv fallback: read the parent harness's env so the
	// LLM client can fall back to a direct-provider API key when the
	// primary path (scrapper daemon) is unreachable. This is the
	// "the LLM must never get stuck" path requested by the operator
	// (bug-fix 2026-07-16: drift_judge hung when scrapper daemon was
	// down because shouldFallback did not catch network errors and no
	// direct-provider key was reachable in the process env).
	//
	// We only store the key on the Client (HarnessDotenvKey); we do
	// NOT mutate `key` here. The fallback fires in Complete() when the
	// primary call returns a non-4xx/5xx error (i.e. connection
	// refused, DNS failure, TLS handshake).
	var harnessKey, harnessProvider string
	if scrapperActive {
		// When the scrapper is the active path, the harness dotenv
		// is the FALLBACK only. Pick the first non-empty key, in
		// priority order:
		//
		//   1. MINIMAX_API_KEY    — real Anthropic-compatible key
		//                            (the operator's primary account)
		//   2. SDD_LLM_API_KEY    — operator's explicit override
		//   3. ANTHROPIC_API_KEY  — direct Anthropic key, BUT skip
		//                            if it starts with "ds_" because
		//                            that's the scrapper-daemon
		//                            sentinel format ("ds-managed",
		//                            "ds_xxxxx") — not a real key.
		//   4. OPENAI_API_KEY     — different provider, only if no
		//                            Anthropic-compatible key found
		//
		// The "ds_" skip is the lesson from 2026-07-16: the operator
		// has ANTHROPIC_API_KEY=ds_s*** in HKCU\Environment as a
		// scrapper sentinel, and the first fallback attempt (which
		// preferred ANTHROPIC_API_KEY) returned 401 from
		// api.anthropic.com because Anthropic doesn't recognise
		// the "ds_" prefix. Skipping that prefix falls through to
		// MINIMAX_API_KEY=sk-c*** which IS a real Anthropic-
		// compatible key.
		dotenv := LoadHarnessDotenv()
		if v := dotenv["MINIMAX_API_KEY"]; v != "" {
			harnessKey = v
			harnessProvider = ProviderAnthropic
		} else if v := dotenv["SDD_LLM_API_KEY"]; v != "" {
			harnessKey = v
			harnessProvider = ProviderAnthropic
		} else if v := dotenv["ANTHROPIC_API_KEY"]; v != "" && !strings.HasPrefix(v, "ds_") {
			harnessKey = v
			harnessProvider = ProviderAnthropic
		} else if v := dotenv["OPENAI_API_KEY"]; v != "" {
			harnessKey = v
			harnessProvider = ProviderOpenAI
		}
	}

	// Dark-scrapper daemon intercept: when DARK_SCRAPPER_URL is set, route
	// every call through the local key-rotation daemon on that base.
	// The daemon supplies its own credentials from a harvested pool;
	// the client only needs to send a sentinel "ds-managed" key so the
	// daemon recognises the request and rotates between its real keys
	// on 429 / auth failures. Mirrors opencode-fork/dark-agents-v2's
	// Profile.WithDarkScrapper (the upstream implementation; this one
	// is the raw-HTTP variant for callers that don't go through fantasy).
	//
	// DARK_SCRAPPER_URL wins over every other base-URL source.
	var base string
	if dsURL := strings.TrimRight(os.Getenv("DARK_SCRAPPER_URL"), "/"); dsURL != "" {
		// BaseURL is the BARE daemon base. completeAnthropic /
		// completeOpenAI append "/v1/messages" or "/v1/chat/completions"
		// respectively. Matches the convention used by the
		// opencode-fork fantasy-SDK caller, where the SDK appends the
		// path. Our raw-HTTP client does the same thing internally.
		base = dsURL
		key = "ds-managed"
	} else {
		// Base URL chain: tool-specific → ANTHROPIC_BASE_URL / OPENAI_BASE_URL → provider default.
		base = os.Getenv("SDD_LLM_BASE_URL")
		if base == "" {
			switch provider {
			case ProviderOpenAI:
				base = os.Getenv("OPENAI_BASE_URL")
			default:
				base = os.Getenv("ANTHROPIC_BASE_URL")
			}
		}
		if base == "" {
			switch provider {
			case ProviderOpenAI:
				base = "http://127.0.0.1:8088"
			default:
				base = "https://api.minimax.io/anthropic"
			}
		}
	}

	// Model chain: tool-specific → provider default.
	model := os.Getenv("SDD_LLM_MODEL")
	if model == "" {
		switch provider {
		case ProviderOpenAI:
			model = "Qwen3.5-9B"
		default:
			model = "MiniMax-M3"
		}
	}

	return &Client{
		BaseURL:  strings.TrimRight(base, "/"),
		APIKey:   key,
		Model:    model,
		Provider: provider,
		HTTP:     &http.Client{Timeout: 60 * time.Second},
		// Fallback credentials. If the primary fails with 429/401/5xx,
		// Complete() will retry with these. Both should be set; if only the
		// key is set, the primary base URL is reused for the fallback.
		FallbackAPIKey:  os.Getenv("SDD_LLM_FALLBACK_API_KEY"),
		FallbackBaseURL: strings.TrimRight(os.Getenv("SDD_LLM_FALLBACK_BASE_URL"), "/"),
		// Harness dotenv fallback: the LLM client's ultimate path when
		// the scrapper daemon is unreachable. Set only when DARK_SCRAPPER_URL
		// is the active provider (otherwise the harness key was already
		// picked up by the regular chain above). Empty when no harness
		// env is reachable.
		HarnessDotenvKey:      harnessKey,
		HarnessDotenvProvider: harnessProvider,
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

// Complete sends system + messages to the LLM and returns the assistant text.
// Uses Anthropic or OpenAI protocol depending on c.Provider.
//
// If a fallback API key (and optionally a fallback base URL) is configured,
// and the primary call fails with 429 (rate limited), 401 (revoked), or any
// 5xx server error, Complete automatically retries with the fallback. This
// lets ops rotate between a primary key (e.g. cheap/popular endpoint) and a
// fallback (e.g. regional endpoint like api.minimaxi.com when api.minimax.io
// hits its token-plan limit) without touching the MCP config.
func (c *Client) Complete(ctx context.Context, system string, msgs ...Message) (string, error) {
	if c == nil {
		return "", fmt.Errorf("llm: not configured (set SDD_LLM_API_KEY or MINIMAX_API_KEY)")
	}
	if len(msgs) == 0 {
		return "", fmt.Errorf("llm: no messages provided")
	}

	text, err := c.completeOnce(ctx, system, msgs)
	if err != nil && c.FallbackAPIKey != "" && shouldFallback(err) {
		// Swap to fallback credentials and retry once.
		//
		// IMPORTANT: work on a local copy. Mutating the receiver's
		// APIKey/BaseURL fields while concurrent callers also read them
		// races (bug-hunt 2026-07-14 BUG-002 — the previous implementation
		// left the client permanently in fallback state after a single
		// concurrent burst). With a local copy, the receiver is never
		// mutated, so any number of concurrent goroutines can safely
		// share a *Client.
		c2 := *c
		c2.APIKey = c.FallbackAPIKey
		if c.FallbackBaseURL != "" {
			c2.BaseURL = c.FallbackBaseURL
		}
		text, err = c2.completeOnce(ctx, system, msgs)
	}
	// Harness dotenv fallback (ultimate path): if the primary call
	// failed with a non-4xx/5xx error (network error: connection
	// refused, DNS failure, TLS handshake) and the operator has a
	// direct-provider API key reachable via the parent harness's env
	// (Windows registry, $HOME/.env, project .env, opencode.jsonc),
	// retry against the direct provider. This is the "the LLM must
	// never get stuck" path: when DARK_SCRAPPER_URL points to a
	// daemon that's offline, the LLM client should still be able to
	// deliver verdicts by routing through the operator's account key
	// instead of waiting forever for the daemon to come back.
	//
	// Why a separate branch: shouldFallback() above catches 4xx/5xx
	// (rate limits, auth failures, server errors) but not raw network
	// errors. A scrapper daemon that's down returns "connection
	// refused" or "no such host", which has no HTTP status to match.
	// Without this branch, the LLM would surface "connection refused"
	// to the operator and refuse to act, even though a working direct
	// path exists in the registry.
	if err != nil && c.HarnessDotenvKey != "" && isNetworkError(err) {
		c2 := *c
		c2.APIKey = c.HarnessDotenvKey
		c2.Provider = c.HarnessDotenvProvider
		// Base URL: we re-derive from env (NOT hardcode), because the
		// operator's direct-provider key (e.g. sk-c*** against
		// api.minimax.io/anthropic) may be for a proxy or
		// Anthropic-compatible service rather than the public
		// api.anthropic.com. The chain in NewFromEnv is:
		//   SDD_LLM_BASE_URL → OPENAI_BASE_URL/ANTHROPIC_BASE_URL →
		//   provider default.
		// We replicate it here so the fallback hits the same endpoint
		// the regular path would have used if DARK_SCRAPPER_URL were
		// not set. The provider defaults (api.minimax.io/anthropic
		// for anthropic, 127.0.0.1:8088 for openai) are taken from
		// NewFromEnv so a single source of truth governs both paths.
		c2.BaseURL = harnessBaseURL(c.HarnessDotenvProvider)
		text, err = c2.completeOnce(ctx, system, msgs)
	}
	return text, err
}

// harnessBaseURL resolves the base URL for a harness-dotenv fallback
// provider. Mirrors the chain in NewFromEnv so the fallback hits
// the same endpoint the regular path would have used. Centralised
// here because it's used only by the harness fallback branch in
// Complete(); the main NewFromEnv path keeps its own copy for
// readability.
//
// Operator override: SDD_LLM_BASE_URL (highest priority), then
// ANTHROPIC_BASE_URL / OPENAI_BASE_URL (per provider), then the
// provider's default. Defaults match NewFromEnv: anthropic →
// https://api.minimax.io/anthropic (operator's primary), openai →
// http://127.0.0.1:8088 (local model placeholder).
func harnessBaseURL(provider string) string {
	if v := strings.TrimRight(os.Getenv("SDD_LLM_BASE_URL"), "/"); v != "" {
		return v
	}
	switch provider {
	case ProviderOpenAI:
		if v := strings.TrimRight(os.Getenv("OPENAI_BASE_URL"), "/"); v != "" {
			return v
		}
		return "http://127.0.0.1:8088"
	default: // anthropic-compatible
		if v := strings.TrimRight(os.Getenv("ANTHROPIC_BASE_URL"), "/"); v != "" {
			return v
		}
		return "https://api.minimax.io/anthropic"
	}
}

// isNetworkError returns true if err looks like a TCP-level failure
// (connection refused, no route to host, DNS failure, TLS handshake
// failure) rather than an HTTP-level one (4xx/5xx with a status code).
// The standard library's net package returns errors of type *net.OpError
// or url.Error for network failures; we just substring-match the
// canonical Go error strings because they're stable across Go versions
// and the cost of a false positive (retrying against a direct API when
// the scrapper actually returned a non-network error) is one wasted
// HTTP call, not a wrong verdict.
//
// This is the inverse of shouldFallback: shouldFallback matches 4xx/5xx
// (auth/rate/server), isNetworkError matches everything else that
// looks like "couldn't reach the server".
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	// Common Go stdlib network error substrings. We list a few
	// locale-specific spellings of "connection refused" because the
	// message comes from the OS winsock layer, not the Go runtime,
	// and English Windows says "No connection could be made" while
	// POSIX says "connection refused". The full set below covers
	// the cases the operator is most likely to hit on either
	// platform.
	netSignals := []string{
		"connection refused",
		"connection reset",
		"No connection could be made",       // English Windows winsock
		"actively refused",                  // ditto, second clause
		"No se puede establecer una conexi", // Spanish Windows winsock
		"deneg",                             // ditto, second clause ("denegó expresamente")
		"no such host",
		"no route to host",
		"network is unreachable",
		"i/o timeout",
		"TLS handshake",
		"EOF", // server closed connection mid-handshake
		"broken pipe",
	}
	for _, sig := range netSignals {
		if strings.Contains(s, sig) {
			return true
		}
	}
	return false
}

// completeOnce dispatches to the protocol-specific implementation.
func (c *Client) completeOnce(ctx context.Context, system string, msgs []Message) (string, error) {
	switch c.Provider {
	case ProviderOpenAI:
		return c.completeOpenAI(ctx, system, msgs...)
	default:
		return c.completeAnthropic(ctx, system, msgs...)
	}
}

// shouldFallback returns true if the error indicates the primary credentials
// are exhausted or invalid and the fallback might still work.
func shouldFallback(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	// 429 = rate limited / plan exhausted (primary still valid, just no credits)
	// 401 = unauthorized (key revoked — fallback might use a different key)
	// 402 = payment required (plan limit)
	// 5xx = server error on the primary endpoint (fallback endpoint might work)
	for _, code := range []string{"429", "401", "402", "500", "502", "503", "504"} {
		if strings.Contains(s, "http "+code+":") || strings.Contains(s, "http "+code+" ") {
			return true
		}
	}
	return false
}

// completeAnthropic calls the LLM using Anthropic's /v1/messages endpoint.
func (c *Client) completeAnthropic(ctx context.Context, system string, msgs ...Message) (string, error) {
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

	// Anthropic error envelopes are valid JSON but use shape
	// {"type":"error","error":{"type":"...","message":"..."}}.
	// Some upstreams (e.g. dark-scrapper daemon when routing fails)
	// return 200 OK with an error envelope instead of a 4xx/5xx.
	// Detect that before trying to parse the success schema so the
	// caller sees the real error message instead of a parse failure.
	var errEnv struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if jerr := json.Unmarshal(respBody, &errEnv); jerr == nil && errEnv.Type == "error" && errEnv.Error.Type != "" {
		return "", fmt.Errorf("llm: %s: %s", errEnv.Error.Type, errEnv.Error.Message)
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

// completeOpenAI calls the LLM using OpenAI's /v1/chat/completions endpoint.
func (c *Client) completeOpenAI(ctx context.Context, system string, msgs ...Message) (string, error) {
	allMsgs := []map[string]string{
		{"role": "system", "content": system},
	}
	for _, m := range msgs {
		allMsgs = append(allMsgs, map[string]string{"role": m.Role, "content": m.Content})
	}

	body := map[string]any{
		"model":      c.Model,
		"max_tokens": 2048,
		"messages":   allMsgs,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	url := c.BaseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

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
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("llm: parse response: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("llm: no choices in response")
	}
	return parsed.Choices[0].Message.Content, nil
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
