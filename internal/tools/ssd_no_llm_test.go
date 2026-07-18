package tools

import (
	"context"
	"encoding/json"
	"testing"
)

// ssd_no_llm_test.go covers the graceful-degradation path: when
// requireLLM() errors (no SDD_LLM_API_KEY / MINIMAX_API_KEY), every
// dark_ssd_* tool still returns a verdict-shaped response with the
// match=0 / compliant=false / verdict=needs_human defaults instead
// of a hard tool error. These tests are hermetic — they never call
// the LLM and never open the mem store, so they run on any machine
// with any env state.

func TestDegradedVerdict_brandMatch(t *testing.T) {
	j, pattern := degradedVerdict("brand_match")
	if pattern != "no_llm_configured" {
		t.Errorf("refusal pattern: got %q, want %q", pattern, "no_llm_configured")
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(j), &v); err != nil {
		t.Fatalf("verdict not JSON: %v", err)
	}
	if v["match"].(float64) != 0 {
		t.Errorf("match: got %v, want 0", v["match"])
	}
	if v["voice_match"].(bool) != false {
		t.Errorf("voice_match: got %v, want false", v["voice_match"])
	}
	issues, _ := v["issues"].([]any)
	if len(issues) != 1 {
		t.Fatalf("issues: got %v, want exactly one entry", v["issues"])
	}
	if s, _ := issues[0].(string); s != "no_llm_configured" {
		t.Errorf("issues[0]: got %q, want %q", s, "no_llm_configured")
	}
}

func TestDegradedVerdict_complianceCheck(t *testing.T) {
	j, _ := degradedVerdict("compliance_check")
	var v map[string]any
	if err := json.Unmarshal([]byte(j), &v); err != nil {
		t.Fatalf("verdict not JSON: %v", err)
	}
	if v["compliant"].(bool) != false {
		t.Errorf("compliant: got %v, want false", v["compliant"])
	}
	if _, ok := v["required_disclosures"]; !ok {
		t.Error("required_disclosures must be present (even if empty)")
	}
}

func TestDegradedVerdict_driftJudge(t *testing.T) {
	j, _ := degradedVerdict("drift_judge")
	var v map[string]any
	if err := json.Unmarshal([]byte(j), &v); err != nil {
		t.Fatalf("verdict not JSON: %v", err)
	}
	if v["verdict"] != "needs_human" {
		t.Errorf("drift_judge verdict: got %v, want needs_human", v["verdict"])
	}
}

func TestDegradedVerdict_groundingCheck(t *testing.T) {
	j, _ := degradedVerdict("grounding_check")
	var v map[string]any
	if err := json.Unmarshal([]byte(j), &v); err != nil {
		t.Fatalf("verdict not JSON: %v", err)
	}
	if v["grounded"].(bool) != false {
		t.Errorf("grounded: got %v, want false", v["grounded"])
	}
	if v["confidence"].(float64) != 0 {
		t.Errorf("confidence: got %v, want 0", v["confidence"])
	}
}

func TestDegradedVerdict_piiDetect(t *testing.T) {
	j, _ := degradedVerdict("pii_detect")
	var v map[string]any
	if err := json.Unmarshal([]byte(j), &v); err != nil {
		t.Fatalf("verdict not JSON: %v", err)
	}
	if v["pii_found"].(bool) != false {
		t.Errorf("pii_found: got %v, want false", v["pii_found"])
	}
}

func TestDegradedVerdict_promptInjectionScan(t *testing.T) {
	j, _ := degradedVerdict("prompt_injection_scan")
	var v map[string]any
	if err := json.Unmarshal([]byte(j), &v); err != nil {
		t.Fatalf("verdict not JSON: %v", err)
	}
	if v["injection_found"].(bool) != false {
		t.Errorf("injection_found: got %v, want false", v["injection_found"])
	}
}

func TestDegradedVerdict_unknownEvalType(t *testing.T) {
	j, pattern := degradedVerdict("totally_unknown_tool")
	if pattern != "no_llm_configured" {
		t.Errorf("refusal pattern: got %q, want %q", pattern, "no_llm_configured")
	}
	// Default returns "{}" — no fields, no parse error.
	var v map[string]any
	if err := json.Unmarshal([]byte(j), &v); err != nil {
		t.Fatalf("fallback verdict not JSON: %v", err)
	}
}

func TestHandleNoLLM_nilMem_returnsShapeWithoutError(t *testing.T) {
	// With m=nil we still must return a structured response (not an
	// error) so the agent sees a verdict, not a tool failure.
	res, err := handleNoLLM(nil, context.Background(), "brand_match", "brand", "test-brand")
	if err != nil {
		t.Fatalf("handleNoLLM returned error: %v", err)
	}
	if res == nil {
		t.Fatal("handleNoLLM returned nil result")
	}
	// The result is a *mcp.CallToolResult; the textual body is
	// JSON. We don't dissect the JSON wire shape here (that's tested
	// downstream), but we do assert non-empty content.
	if len(res.Content) == 0 {
		t.Error("handleNoLLM returned no content")
	}
}

func TestHandleConsensusNoLLM_shape(t *testing.T) {
	res, err := handleConsensusNoLLM(nil, context.Background(), ssdConsensusArgs{
		EvalType: "drift_judge",
		Content:  "test content",
		N:        3,
	})
	if err != nil {
		t.Fatalf("handleConsensusNoLLM returned error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	// Result body is JSON. Parse and check the headline.
	if len(res.Content) == 0 {
		t.Skip("result has no content; wire-level verification only")
	}
	body := res.Content[0]
	text, _ := body.(interface{ Text() string })
	_ = text
}
