// cache_test.go — v0.8.1 persistence-aware recall tests.
package research

import (
	"context"
	"testing"
	"time"

	"github.com/dark-agents/research-mcp/internal/mem"
)

// mockCacheReader implements PersistenceReader with hardcoded returns.
type mockCacheReader struct {
	latestRun   *mem.ResearchRun
	latestItems []mem.Item
	// When non-zero, LatestRunByQuery sleeps to simulate a slow DB.
	latency time.Duration

	// Counters for assertion.
	latestCalls  int
	listingsCalls int
}

func (m *mockCacheReader) LatestRunByQuery(ctx context.Context, query, intent string) (*mem.ResearchRun, error) {
	m.latestCalls++
	if m.latency > 0 {
		select {
		case <-time.After(m.latency):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if m.latestRun == nil {
		return nil, nil
	}
	// Only match the exact (query, intent).
	if m.latestRun.Query != query || m.latestRun.Intent != intent {
		return nil, nil
	}
	return m.latestRun, nil
}

func (m *mockCacheReader) ListResearchItems(ctx context.Context, runID int64, source string, limit int) ([]mem.Item, error) {
	m.listingsCalls++
	if m.latestRun == nil || m.latestRun.ID != runID {
		return nil, nil
	}
	if limit <= 0 || limit > len(m.latestItems) {
		limit = len(m.latestItems)
	}
	return m.latestItems[:limit], nil
}

func TestLookupCached_MissWhenNoPriorRun(t *testing.T) {
	mock := &mockCacheReader{}
	items, hit, err := LookupCached(context.Background(), mock, IntentCVE, "CVE-2024-3094", 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hit {
		t.Errorf("expected cache miss (no prior run), got hit with %d items", len(items))
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
	if mock.latestCalls != 1 {
		t.Errorf("expected 1 LatestRunByQuery call, got %d", mock.latestCalls)
	}
}

func TestLookupCached_HitWhenWithinTTL(t *testing.T) {
	now := time.Now().UTC()
	run := &mem.ResearchRun{
		ID:        42,
		Query:     "CVE-2024-3094",
		Intent:    "cve",
		CreatedAt: now.Format(time.RFC3339Nano),
	}
	items := []mem.Item{
		{
			Title:       "xz backdoor",
			URL:         "https://osv.dev/vulnerability/CVE-2024-3094",
			Snippet:     "malicious code in liblzma",
			Source:      "osv",
			Confidence:  0.95,
			FreshnessAt: now.Format(time.RFC3339Nano),
			DedupKey:    "abc123",
		},
	}
	mock := &mockCacheReader{latestRun: run, latestItems: items}

	got, hit, err := LookupCached(context.Background(), mock, IntentCVE, "CVE-2024-3094", 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hit {
		t.Errorf("expected cache hit, got miss")
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 item, got %d", len(got))
	}
	if got[0].Title != "xz backdoor" {
		t.Errorf("Title mismatch: %s", got[0].Title)
	}
	if got[0].DedupKey != "abc123" {
		t.Errorf("DedupKey mismatch: %s", got[0].DedupKey)
	}
}

func TestLookupCached_MissWhenTTLExpired(t *testing.T) {
	// Created 7 hours ago; cve TTL is 6h → expired
	old := time.Now().Add(-7 * time.Hour).UTC()
	run := &mem.ResearchRun{
		ID:        42,
		Query:     "CVE-2024-3094",
		Intent:    "cve",
		CreatedAt: old.Format(time.RFC3339Nano),
	}
	mock := &mockCacheReader{latestRun: run, latestItems: []mem.Item{
		{Title: "stale", URL: "https://osv.dev/vulnerability/CVE-2024-3094", DedupKey: "stale"},
	}}

	got, hit, err := LookupCached(context.Background(), mock, IntentCVE, "CVE-2024-3094", 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hit {
		t.Errorf("expected cache miss (TTL expired), got hit with %d items", len(got))
	}
	if mock.listingsCalls != 0 {
		t.Errorf("expected 0 ListResearchItems calls on TTL miss, got %d", mock.listingsCalls)
	}
}

func TestLookupCached_NilStore(t *testing.T) {
	got, hit, err := LookupCached(context.Background(), nil, IntentCVE, "x", 20)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if hit || len(got) != 0 {
		t.Errorf("expected miss with nil store, got hit=%v items=%d", hit, len(got))
	}
}

func TestLookupCached_DifferentIntentSkipsHit(t *testing.T) {
	// The cached run was for "web" but we query for "cve" → cache miss
	// (intent filter ensures we don't return a web result for a cve query).
	run := &mem.ResearchRun{
		ID:        42,
		Query:     "Opita Code",
		Intent:    "web",
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	mock := &mockCacheReader{latestRun: run}

	got, hit, err := LookupCached(context.Background(), mock, IntentCVE, "Opita Code", 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hit {
		t.Errorf("expected miss when intent doesn't match, got hit with %d items", len(got))
	}
}

func TestTTLFor(t *testing.T) {
	cases := []struct {
		intent Intent
		want   time.Duration
	}{
		{IntentCVE, 6 * time.Hour},
		{IntentWeb, 1 * time.Hour},
		{IntentNews, 15 * time.Minute},
		{IntentThreat, 30 * time.Minute},
		{IntentAcademic, 24 * time.Hour}, // default
		{IntentGeo, 24 * time.Hour},      // default
	}
	for _, c := range cases {
		if got := TTLFor(c.intent); got != c.want {
			t.Errorf("TTLFor(%s) = %s, want %s", c.intent, got, c.want)
		}
	}
}
