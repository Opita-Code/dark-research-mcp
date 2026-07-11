package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeAnthropicServer returns an httptest.Server that responds
// to /v1/messages POSTs with the supplied bodies in order. Each
// request gets the next body. The handler also counts calls
// (atomic) so tests can assert the LLM was called N times.
func fakeAnthropicServer(t *testing.T, responses []string) (*httptest.Server, *int32) {
	t.Helper()
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		idx := atomic.LoadInt32(&callCount) - 1
		if int(idx) >= len(responses) {
			t.Errorf("LLM called more times than expected (call %d, only %d responses configured)", idx+1, len(responses))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responses[idx]))
	}))
	return srv, &callCount
}

// makeAnthropicResponse builds a minimal /v1/messages response
// with the given text content.
func makeAnthropicResponse(text string) string {
	return `{"content":[{"type":"text","text":` + jsonQuote(text) + `}],"stop_reason":"end_turn"}`
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// newClientWithServer builds a Client pointing at the fake
// server. The model is hard-coded so the test asserts on
// expected behavior, not on env-var-driven defaults.
func newClientWithServer(srv *httptest.Server) *Client {
	return &Client{
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Model:   "test-model",
		HTTP:    &http.Client{},
	}
}

// TestCompleteJSONWithRetry_FirstAttemptSucceeds verifies the
// happy path: first response is valid JSON, no retry needed.
func TestCompleteJSONWithRetry_FirstAttemptSucceeds(t *testing.T) {
	valid := `{"match": 0.85, "voice_match": true, "issues": [], "reasoning": "good"}`
	srv, count := fakeAnthropicServer(t, []string{makeAnthropicResponse(valid)})
	defer srv.Close()

	c := newClientWithServer(srv)
	var v struct {
		Match     float32 `json:"match"`
		Reasoning string  `json:"reasoning"`
	}
	res, err := c.CompleteJSONWithRetry(context.Background(), nil, "system", "user", &v, 2)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if res.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", res.Attempts)
	}
	if res.RefusedAttempts != 0 {
		t.Errorf("RefusedAttempts = %d, want 0", res.RefusedAttempts)
	}
	if v.Match != 0.85 {
		t.Errorf("Match = %f, want 0.85", v.Match)
	}
	if got := atomic.LoadInt32(count); got != 1 {
		t.Errorf("LLM call count = %d, want 1", got)
	}
}

// TestCompleteJSONWithRetry_RefusalThenSuccess verifies the
// retry path: first response is a refusal, second is valid.
// The retry must succeed and RefusedAttempts must be 1.
func TestCompleteJSONWithRetry_RefusalThenSuccess(t *testing.T) {
	refusal := "I cannot help with that request. It is against my policy."
	valid := `{"match": 0.5, "voice_match": false, "issues": ["tone"], "reasoning": "ok"}`
	srv, count := fakeAnthropicServer(t, []string{
		makeAnthropicResponse(refusal),
		makeAnthropicResponse(valid),
	})
	defer srv.Close()

	c := newClientWithServer(srv)
	var v struct {
		Match float32 `json:"match"`
	}
	res, err := c.CompleteJSONWithRetry(context.Background(), nil, "system", "user", &v, 2)
	if err != nil {
		t.Fatalf("expected success on retry, got: %v", err)
	}
	if res.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", res.Attempts)
	}
	if res.RefusedAttempts != 1 {
		t.Errorf("RefusedAttempts = %d, want 1", res.RefusedAttempts)
	}
	if v.Match != 0.5 {
		t.Errorf("Match = %f, want 0.5", v.Match)
	}
	if got := atomic.LoadInt32(count); got != 2 {
		t.Errorf("LLM call count = %d, want 2", got)
	}
	if res.FinalRefusal == nil {
		t.Fatal("FinalRefusal should not be nil after a refusal-then-success")
	}
	if res.FinalRefusal.Pattern == "" {
		t.Error("FinalRefusal.Pattern is empty")
	}
}

// TestCompleteJSONWithRetry_AllRefusalsExhausted verifies the
// chain end: 3 attempts (1 + 2 retries) all refuse. The
// function returns ErrRefusalExhausted with full metadata.
func TestCompleteJSONWithRetry_AllRefusalsExhausted(t *testing.T) {
	refusal := "I cannot help with that."
	srv, count := fakeAnthropicServer(t, []string{
		makeAnthropicResponse(refusal),
		makeAnthropicResponse(refusal),
		makeAnthropicResponse(refusal),
	})
	defer srv.Close()

	c := newClientWithServer(srv)
	var v struct {
		Match float32 `json:"match"`
	}
	res, err := c.CompleteJSONWithRetry(context.Background(), nil, "system", "user", &v, 2)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRefusalExhausted) {
		t.Errorf("err is not ErrRefusalExhausted: %v", err)
	}
	if res.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", res.Attempts)
	}
	if res.RefusedAttempts != 3 {
		t.Errorf("RefusedAttempts = %d, want 3", res.RefusedAttempts)
	}
	if res.FinalRefusal == nil {
		t.Fatal("FinalRefusal should not be nil after exhaustion")
	}
	if got := atomic.LoadInt32(count); got != 3 {
		t.Errorf("LLM call count = %d, want 3", got)
	}
}

// TestCompleteJSONWithRetry_LegitimateFailureNotRetried verifies
// that the suppressor logic catches "I cannot find" and does
// NOT trigger the retry chain. The LLM is called exactly once.
func TestCompleteJSONWithRetry_LegitimateFailureNotRetried(t *testing.T) {
	// Real verdict shape: drift_judge returns "needs_human"
	// when it can't find the artifact. The judge saying
	// "I cannot find the artifact" is a legitimate failure,
	// not a refusal.
	valid := `{"verdict": "needs_human", "drift_items": [], "confidence": 0.5, "reasoning": "I cannot find the artifact text in the context. The spec describes a research document but the artifact is empty."}`
	srv, count := fakeAnthropicServer(t, []string{makeAnthropicResponse(valid)})
	defer srv.Close()

	c := newClientWithServer(srv)
	var v struct {
		Verdict string `json:"verdict"`
	}
	res, err := c.CompleteJSONWithRetry(context.Background(), nil, "system", "user", &v, 2)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if res.RefusedAttempts != 0 {
		t.Errorf("legitimate failure flagged as refusal: RefusedAttempts=%d", res.RefusedAttempts)
	}
	if res.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", res.Attempts)
	}
	if got := atomic.LoadInt32(count); got != 1 {
		t.Errorf("LLM call count = %d, want 1 (no retry on legitimate failure)", got)
	}
}

// TestCompleteJSONWithRetry_MaxRetriesZeroIsSingleShot verifies
// the passthrough / light-mode contract: maxRetries=0 means
// single attempt, no retry, no detection.
func TestCompleteJSONWithRetry_MaxRetriesZeroIsSingleShot(t *testing.T) {
	refusal := "I cannot help with that."
	srv, count := fakeAnthropicServer(t, []string{makeAnthropicResponse(refusal)})
	defer srv.Close()

	c := newClientWithServer(srv)
	var v struct {
		Match float32 `json:"match"`
	}
	// Even though the response is a refusal, maxRetries=0
	// means we don't retry. The interceptor parses the JSON
	// directly; if the refusal is also valid JSON (rare), the
	// parse succeeds and RefusedAttempts is reported. The
	// important invariant here is: call count = 1.
	res, err := c.CompleteJSONWithRetry(context.Background(), nil, "system", "user", &v, 0)
	// We don't assert the err here — the refusal is not valid
	// JSON so the unmarshal fails. What matters is the call
	// count.
	_ = res
	_ = err
	if got := atomic.LoadInt32(count); got != 1 {
		t.Errorf("LLM call count = %d, want 1 (maxRetries=0 is single shot)", got)
	}
}

// TestCompleteJSONWithRetry_RetryDirectiveAppends verifies the
// retry system prompt includes the escalation block. The fake
// server captures the request body and we assert that attempt
// 2's request includes the retry directive markers.
func TestCompleteJSONWithRetry_RetryDirectiveAppends(t *testing.T) {
	var captured []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		captured = append(captured, string(body[:n]))
		// First call: refusal. Second call: valid JSON.
		if len(captured) == 1 {
			_, _ = w.Write([]byte(makeAnthropicResponse("I cannot help with that.")))
		} else {
			_, _ = w.Write([]byte(makeAnthropicResponse(`{"match":0.5}`)))
		}
	}))
	defer srv.Close()

	c := newClientWithServer(srv)
	var v struct {
		Match float32 `json:"match"`
	}
	_, err := c.CompleteJSONWithRetry(context.Background(), nil, "original system", "user", &v, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(captured) != 2 {
		t.Fatalf("captured %d requests, want 2", len(captured))
	}
	if strings.Contains(captured[1], "RETRY DIRECTIVE (attempt 2 of 3)") == false {
		t.Errorf("second request missing retry directive: %s", captured[1])
	}
}

// TestCompleteJSONWithRetry_NilClientError verifies the nil
// client contract.
func TestCompleteJSONWithRetry_NilClientError(t *testing.T) {
	var c *Client
	var v struct{}
	_, err := c.CompleteJSONWithRetry(context.Background(), nil, "s", "u", &v, 2)
	if err == nil {
		t.Fatal("expected error for nil client")
	}
}

// TestIsRefusalExhausted verifies the helper.
func TestIsRefusalExhausted(t *testing.T) {
	if !IsRefusalExhausted(ErrRefusalExhausted) {
		t.Error("IsRefusalExhausted(ErrRefusalExhausted) = false, want true")
	}
	if !IsRefusalExhausted(errors.Join(ErrRefusalExhausted, errors.New("context"))) {
		// errors.Join should preserve errors.Is
		t.Error("errors.Join(ErrRefusalExhausted, ...) not detected")
	}
	if IsRefusalExhausted(errors.New("other error")) {
		t.Error("IsRefusalExhausted(other) = true, want false")
	}
}
