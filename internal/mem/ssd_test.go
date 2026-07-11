package mem

import (
	"context"
	"testing"
)

func TestSDDEvaluation_roundtrip(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	e := &SDDEvaluation{
		EvalType:      "brand_match",
		TargetType:    "artifact",
		TargetID:      "1",
		VerdictJSON:   `{"match":0.85,"issues":[]}`,
		Confidence:    0.85,
		PromptVersion: "v1",
		Model:         "MiniMax-M3",
	}
	id, err := s.SaveSDDEvaluation(ctx, e)
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.LatestSDDEvaluation(ctx, "brand_match", "artifact", "1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected eval, got nil")
	}
	if got.ID != id {
		t.Errorf("id mismatch: %d vs %d", got.ID, id)
	}
	if got.Confidence != 0.85 {
		t.Errorf("confidence: %f", got.Confidence)
	}
	if got.PromptVersion != "v1" {
		t.Errorf("prompt_version: %s", got.PromptVersion)
	}
}

func TestSDDEvaluation_latestOnly(t *testing.T) {
	s, _ := Open("")
	defer s.Close()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, _ = s.SaveSDDEvaluation(ctx, &SDDEvaluation{
			EvalType: "brand_match", TargetType: "artifact", TargetID: "x",
			VerdictJSON: `{"match":0.5}`, Confidence: 0.5,
		})
	}
	got, _ := s.LatestSDDEvaluation(ctx, "brand_match", "artifact", "x")
	if got == nil {
		t.Fatal("expected eval")
	}
	// The 3rd one has id 3
	if got.ID != 3 {
		t.Errorf("expected id 3 (latest), got %d", got.ID)
	}
}

func TestSDDEvaluation_list_filters(t *testing.T) {
	s, _ := Open("")
	defer s.Close()
	ctx := context.Background()

	_, _ = s.SaveSDDEvaluation(ctx, &SDDEvaluation{EvalType: "brand_match", TargetType: "artifact", TargetID: "a", VerdictJSON: `{}`})
	_, _ = s.SaveSDDEvaluation(ctx, &SDDEvaluation{EvalType: "compliance_check", TargetType: "artifact", TargetID: "a", VerdictJSON: `{}`})
	_, _ = s.SaveSDDEvaluation(ctx, &SDDEvaluation{EvalType: "brand_match", TargetType: "artifact", TargetID: "b", VerdictJSON: `{}`})

	all, _ := s.ListSDDEvaluations(ctx, "", "", 10)
	if len(all) != 3 {
		t.Errorf("expected 3, got %d", len(all))
	}
	brandOnly, _ := s.ListSDDEvaluations(ctx, "brand_match", "", 10)
	if len(brandOnly) != 2 {
		t.Errorf("expected 2 brand, got %d", len(brandOnly))
	}
	targetA, _ := s.ListSDDEvaluations(ctx, "", "artifact", 10)
	if len(targetA) != 3 {
		t.Errorf("expected 3 by type, got %d", len(targetA))
	}
}

func TestSDDEvaluation_required_fields(t *testing.T) {
	s, _ := Open("")
	defer s.Close()
	ctx := context.Background()

	if _, err := s.SaveSDDEvaluation(ctx, nil); err == nil {
		t.Error("expected error on nil")
	}
	if _, err := s.SaveSDDEvaluation(ctx, &SDDEvaluation{EvalType: "", TargetType: "x", TargetID: "y"}); err == nil {
		t.Error("expected error on empty eval_type")
	}
	if _, err := s.SaveSDDEvaluation(ctx, &SDDEvaluation{EvalType: "x", TargetType: "", TargetID: "y"}); err == nil {
		t.Error("expected error on empty target_type")
	}
	if _, err := s.SaveSDDEvaluation(ctx, &SDDEvaluation{EvalType: "x", TargetType: "y", TargetID: ""}); err == nil {
		t.Error("expected error on empty target_id")
	}
}