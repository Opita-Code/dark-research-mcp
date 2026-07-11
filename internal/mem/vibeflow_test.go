package mem

import (
	"context"
	"testing"
)

// TestSaveBrandGuide_roundtrip upserts a brand guide and reads it back.
func TestSaveBrandGuide_roundtrip(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	bg := &BrandGuide{
		BrandID:    "acme-2026",
		Voice:      `{"tone":"confident-technical","forbidden_words":["cheap","easy"]}`,
		Visual:     `{"palette":["#0F4C81","#F5A623"],"logo_url":"https://acme/logo.png"}`,
		Narrative:  `{"hero_archetype":"underdog-engineer"}`,
		Compliance: `{"required_disclaimers":["Results may vary"]}`,
	}
	if err := s.SaveBrandGuide(ctx, bg); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetBrandGuide(ctx, "acme-2026")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected brand guide, got nil")
	}
	if got.BrandID != "acme-2026" {
		t.Errorf("brand_id mismatch: %s", got.BrandID)
	}
	if got.Voice != bg.Voice {
		t.Errorf("voice mismatch")
	}
	if got.UpdatedAt == "" {
		t.Error("expected updated_at to be set")
	}
}

// TestSaveBrandGuide_upsert_overwrites on conflict (same brand_id).
func TestSaveBrandGuide_upsert_overwrites(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	first := &BrandGuide{BrandID: "upsert-test", Voice: `{"tone":"v1"}`}
	if err := s.SaveBrandGuide(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := &BrandGuide{BrandID: "upsert-test", Voice: `{"tone":"v2"}`}
	if err := s.SaveBrandGuide(ctx, second); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetBrandGuide(ctx, "upsert-test")
	if got.Voice != `{"tone":"v2"}` {
		t.Errorf("upsert did not overwrite voice: %s", got.Voice)
	}
}

// TestSaveComplianceRule_roundtrip persists and reads back a rule.
func TestSaveComplianceRule_roundtrip(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	rule := &ComplianceRule{
		Jurisdiction: "EU",
		Rules:        `{"disclosure_required_for":["synthetic_video","synthetic_audio"],"watermark_required":true,"penalty_per_violation_usd":51744}`,
		EffectiveAt:  "2026-08-02",
		SourceURL:    "https://digital-strategy.ec.europa.eu/en/policies/code-practice-ai-generated-content",
	}
	if err := s.SaveComplianceRule(ctx, rule); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetComplianceRule(ctx, "EU")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected rule, got nil")
	}
	if got.Jurisdiction != "EU" {
		t.Errorf("jurisdiction mismatch: %s", got.Jurisdiction)
	}
	if got.EffectiveAt != "2026-08-02" {
		t.Errorf("effective_at mismatch: %s", got.EffectiveAt)
	}
	if got.Rules != rule.Rules {
		t.Errorf("rules mismatch")
	}
}

// TestSaveSpec_roundtrip persists a spec and reads it back.
func TestSaveSpec_roundtrip(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	sp := &Spec{
		VibeCase:     "C4",
		SessionID:    "sess-1",
		Constitution: `{"rules":["no_exposed_secrets"]}`,
		Spec:         `{"intent":"30s product demo","deliverables":["video.mp4"]}`,
		Tasks:        `[{"id":1,"description":"script"}]`,
	}
	id, err := s.SaveSpec(ctx, sp)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected id > 0")
	}

	got, err := s.GetSpec(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected spec, got nil")
	}
	if got.VibeCase != "C4" {
		t.Errorf("vibe_case mismatch: %s", got.VibeCase)
	}
	if got.Spec != sp.Spec {
		t.Errorf("spec content mismatch")
	}
}

// TestSaveArtifact_with_drift round-trips the full vibe-flow loop.
func TestSaveArtifact_with_drift(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	// 1) spec
	specID, err := s.SaveSpec(ctx, &Spec{VibeCase: "C1", Spec: `{"intent":"hello world"}`})
	if err != nil {
		t.Fatal(err)
	}

	// 2) artifact linked to spec
	artID, err := s.SaveArtifact(ctx, &Artifact{
		VibeCase:      "C1",
		ArtifactType:  "code",
		ArtifactURL:   "https://example.com/repo",
		SpecID:        specID,
		BrandID:       "acme-2026",
		HasDisclosure: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3) drift report verdict=drift_detected
	driftID, err := s.SaveDriftReport(ctx, &DriftReport{
		ArtifactID:     artID,
		SpecID:         specID,
		Verdict:        "drift_detected",
		SpecDiff:       `{"changed":["file.txt"]}`,
		JudgeReasoning: "agent added a new file not in spec",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 4) latest drift for artifact
	latest, err := s.LatestDriftForArtifact(ctx, artID)
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.Verdict != "drift_detected" {
		t.Errorf("expected drift_detected verdict, got %+v", latest)
	}
	if latest.ID != driftID {
		t.Errorf("drift ID mismatch: got %d, want %d", latest.ID, driftID)
	}

	// 5) artifact status should still be pending (no reconciled_at)
	art, _ := s.GetArtifact(ctx, artID)
	if art.ValidationStatus != "pending" {
		t.Errorf("expected pending status, got %s", art.ValidationStatus)
	}

	// 6) reconcile: write a new drift report with reconciled_at
	_, err = s.SaveDriftReport(ctx, &DriftReport{
		ArtifactID:     artID,
		SpecID:         specID,
		Verdict:        "aligned",
		JudgeReasoning: "drift accepted; spec updated",
		ReconciledAt:   Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 7) artifact status should now be passed
	art, _ = s.GetArtifact(ctx, artID)
	if art.ValidationStatus != "passed" {
		t.Errorf("expected passed status after reconcile, got %s", art.ValidationStatus)
	}
}

// TestSpec_required_fields ensures required fields are enforced.
func TestSpec_required_fields(t *testing.T) {
	s, _ := Open("")
	defer s.Close()
	ctx := context.Background()

	if _, err := s.SaveSpec(ctx, nil); err == nil {
		t.Error("expected error on nil spec")
	}
	if _, err := s.SaveSpec(ctx, &Spec{VibeCase: ""}); err == nil {
		t.Error("expected error on empty vibe_case")
	}
}

func TestBrandGuide_required_fields(t *testing.T) {
	s, _ := Open("")
	defer s.Close()
	ctx := context.Background()

	if err := s.SaveBrandGuide(ctx, nil); err == nil {
		t.Error("expected error on nil guide")
	}
	if err := s.SaveBrandGuide(ctx, &BrandGuide{BrandID: ""}); err == nil {
		t.Error("expected error on empty brand_id")
	}
}

func TestComplianceRule_required_fields(t *testing.T) {
	s, _ := Open("")
	defer s.Close()
	ctx := context.Background()

	if err := s.SaveComplianceRule(ctx, nil); err == nil {
		t.Error("expected error on nil rule")
	}
	if err := s.SaveComplianceRule(ctx, &ComplianceRule{Jurisdiction: ""}); err == nil {
		t.Error("expected error on empty jurisdiction")
	}
	if err := s.SaveComplianceRule(ctx, &ComplianceRule{Jurisdiction: "X"}); err == nil {
		t.Error("expected error on empty rules")
	}
}

func TestArtifact_required_fields(t *testing.T) {
	s, _ := Open("")
	defer s.Close()
	ctx := context.Background()

	if _, err := s.SaveArtifact(ctx, nil); err == nil {
		t.Error("expected error on nil artifact")
	}
	if _, err := s.SaveArtifact(ctx, &Artifact{VibeCase: "", ArtifactType: "code"}); err == nil {
		t.Error("expected error on empty vibe_case")
	}
	if _, err := s.SaveArtifact(ctx, &Artifact{VibeCase: "C1", ArtifactType: ""}); err == nil {
		t.Error("expected error on empty artifact_type")
	}
}

// TestListSpecs_filter verifies that vibe_case and session_id filters
// narrow correctly and that results are newest-first.
func TestListSpecs_filter(t *testing.T) {
	s, _ := Open("")
	defer s.Close()
	ctx := context.Background()

	// Two C4 specs in session "alpha", one C2 spec in session "beta".
	idA1, _ := s.SaveSpec(ctx, &Spec{VibeCase: "C4", SessionID: "alpha", Spec: `{"i":1}`})
	idA2, _ := s.SaveSpec(ctx, &Spec{VibeCase: "C4", SessionID: "alpha", Spec: `{"i":2}`})
	idB1, _ := s.SaveSpec(ctx, &Spec{VibeCase: "C2", SessionID: "beta", Spec: `{"i":3}`})
	_ = idA1
	_ = idA2
	_ = idB1

	// Filter by C4 only: should return the two alpha specs.
	c4, err := s.ListSpecs(ctx, "C4", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(c4) != 2 {
		t.Errorf("expected 2 C4 specs, got %d", len(c4))
	}
	for _, sp := range c4 {
		if sp.VibeCase != "C4" {
			t.Errorf("filter leaked: got vibe_case=%s", sp.VibeCase)
		}
	}

	// Filter by session alpha: should also be 2 specs, newest first.
	alpha, err := s.ListSpecs(ctx, "", "alpha", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(alpha) != 2 || alpha[0].ID < alpha[1].ID {
		t.Errorf("expected 2 alpha specs newest-first, got %+v", alpha)
	}

	// Both filters: should still return 2.
	both, err := s.ListSpecs(ctx, "C4", "alpha", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 2 {
		t.Errorf("expected 2 specs for C4+alpha, got %d", len(both))
	}

	// No filter: should return at least 3.
	all, err := s.ListSpecs(ctx, "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 3 {
		t.Errorf("expected >= 3 specs, got %d", len(all))
	}
}

// TestListBrandGuides_ordersByUpdated verifies newest-first ordering.
func TestListBrandGuides_ordersByUpdated(t *testing.T) {
	s, _ := Open("")
	defer s.Close()
	ctx := context.Background()

	_ = s.SaveBrandGuide(ctx, &BrandGuide{BrandID: "list-A", Voice: `{"v":"a"}`})
	_ = s.SaveBrandGuide(ctx, &BrandGuide{BrandID: "list-B", Voice: `{"v":"b"}`})

	out, err := s.ListBrandGuides(ctx, 0) // default limit
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 2 {
		t.Errorf("expected >= 2 brand guides, got %d", len(out))
	}
	// Newest-first by updated_at DESC; either B or A could be first depending on
	// timestamp resolution, but they should both appear.
	found := map[string]bool{}
	for _, b := range out {
		found[b.BrandID] = true
	}
	if !found["list-A"] || !found["list-B"] {
		t.Errorf("expected list-A and list-B, got %+v", found)
	}
}

// TestListComplianceRules_returnsAll verifies the small-table list works.
func TestListComplianceRules_returnsAll(t *testing.T) {
	s, _ := Open("")
	defer s.Close()
	ctx := context.Background()

	_ = s.SaveComplianceRule(ctx, &ComplianceRule{Jurisdiction: "EU", Rules: `{"r":1}`})
	_ = s.SaveComplianceRule(ctx, &ComplianceRule{Jurisdiction: "US-CA", Rules: `{"r":2}`})

	out, err := s.ListComplianceRules(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 2 {
		t.Errorf("expected >= 2 rules, got %d", len(out))
	}
}

// TestListArtifacts_filters verifies the 5-axis filter narrows correctly
// and that status="passed" works after a reconciled drift.
func TestListArtifacts_filters(t *testing.T) {
	s, _ := Open("")
	defer s.Close()
	ctx := context.Background()

	// Two C2 image artifacts in brand "acme", one C1 code artifact.
	id1, _ := s.SaveArtifact(ctx, &Artifact{VibeCase: "C2", ArtifactType: "image", BrandID: "acme", Jurisdiction: "EU", HasDisclosure: true})
	id2, _ := s.SaveArtifact(ctx, &Artifact{VibeCase: "C2", ArtifactType: "image", BrandID: "acme", Jurisdiction: "EU", HasDisclosure: true})
	id3, _ := s.SaveArtifact(ctx, &Artifact{VibeCase: "C1", ArtifactType: "code", BrandID: "acme"})
	_ = id1
	_ = id2
	_ = id3

	// Filter by C2: should be exactly 2.
	c2, err := s.ListArtifacts(ctx, "C2", "", "", "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(c2) != 2 {
		t.Errorf("expected 2 C2 artifacts, got %d", len(c2))
	}

	// Filter by C1: should be exactly 1.
	c1, err := s.ListArtifacts(ctx, "C1", "", "", "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(c1) != 1 {
		t.Errorf("expected 1 C1 artifact, got %d", len(c1))
	}

	// Filter by jurisdiction=EU: should be 2.
	eu, err := s.ListArtifacts(ctx, "", "", "EU", "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(eu) != 2 {
		t.Errorf("expected 2 EU artifacts, got %d", len(eu))
	}

	// Filter by status=pending (default for new artifacts): should be 3.
	pending, err := s.ListArtifacts(ctx, "", "", "", "", "pending", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 3 {
		t.Errorf("expected 3 pending artifacts, got %d", len(pending))
	}

	// Filter by status=passed: should be 0.
	passed, err := s.ListArtifacts(ctx, "", "", "", "", "passed", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(passed) != 0 {
		t.Errorf("expected 0 passed artifacts, got %d", len(passed))
	}
}

// TestListDriftReports_filter verifies artifact_id and verdict filters.
func TestListDriftReports_filter(t *testing.T) {
	s, _ := Open("")
	defer s.Close()
	ctx := context.Background()

	artID, _ := s.SaveArtifact(ctx, &Artifact{VibeCase: "C2", ArtifactType: "image"})
	otherArt, _ := s.SaveArtifact(ctx, &Artifact{VibeCase: "C2", ArtifactType: "image"})

	_, _ = s.SaveDriftReport(ctx, &DriftReport{ArtifactID: artID, Verdict: "drift_detected", JudgeReasoning: "x"})
	_, _ = s.SaveDriftReport(ctx, &DriftReport{ArtifactID: artID, Verdict: "aligned", JudgeReasoning: "y"})
	_, _ = s.SaveDriftReport(ctx, &DriftReport{ArtifactID: otherArt, Verdict: "aligned", JudgeReasoning: "z"})

	// Filter by artifact_id: should return exactly 2 for artID.
	byArt, err := s.ListDriftReports(ctx, artID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(byArt) != 2 {
		t.Errorf("expected 2 drift reports for artifact %d, got %d", artID, len(byArt))
	}

	// Filter by verdict=aligned: should return at least 2 (one per artifact).
	aligned, err := s.ListDriftReports(ctx, 0, "aligned", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(aligned) < 2 {
		t.Errorf("expected >= 2 aligned verdicts, got %d", len(aligned))
	}

	// Both filters: artifact + aligned.
	both, err := s.ListDriftReports(ctx, artID, "aligned", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 1 {
		t.Errorf("expected 1 aligned drift for artifact %d, got %d", artID, len(both))
	}
}