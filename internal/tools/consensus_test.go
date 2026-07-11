package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dark-agents/research-mcp/internal/llm"
	"github.com/dark-agents/research-mcp/internal/mem"
	"github.com/mark3labs/mcp-go/mcp"
)

// mockLLMServer returns an httptest.Server that responds to POST /v1/messages
// with the Anthropic-compatible envelope, returning each item from `responses`
// (in order, modulo wrap) as the assistant text. After responses are exhausted
// it cycles back to the first. This lets a single test drive N samples
// deterministically.
func mockLLMServer(t *testing.T, responses []map[string]any) *httptest.Server {
	t.Helper()
	var callCount int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if got := r.Header.Get("x-api-key"); got == "" {
			t.Error("missing x-api-key header")
		}
		idx := int(atomic.AddInt32(&callCount, 1)-1) % len(responses)
		envelope := map[string]any{
			"content":    []map[string]any{{"type": "text", "text": mustJSON(t, responses[idx])}},
			"stop_reason": "end_turn",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(envelope)
	}))
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// installMockLLM attaches a fresh *llm.Client pointed at srv to the
// package-level shared singleton. Returns the cleanup func.
//
// We reset sharedLLM (not just swap) because the consensus tool reads
// via requireLLM → getLLM, which returns sharedLLM as-is.
func installMockLLM(t *testing.T, srv *httptest.Server) func() {
	t.Helper()
	prev := sharedLLM
	sharedLLM = &llm.Client{
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Model:   "test-model",
		HTTP:    &http.Client{Timeout: 5 * time.Second},
	}
	return func() {
		sharedLLM = prev
		// Also reset sharedCache to keep test isolation airtight.
		sharedCache = nil
	}
}

// callConsensus invokes the consensus handler with the given args and
// returns the parsed JSON result.
func callConsensus(t *testing.T, args map[string]any) (map[string]any, error) {
	t.Helper()
	tool := ssdConsensusTool()
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "dark_ssd_consensus",
			Arguments: args,
		},
	}
	res, err := tool.Handler(context.Background(), req)
	if err != nil {
		return nil, err
	}
	if res == nil {
		t.Fatal("nil result")
	}
	tc := res.Content[0].(mcp.TextContent)
	var out map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &out); err != nil {
		t.Fatalf("result not JSON: %v\npayload: %s", err, tc.Text)
	}
	return out, nil
}

// TestConsensus_AllAgree is the happy path: N=3 samples all return
// compliant=true → mode is "compliant", agreement is "3/3".
func TestConsensus_AllAgree(t *testing.T) {
	allCompliant := []map[string]any{
		{"compliant": true, "confidence": 0.9, "reasoning": "looks good"},
		{"compliant": true, "confidence": 0.85, "reasoning": "ok"},
		{"compliant": true, "confidence": 0.95, "reasoning": "fine"},
	}
	srv := mockLLMServer(t, allCompliant)
	t.Cleanup(srv.Close)
	defer installMockLLM(t, srv)()

	// compliance_check needs a mem store to look up the rule.
	defer installSharedMem(t, freshMemStore(t))()
	// Register the EU rule so buildConsensusPrompt's GetComplianceRule succeeds.
	if err := shared.mem.SaveComplianceRule(context.Background(), &mem.ComplianceRule{
		Jurisdiction: "EU",
		Rules:        `{"disclosure_required": true}`,
	}); err != nil {
		t.Fatalf("SaveComplianceRule: %v", err)
	}

	out, err := callConsensus(t, map[string]any{
		"eval_type":   "compliance_check",
		"jurisdiction": "EU",
		"content":     "fake ad copy",
		"n":           3,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if got, _ := out["samples"].(float64); int(got) != 3 {
		t.Errorf("samples: got %v, want 3", out["samples"])
	}
	if got, _ := out["mode"].(string); got != "compliant" {
		t.Errorf("mode: got %q, want compliant", got)
	}
	if got, _ := out["agreement"].(string); got != "3/3" {
		t.Errorf("agreement: got %q, want 3/3", got)
	}
	// Confidence stats: avg=0.9, min=0.85, max=0.95
	if got, _ := out["confidence_avg"].(float64); got < 0.89 || got > 0.91 {
		t.Errorf("confidence_avg: got %v, want ~0.90", got)
	}
	if got, _ := out["confidence_min"].(float64); got != 0.85 {
		t.Errorf("confidence_min: got %v, want 0.85", got)
	}
	if got, _ := out["confidence_max"].(float64); got != 0.95 {
		t.Errorf("confidence_max: got %v, want 0.95", got)
	}
}

// TestConsensus_Majority drills the 2/3 case: 2 compliant, 1 non_compliant.
func TestConsensus_Majority(t *testing.T) {
	mixed := []map[string]any{
		{"compliant": true, "confidence": 0.8, "reasoning": "ok"},
		{"compliant": true, "confidence": 0.7, "reasoning": "ok"},
		{"compliant": false, "confidence": 0.6, "reasoning": "issue"},
	}
	srv := mockLLMServer(t, mixed)
	t.Cleanup(srv.Close)
	defer installMockLLM(t, srv)()

	defer installSharedMem(t, freshMemStore(t))()
	if err := shared.mem.SaveComplianceRule(context.Background(), &mem.ComplianceRule{
		Jurisdiction: "EU",
		Rules:        `{"disclosure_required": true}`,
	}); err != nil {
		t.Fatalf("SaveComplianceRule: %v", err)
	}

	out, err := callConsensus(t, map[string]any{
		"eval_type":    "compliance_check",
		"jurisdiction": "EU",
		"content":      "borderline ad",
		"n":            3,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if got, _ := out["mode"].(string); got != "compliant" {
		t.Errorf("mode: got %q, want compliant (2 of 3)", got)
	}
	if got, _ := out["mode_count"].(float64); int(got) != 2 {
		t.Errorf("mode_count: got %v, want 2", out["mode_count"])
	}
	if got, _ := out["agreement"].(string); got != "2/3" {
		t.Errorf("agreement: got %q, want 2/3", got)
	}
	// Headline counts should reflect both buckets.
	hc, _ := out["headline_counts"].(map[string]any)
	if hc["compliant"] != float64(2) {
		t.Errorf("headline_counts[compliant]: got %v, want 2", hc["compliant"])
	}
	if hc["non_compliant"] != float64(1) {
		t.Errorf("headline_counts[non_compliant]: got %v, want 1", hc["non_compliant"])
	}
}

// TestConsensus_NDefaultsTo3 confirms that omitting `n` defaults to 3.
func TestConsensus_NDefaultsTo3(t *testing.T) {
	one := []map[string]any{
		{"compliant": true, "confidence": 0.9, "reasoning": "ok"},
	}
srv := mockLLMServer(t, one)
	t.Cleanup(srv.Close)
	defer installMockLLM(t, srv)()

	defer installSharedMem(t, freshMemStore(t))()
	if err := shared.mem.SaveComplianceRule(context.Background(), &mem.ComplianceRule{
		Jurisdiction: "EU",
		Rules:        `{"disclosure_required": true}`,
	}); err != nil {
		t.Fatalf("SaveComplianceRule: %v", err)
	}

	out, err := callConsensus(t, map[string]any{
		"eval_type":   "compliance_check",
		"jurisdiction": "EU",
		"content":     "x",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if got, _ := out["samples"].(float64); int(got) != 3 {
		t.Errorf("samples: got %v, want 3 (default N)", out["samples"])
	}
}

// TestConsensus_NCappedAt7 confirms that N>7 is capped to 7.
func TestConsensus_NCappedAt7(t *testing.T) {
	one := []map[string]any{
		{"compliant": true, "confidence": 0.9, "reasoning": "ok"},
	}
	srv := mockLLMServer(t, one)
	t.Cleanup(srv.Close)
	defer installMockLLM(t, srv)()

	defer installSharedMem(t, freshMemStore(t))()
	if err := shared.mem.SaveComplianceRule(context.Background(), &mem.ComplianceRule{
		Jurisdiction: "EU",
		Rules:        `{"disclosure_required": true}`,
	}); err != nil {
		t.Fatalf("SaveComplianceRule: %v", err)
	}

	out, err := callConsensus(t, map[string]any{
		"eval_type":    "compliance_check",
		"jurisdiction": "EU",
		"content":      "x",
		"n":            99,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if got, _ := out["samples"].(float64); int(got) != 7 {
		t.Errorf("samples: got %v, want 7 (cap)", out["samples"])
	}
}

// TestConsensus_DriftJudgePath exercises the drift_judge eval_type so we
// know the headline field is `verdict` (not `compliant`).
func TestConsensus_DriftJudgePath(t *testing.T) {
	samples := []map[string]any{
		{"verdict": "aligned", "confidence": 0.8, "reasoning": "ok"},
		{"verdict": "aligned", "confidence": 0.7, "reasoning": "ok"},
		{"verdict": "drift_detected", "confidence": 0.6, "reasoning": "mismatch"},
	}
	srv := mockLLMServer(t, samples)
	t.Cleanup(srv.Close)
	defer installMockLLM(t, srv)()

	out, err := callConsensus(t, map[string]any{
		"eval_type": "drift_judge",
		"content":   "spec vs artifact",
		"n":         3,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if got, _ := out["mode"].(string); got != "aligned" {
		t.Errorf("mode: got %q, want aligned", got)
	}
}

// TestConsensus_UnsupportedEvalType confirms we return a clear error
// when the eval_type is unknown — so the agent sees a stable contract.
func TestConsensus_UnsupportedEvalType(t *testing.T) {
	// No LLM call needed; buildConsensusPrompt short-circuits.
	// We must still install a mock LLM so requireLLM() succeeds.
	srv := mockLLMServer(t, nil)
	t.Cleanup(srv.Close)
	defer installMockLLM(t, srv)()

	_, err := callConsensus(t, map[string]any{
		"eval_type": "not_a_real_judge",
		"content":   "x",
	})
	if err == nil {
		t.Fatal("expected error for unsupported eval_type")
	}
	if !strings.Contains(err.Error(), "unsupported eval_type") {
		t.Errorf("error: got %q, want substring 'unsupported eval_type'", err.Error())
	}
}

// TestConsensus_BrandMatchMissingBrandID confirms that brand_match
// requires brand_id (otherwise we can't fetch the guide).
func TestConsensus_BrandMatchMissingBrandID(t *testing.T) {
	srv := mockLLMServer(t, nil)
	t.Cleanup(srv.Close)
	defer installMockLLM(t, srv)()

	_, err := callConsensus(t, map[string]any{
		"eval_type": "brand_match",
		"content":   "x",
	})
	if err == nil {
		t.Fatal("expected error when brand_id missing for brand_match")
	}
	if !strings.Contains(err.Error(), "brand_id required") {
		t.Errorf("error: got %q, want substring 'brand_id required'", err.Error())
	}
}

// TestConfidenceStats is a focused unit test for the math (avg/stddev/min/max).
// Catches off-by-one in stddev divisor and Newton-step quirks in sqrt32.
func TestConfidenceStats(t *testing.T) {
	cases := []struct {
		name             string
		xs               []float32
		wantAvg          float32
		wantMin, wantMax float32
		wantStddevApprox float32 // close to, not exact
	}{
		{"empty", nil, 0, 0, 0, 0},
		{"single", []float32{0.9}, 0.9, 0.9, 0.9, 0},
		{"three uniform", []float32{0.5, 0.5, 0.5}, 0.5, 0.5, 0.5, 0},
		{"three varied", []float32{0.8, 0.7, 0.6}, 0.7, 0.6, 0.8, 0.1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			avg, stddev, mn, mx := confidenceStats(tc.xs)
			if err := approx(avg, tc.wantAvg, 1e-3); err != nil {
				t.Errorf("avg: %v", err)
			}
			if mn != tc.wantMin {
				t.Errorf("min: got %v, want %v", mn, tc.wantMin)
			}
			if mx != tc.wantMax {
				t.Errorf("max: got %v, want %v", mx, tc.wantMax)
			}
			if err := approx(stddev, tc.wantStddevApprox, 1e-2); err != nil {
				t.Errorf("stddev: %v", err)
			}
		})
	}
}

// approx returns nil if |a-b| <= eps, else a descriptive error.
func approx(a, b, eps float32) error {
	d := a - b
	if d < 0 {
		d = -d
	}
	if d <= eps {
		return nil
	}
	return fmt.Errorf("got %v, want %v (eps %v)", a, b, eps)
}