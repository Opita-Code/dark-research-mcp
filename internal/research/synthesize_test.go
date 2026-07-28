// synthesize_test.go — v0.8.1 LLM synthesis tests.
package research

import (
	"context"
	"errors"
	"testing"
)

// mockLLMClient records calls and returns canned responses.
type mockLLMClient struct {
	summary    string
	err        error
	calls      int
	lastSystem string
	lastMsgs   []LLMMessage
}

func (m *mockLLMClient) Complete(ctx context.Context, system string, msgs ...LLMMessage) (string, error) {
	m.calls++
	m.lastSystem = system
	m.lastMsgs = msgs
	return m.summary, m.err
}

func TestSynthesize_SkipsWhenNoLLM(t *testing.T) {
	got, err := Synthesize(context.Background(), nil, "x", []Item{
		{Title: "a", Source: "osv"},
		{Title: "b", Source: "nvd"},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty summary, got %q", got)
	}
}

func TestSynthesize_SkipsWhenTooFewItems(t *testing.T) {
	llm := &mockLLMClient{summary: "should not happen"}
	got, err := Synthesize(context.Background(), llm, "x", []Item{
		{Title: "single", Source: "osv"},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty summary, got %q", got)
	}
	if llm.calls != 0 {
		t.Errorf("expected 0 LLM calls, got %d", llm.calls)
	}
}

func TestSynthesize_SkipsWhenOnlyOneBackend(t *testing.T) {
	llm := &mockLLMClient{summary: "should not happen"}
	got, err := Synthesize(context.Background(), llm, "x", []Item{
		{Title: "a", Source: "osv"},
		{Title: "b", Source: "osv"},
		{Title: "c", Source: "osv"},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty summary, got %q", got)
	}
	if llm.calls != 0 {
		t.Errorf("expected 0 LLM calls when only one source, got %d", llm.calls)
	}
}

func TestSynthesize_CallsWhenEnabled(t *testing.T) {
	llm := &mockLLMClient{summary: "Two findings (osv, nvd) confirm the xz backdoor vulnerability."}
	items := []Item{
		{Title: "xz backdoor", URL: "https://osv.dev/CVE-2024-3094", Source: "osv", Confidence: 0.95},
		{Title: "CVE-2024-3094", URL: "https://nvd.nist.gov/CVE-2024-3094", Source: "nvd", Confidence: 0.97},
	}

	got, err := Synthesize(context.Background(), llm, "CVE-2024-3094", items)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != llm.summary {
		t.Errorf("expected summary %q, got %q", llm.summary, got)
	}
	if llm.calls != 1 {
		t.Errorf("expected 1 LLM call, got %d", llm.calls)
	}
	// Verify the prompt contains both URLs so the LLM has the data.
	combined := llm.lastSystem
	for _, m := range llm.lastMsgs {
		combined += m.Content
	}
	if !contains(combined, "osv.dev") || !contains(combined, "nvd.nist.gov") {
		t.Errorf("prompt missing one of the URLs: %s", combined)
	}
}

func TestSynthesize_LLMErrorPropagates(t *testing.T) {
	llm := &mockLLMClient{err: errors.New("rate limited")}
	_, err := Synthesize(context.Background(), llm, "x", []Item{
		{Title: "a", Source: "osv"},
		{Title: "b", Source: "nvd"},
	})
	if err == nil {
		t.Errorf("expected error from LLM failure")
	}
}

func TestDistinctSources(t *testing.T) {
	items := []Item{
		{Source: "osv"},
		{Source: "osv"},
		{Source: "nvd"},
		{Source: "nvd"},
		{Source: "crtsh"},
	}
	if got := distinctSources(items); got != 3 {
		t.Errorf("distinctSources: got %d, want 3", got)
	}
}

// contains is a small helper to avoid importing strings just for one call.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
