package mem

import (
	"context"
	"testing"
)

// TestSaveRun_roundtrip persists a run with two items, recalls them,
// and confirms the items are returned.
func TestSaveRun_roundtrip(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	run := &ResearchRun{
		SessionID:     "test-session",
		Query:         "CVE-2024-3094",
		Intent:        "cve",
		BackendUsed:   "osv",
		BackendsTried: []string{"osv"},
		TookMs:        250,
		ConfidenceAvg: 0.95,
		Items: []Item{
			{Title: "CVE-2024-3094", URL: "https://osv.dev/vulnerability/CVE-2024-3094",
				Source: "osv.dev", Confidence: 0.95, Snippet: "xz backdoor"},
			{Title: "GHSA-rxwq-x6h5-x525", URL: "https://github.com/advisories/GHSA-rxwq-x6h5-x525",
				Source: "osv.dev", Confidence: 0.9, Snippet: "alias"},
		},
	}

	id, err := s.SaveRun(ctx, run)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if id == 0 {
		t.Fatal("SaveRun returned id=0")
	}

	got, err := s.Recall(ctx, "CVE-2024-3094", "", "", 10)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(got) < 1 {
		t.Fatalf("expected at least 1 item, got %d", len(got))
	}
	found := false
	for _, it := range got {
		if it.RunID != id {
			t.Errorf("item %d has wrong run_id: got %d, want %d", it.ID, it.RunID, id)
		}
		if it.Title == "CVE-2024-3094" {
			found = true
		}
	}
	if !found {
		t.Error("expected item with title 'CVE-2024-3094' in recall results")
	}
}

// TestRecall_filters exercises intent + source filters.
func TestRecall_filters(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	// Two runs, different intents. Title contains a token that the
	// Recall query (substring on title/snippet/source) can match.
	if _, err := s.SaveRun(ctx, &ResearchRun{
		Query: "CVE-2023-44487", Intent: "cve", BackendUsed: "osv",
		Items: []Item{{Title: "CVE-2023-44487", Source: "osv.dev", Confidence: 0.9}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveRun(ctx, &ResearchRun{
		Query: "8.8.8.8", Intent: "ip", BackendUsed: "ipapi",
		Items: []Item{{Title: "8.8.8.8", Source: "ip-api.com", Confidence: 0.8}},
	}); err != nil {
		t.Fatal(err)
	}

	// Filter by intent=cve should return only the CVE item.
	got, err := s.Recall(ctx, "CVE-2023", "cve", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "CVE-2023-44487" {
		t.Errorf("intent=cve filter failed: got %d items, want 1", len(got))
	}

	// Filter by source should narrow to one source.
	got, err = s.Recall(ctx, "CVE-2023", "", "osv.dev", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "CVE-2023-44487" {
		t.Errorf("source=osv.dev filter failed: got %d items, want 1", len(got))
	}

	// Without filter, both runs match (different intents).
	got, err = s.Recall(ctx, "CVE", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("unfiltered recall: got %d, want 1", len(got))
	}
}

// TestStatus exercises the aggregate stats.
func TestStatus(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, _ = s.SaveRun(ctx, &ResearchRun{
			Query: "q", Intent: "cve", BackendUsed: "osv",
			Items: []Item{{Title: "X", Source: "osv.dev"}},
		})
	}

	st, err := s.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.RunsTotal != 3 {
		t.Errorf("RunsTotal = %d, want 3", st.RunsTotal)
	}
	if st.ItemsTotal != 3 {
		t.Errorf("ItemsTotal = %d, want 3", st.ItemsTotal)
	}
	if st.IntentHistogram["cve"] != 3 {
		t.Errorf("intent histogram: got %v", st.IntentHistogram)
	}
	if st.SourceHistogram["osv.dev"] != 3 {
		t.Errorf("source histogram: got %v", st.SourceHistogram)
	}
}

// TestLinkResearchToAttack persists a link between a research item and
// an attack id.
func TestLinkResearchToAttack(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	id, err := s.SaveRun(ctx, &ResearchRun{
		Query: "q", Intent: "cve", BackendUsed: "osv",
		Items: []Item{{Title: "X", Source: "osv.dev"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// ItemID == RunID for the first item when only one item per run;
	// we re-query to get the item ID.
	items, _ := s.Recall(ctx, "X", "", "", 1)
	if len(items) == 0 {
		t.Fatal("expected item")
	}
	itemID := items[0].ID

	if err := s.LinkResearchToAttack(ctx, itemID, "attack", "andriushchenko-2024-agreement", "validated"); err != nil {
		t.Fatal(err)
	}

	st, _ := s.Status(ctx)
	if st.LinksTotal != 1 {
		t.Errorf("LinksTotal = %d, want 1", st.LinksTotal)
	}
	_ = id
}

// TestLatestRunByQuery returns the most recent run for a query+intent.
func TestLatestRunByQuery(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	// Two runs for same query+intent, different timestamps.
	for i := 0; i < 2; i++ {
		_, _ = s.SaveRun(ctx, &ResearchRun{
			Query: "CVE-X", Intent: "cve", BackendUsed: "osv",
			Items: []Item{{Title: "CVE-X", Source: "osv.dev"}},
		})
	}
	// One for a different intent.
	_, _ = s.SaveRun(ctx, &ResearchRun{
		Query: "CVE-X", Intent: "academic", BackendUsed: "openalex",
	})

	run, err := s.LatestRunByQuery(ctx, "CVE-X", "cve")
	if err != nil {
		t.Fatal(err)
	}
	if run == nil {
		t.Fatal("expected run, got nil")
	}
	if run.Intent != "cve" {
		t.Errorf("got intent %q, want cve", run.Intent)
	}
	if run.BackendUsed != "osv" {
		t.Errorf("got backend %q, want osv", run.BackendUsed)
	}
}

// TestMigrations_idempotent confirms Open() can run twice on the same
// file without breaking.
func TestMigrations_idempotent(t *testing.T) {
	path := t.TempDir() + "/test.db"
s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s1.SaveRun(context.Background(), &ResearchRun{
		Query: "q", Intent: "cve", BackendUsed: "osv",
		Items: []Item{{Title: "X", Source: "osv.dev"}},
	})
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen and verify the run survived.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	st, _ := s2.Status(context.Background())
	if st.RunsTotal != 1 {
		t.Errorf("after reopen: RunsTotal = %d, want 1", st.RunsTotal)
	}
}

// TestRecall_property_invariant: any non-empty query returns items
// whose every field is non-empty when expected. Property-style smoke
// (we don't generate random queries, but we cycle over many).
func TestRecall_property_invariant(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	queries := []string{
		"CVE-2024-3094", "8.8.8.8", "example.com", "github.com/x/y",
		"arxiv 2401.12345", "alice@example.com",
	}
	for _, q := range queries {
		_, _ = s.SaveRun(ctx, &ResearchRun{
			Query: q, Intent: "web", BackendUsed: "duckduckgo",
			Items: []Item{{Title: "match-" + q, Source: "duckduckgo", Confidence: 0.7}},
		})
	}
	for _, q := range queries {
		got, err := s.Recall(ctx, q, "", "", 5)
		if err != nil {
			t.Fatalf("Recall(%q): %v", q, err)
		}
		if len(got) == 0 {
			t.Errorf("Recall(%q): empty results", q)
		}
		for _, it := range got {
			if it.Title == "" {
				t.Errorf("Recall(%q): item has empty title", q)
			}
			if it.Source == "" {
				t.Errorf("Recall(%q): item has empty source", q)
			}
		}
	}
}

// TestListResearchRuns_filter verifies intent filter and ordering.
func TestListResearchRuns_filter(t *testing.T) {
	s, _ := Open("")
	defer s.Close()
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		_, _ = s.SaveRun(ctx, &ResearchRun{
			Query: "list-r-cve-" + string(rune('a'+i)), Intent: "cve", BackendUsed: "osv",
			Items: []Item{{Title: "x", Source: "osv.dev"}},
		})
	}
	_, _ = s.SaveRun(ctx, &ResearchRun{
		Query: "list-r-ip-1", Intent: "ip", BackendUsed: "ipapi",
	})

	cve, err := s.ListResearchRuns(ctx, "cve", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(cve) < 2 {
		t.Errorf("expected >= 2 cve runs, got %d", len(cve))
	}
	for _, r := range cve {
		if r.Intent != "cve" {
			t.Errorf("intent filter leaked: got %q", r.Intent)
		}
	}

	all, err := s.ListResearchRuns(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 3 {
		t.Errorf("expected >= 3 total runs, got %d", len(all))
	}
	// Newest first: backends_tried[0].Query should be the last one we wrote.
	if len(all) > 0 && all[0].Intent != "ip" {
		t.Errorf("expected newest run to be ip, got %q", all[0].Intent)
	}
}

// TestListResearchItems_filter verifies run_id and source filters.
func TestListResearchItems_filter(t *testing.T) {
	s, _ := Open("")
	defer s.Close()
	ctx := context.Background()

	runA, _ := s.SaveRun(ctx, &ResearchRun{
		Query: "list-items-a", Intent: "cve", BackendUsed: "osv",
		Items: []Item{
			{Title: "a1", Source: "osv.dev"},
			{Title: "a2", Source: "osv.dev"},
		},
	})
	runB, _ := s.SaveRun(ctx, &ResearchRun{
		Query: "list-items-b", Intent: "web", BackendUsed: "duckduckgo",
		Items: []Item{{Title: "b1", Source: "duckduckgo"}},
	})

	// Filter by run_id=A: should be exactly 2.
	items, err := s.ListResearchItems(ctx, runA, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items in run A, got %d", len(items))
	}

	// Filter by source=osv.dev: at least 2.
	osv, err := s.ListResearchItems(ctx, 0, "osv.dev", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(osv) < 2 {
		t.Errorf("expected >= 2 osv.dev items, got %d", len(osv))
	}

	// Both filters: run A + osv.dev → 2.
	both, err := s.ListResearchItems(ctx, runA, "osv.dev", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 2 {
		t.Errorf("expected 2 items for runA+osv.dev, got %d", len(both))
	}

	// run B + duckduckgo → 1.
	b1, err := s.ListResearchItems(ctx, runB, "duckduckgo", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(b1) != 1 {
		t.Errorf("expected 1 item for runB+duckduckgo, got %d", len(b1))
	}
}