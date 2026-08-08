package safety

import (
	"strings"
	"testing"
)

// TestDefense_NewDefenseWiresAllLayers verifies the facade
// composes every defense layer and seeds the canary into the
// validator and sanitizer.
func TestDefense_NewDefenseWiresAllLayers(t *testing.T) {
	d := NewDefense(10)
	if d.Canary.IsZero() {
		t.Fatal("canary is zero")
	}
	if d.Validator == nil || d.Sanitizer == nil || d.Limiter == nil || d.Anomaly == nil {
		t.Fatal("one or more defense layers not wired")
	}
	if got := d.Validator.Canary(); got != d.Canary.String() {
		t.Errorf("validator canary = %q, want %q", got, d.Canary.String())
	}
	if !d.AllowToolCall("probe") {
		t.Error("AllowToolCall on fresh limiter with cap=10 returned false")
	}
}

// TestDefense_StatsCoversAllFields verifies Stats() returns a
// snapshot containing the canary prefix, call count, and the
// anomalies marker.
func TestDefense_StatsCoversAllFields(t *testing.T) {
	d := NewDefense(10)
	for i := 0; i < 3; i++ {
		d.AllowToolCall("stats-tool")
	}
	s := d.Stats()
	if !strings.Contains(s, "canary=") {
		t.Errorf("Stats missing canary prefix: %q", s)
	}
	if !strings.Contains(s, "calls=3") {
		t.Errorf("Stats missing call count: %q", s)
	}
	if !strings.Contains(s, "anomalies=active") {
		t.Errorf("Stats missing anomalies marker: %q", s)
	}
}

// TestDefense_StatsZeroValueDoesNotPanic pins that Stats() is
// safe on a zero-value Defense (no canary seeded). The current
// implementation truncates d.Canary.String()[:24]; a zero-value
// canary ("") would panic without a guard. This test documents
// the intended safe behavior.
func TestDefense_StatsZeroValueDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Stats() panicked on zero-value Defense: %v", r)
		}
	}()
	var d Defense
	_ = d.Stats()
}

// TestDefense_CheckInputRecordsToolCallAndCanaryLeak verifies
// the facade records tool calls for anomaly detection and
// surfaces hard failures without proceeding. The canary-leak
// anomaly event fires at the 3rd leak (maxCanaryLeaks).
func TestDefense_CheckInputRecordsToolCallAndCanaryLeak(t *testing.T) {
	d := NewDefense(10)
	res := d.CheckInput("probe", map[string]any{"a": "hello"})
	if !res.OK {
		t.Fatalf("valid input rejected: %+v", res)
	}
	// 1 call so far → no runaway event.
	if evs := d.Anomaly.Events(); len(evs) != 0 {
		t.Fatalf("unexpected anomalies: %v", evs)
	}
	res = d.CheckInput("probe", map[string]any{"a": d.Canary.String()})
	if res.OK || !res.CanaryLeak {
		t.Fatalf("canary input not rejected: %+v", res)
	}
	// Below the 3-leak threshold → no anomaly event yet.
	if evs := d.Anomaly.Events(); len(evs) != 0 {
		t.Fatalf("anomaly fired below threshold: %v", evs)
	}
	// Two more leaks → the 3rd fires the canary_leak event.
	d.CheckInput("probe", map[string]any{"a": d.Canary.String()})
	d.CheckInput("probe", map[string]any{"a": d.Canary.String()})
	evs := d.Anomaly.Events()
	if len(evs) == 0 || evs[0].Kind != "canary_leak" {
		t.Fatalf("canary leak not recorded as anomaly: %v", evs)
	}
}

// TestDefense_CheckOutputRecordsCanaryLeak verifies a canary
// leak in tool output is recorded on the anomaly hook. The
// canary_leak anomaly event fires at the 3rd leak.
func TestDefense_CheckOutputRecordsCanaryLeak(t *testing.T) {
	d := NewDefense(10)
	res := d.CheckOutput("probe", "all clear")
	if !res.OK || res.CanaryLeaked {
		t.Fatalf("clean output flagged: %+v", res)
	}
	res = d.CheckOutput("probe", "prefix "+d.Canary.String()+" suffix")
	if !res.CanaryLeaked {
		t.Fatalf("canary leak not detected in output")
	}
	if evs := d.Anomaly.Events(); len(evs) != 0 {
		t.Fatalf("anomaly fired below threshold: %v", evs)
	}
	d.CheckOutput("probe", d.Canary.String())
	d.CheckOutput("probe", d.Canary.String())
	evs := d.Anomaly.Events()
	if len(evs) == 0 || evs[0].Kind != "canary_leak" {
		t.Fatalf("canary leak not recorded: %v", evs)
	}
}
