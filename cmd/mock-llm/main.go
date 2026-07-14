// Command mock-llm is a minimal Anthropic-compatible mock used to
// validate vibe-studio's end-to-end flow without depending on a live
// LLM. It mirrors the parts of the Anthropic /v1/messages contract
// that dark-research-mcp's internal/llm/client.go consumes:
//
//   - POST /v1/messages with x-api-key (any non-empty value)
//   - Responds with HTTP 200 + JSON body of the form
//     {"content":[{"type":"text","text":"..."}],"stop_reason":"end_turn",
//      "model":"...","usage":{...}}
//
// All requests are logged to stderr with method, path, model, and
// max_tokens (extracted from the body). The reply text is a
// deterministic, well-formed JSON object that vibe-studio's spec 97
// audit task can parse back to confirm the round-trip works.
//
// Usage:
//   go run ./cmd/mock-llm                 # listens on :9000
//   $env:SDD_LLM_BASE_URL = "http://127.0.0.1:9000"
//   $env:SDD_LLM_PROVIDER = "openai"     # so client appends /v1/chat/completions
//   ./vibe-studio-prototype.exe
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type anthropicReq struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	System    string         `json:"system"`
	Messages  []anthropicMsg `json:"messages"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResp struct {
	ID         string             `json:"id"`
	Type       string             `json:"type"`
	Role       string             `json:"role"`
	Content    []anthropicContent `json:"content"`
	Model      string             `json:"model"`
	StopReason string             `json:"stop_reason"`
	Usage      anthropicUsage     `json:"usage"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func main() {
	addr := os.Getenv("MOCK_LLM_ADDR")
	if addr == "" {
		addr = ":9000"
	}
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("mock-llm: starting on %s", addr)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", handleMessages)
	mux.HandleFunc("/v1/chat/completions", handleChatCompletions)
	mux.HandleFunc("/admin/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"uptime_s":1,"provider":"mock-llm"}`)
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

func handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	var req anthropicReq
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("mock-llm: bad json: %v body=%s", err, string(body))
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad json"}}`, http.StatusBadRequest)
		return
	}

	log.Printf("mock-llm: POST /v1/messages model=%q max_tokens=%d system_chars=%d messages=%d",
		req.Model, req.MaxTokens, len(req.System), len(req.Messages))

	// Build a deterministic reply: echo model + system + last user msg
	// wrapped in a JSON object so the vibe-studio prototype's spec_97
	// audit task can parse real structured content back.
	lastUser := ""
	for _, m := range req.Messages {
		if m.Role == "user" {
			lastUser = m.Content
		}
	}
	reply := map[string]any{
		"status":          "ok",
		"mock":            true,
		"model_echoed":    req.Model,
		"system_chars":    len(req.System),
		"last_user_chars": len(lastUser),
		"audit_summary": map[string]any{
			"shared_state_files": []string{
				"dark_mem.go", "dark_mem_semantic.go", "dark_mem_status.go",
				"dark_research.go", "matrix.go", "ssd.go", "vibeflow_data.go",
				"export_diff.go",
			},
			"singleton_leftovers": 0,
			"shared_state_count":  34,
			"verdict":              "P-1 refactor complete; tools.All(cfg, ss) works; spec 97 hardened",
			"note":                 "this response came from mock-llm (port 9000); replace SDD_LLM_BASE_URL with the real provider to swap",
		},
		"ts": time.Now().UTC().Format(time.RFC3339),
	}
	replyJSON, _ := json.MarshalIndent(reply, "", "  ")

	resp := anthropicResp{
		ID:         "msg_mock_" + fmt.Sprintf("%d", time.Now().UnixNano()),
		Type:       "message",
		Role:       "assistant",
		Content:    []anthropicContent{{Type: "text", Text: string(replyJSON)}},
		Model:      req.Model,
		StopReason: "end_turn",
		Usage: anthropicUsage{
			InputTokens:  len(req.System) / 4,
			OutputTokens: len(replyJSON) / 4,
		},
	}
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	// Minimal OpenAI-shape echo. Same content as /v1/messages.
	log.Printf("mock-llm: POST /v1/chat/completions body_bytes=%d", len(body))

	reply := map[string]any{
		"id":      "chatcmpl-mock",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "mock-llm",
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": `{"status":"ok","mock":true,"via":"openai-compat"}`,
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":     len(body) / 4,
			"completion_tokens": 16,
			"total_tokens":      len(body)/4 + 16,
		},
	}
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)

	_ = strings.TrimSpace // keep import live for later debug helpers
}
