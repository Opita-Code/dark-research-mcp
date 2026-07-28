// Package research — synthesize.go (v0.8.1): optional LLM synthesis.
//
// After the multi-backend merge + dedup produces a list of items, the
// caller can opt-in to a one-paragraph analyst summary via the
// synthesize flag (set via Router.Synthesize). When enabled:
//
//   - ≥2 items from ≥2 distinct backends (corroboration threshold).
//     A single-backend result doesn't need synthesis.
//   - LLMClient is non-nil AND SDD_LLM_API_KEY is set in the env.
//   - Router passes the items to the LLM with a system prompt asking
//     for a one-paragraph OSINT summary.
//
// On any failure (LLM error, no key, no API endpoint), the function
// returns summary="" and a nil error so the caller can degrade
// gracefully. Synthesis is an enhancement; the items alone are
// already valid output.
package research

import (
	"context"
	"fmt"
	"strings"
)

// LLMClient is the minimal interface Synthesize needs. *llm.Client
// satisfies it; tests can pass a stub.
type LLMClient interface {
	Complete(ctx context.Context, system string, msgs ...LLMMessage) (string, error)
}

// LLMMessage is one chat turn passed to the LLM. *llm.Message satisfies
// it; tests can pass a stub.
type LLMMessage struct {
	Role    string // "system", "user", "assistant"
	Content string
}

// Synthesize calls the LLM to produce a one-paragraph summary of the
// items. Returns ("", nil) when synthesis is not warranted (degrades
// gracefully):
//
//   - llm is nil → no synthesis
//   - len(items) < 2 → no synthesis (need corroboration)
//   - distinctSources(items) < 2 → no synthesis (need cross-source)
//   - LLM call fails → no synthesis, but no error returned to caller
//
// Returns (summary, error) only when an unexpected error happens. The
// router ignores the error and proceeds without a summary.
func Synthesize(ctx context.Context, llm LLMClient, query string, items []Item) (string, error) {
	if llm == nil {
		return "", nil
	}
	if len(items) < 2 {
		return "", nil
	}
	if distinctSources(items) < 2 {
		return "", nil
	}

	system := `You are an OSINT analyst. Given a research query and a
list of corroborating findings (each from a distinct data source),
produce a one-paragraph (3-5 sentences) summary. Cite the source of
each claim inline, e.g. "(osv.dev)" or "(news: gdelt)". Do not invent
facts not present in the findings. Output ONLY the paragraph, no
preamble, no bullet points.`

	user := fmt.Sprintf("Query: %s\n\nFindings (%d total):\n", query, len(items))
	for i, it := range items {
		user += fmt.Sprintf("\n%d. [%s] %s\n   URL: %s\n",
			i+1, it.Source, it.Title, it.URL)
		if it.Snippet != "" {
			user += "   " + it.Snippet + "\n"
		}
	}

	summary, err := llm.Complete(ctx, system,
		LLMMessage{Role: "user", Content: user},
	)
	if err != nil {
		// Synthesis failure is non-fatal; the items are still useful.
		return "", fmt.Errorf("synthesize: llm: %w", err)
	}
	summary = strings.TrimSpace(summary)
	return summary, nil
}

// distinctSources returns the count of unique Source values in items.
// Used to enforce the "≥2 backends" corroboration threshold.
func distinctSources(items []Item) int {
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		seen[it.Source] = true
	}
	return len(seen)
}
