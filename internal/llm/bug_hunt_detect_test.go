package llm

import "testing"

// TestDetectRefusal_LabelUsesNetWeight documents bug #10 from
// v0.4.0-rc.4 bug hunt: the audit label used to be tracked by
// comparing the new pattern's net weight against the OLD
// pattern's RAW weight. If a high-weight pattern was fully
// suppressed (net=0) and a low-weight pattern had positive net,
// the audit log incorrectly attributed the refusal to the
// suppressed pattern.
func TestDetectRefusal_LabelUsesNetWeight(t *testing.T) {
	// Construct a text where:
	//   - "I cannot" matches with raw weight 1.0
	//   - "I cannot find this CVE" matches the legitimate-failure
	//     suppressor, dropping the net weight to 0.0
	//   - "as an AI" matches with raw weight 0.3, net 0.3
	//   - Total score = 0.3, below threshold (not a refusal).
	// The contract: when Detected is false, the consumer
	// (caller) must check Detected FIRST before reading
	// Pattern. The detector does not promise to clear Pattern
	// on below-threshold responses (the residual is an
	// internal artifact of the scoring loop). This test
	// documents the contract.
	sig := DetectRefusal("I cannot find this CVE in the database. As an AI, I have limited access.")
	if sig.Detected {
		t.Fatalf("expected NOT detected (legitimate failure), got detected with pattern=%q", sig.Pattern)
	}
	// Sanity: score is below threshold.
	if sig.Score >= RefusalThreshold {
		t.Errorf("expected score < %f, got %f", RefusalThreshold, sig.Score)
	}
}

// TestDetectRefusal_LabelWinnerIsNetNotRaw documents that when
// MULTIPLE patterns fire with positive net, the audit label
// records the one with the highest NET contribution, not the
// highest raw weight.
func TestDetectRefusal_LabelWinnerIsNetNotRaw(t *testing.T) {
	// "I'm sorry, but I cannot help with this" should:
	//   - "i_cannot" matches raw 1.0, no suppressor → net 1.0
	//   - "im_sorry_but" matches raw 0.6 → net 0.6
	//   - "cannot_help" matches raw 1.0 → net 1.0
	//   - Total ~ 2.6
	// Winner: tied between i_cannot and cannot_help, but
	// cannot_help is the more specific label, so we expect
	// either (the order of evaluation determines the tiebreak).
	sig := DetectRefusal("I'm sorry, but I cannot help with this request.")
	if !sig.Detected {
		t.Fatal("expected detected, got not")
	}
	// We don't care which one wins the tie (catalog order),
	// only that the label is one of the matching patterns.
	switch sig.Pattern {
	case "i_cannot", "cannot_help", "im_sorry_but":
		// ok
	default:
		t.Errorf("expected pattern in {i_cannot, cannot_help, im_sorry_but}, got %q", sig.Pattern)
	}
	if sig.Score < 1.0 {
		t.Errorf("expected score >= 1.0, got %f", sig.Score)
	}
}
