package safety

import (
	"fmt"
	"log"
	"sync"
)

// Defense is the unified defense-layer facade. A single
// instance is held in package-level state (see Default below)
// and consulted by every tool handler. It is safe for
// concurrent use.
//
// Defense composes:
//   - InputValidator  (L1) — checks tool input args.
//   - OutputSanitizer (L2 + L7) — checks tool results, canary detection.
//   - CanaryToken     (L7) — the secret string embedded in the system prompt.
//   - RateLimiter     (L8) — per-session tool-call cap.
//   - AnomalyDetector (L9) — heuristic event hook.
//
// The defense is initialized with a per-session canary at
// startup. The canary is exposed to the constitution (so it
// can be embedded in the system prompt) but should not appear
// in any user-visible output.
//
// Defense is designed to be FAIL-CLOSED for security-critical
// checks: if the input is overlong or contains a canary, the
// call is rejected. The LLM is never called with input that
// failed the validator. For injection markers in input, the
// call is allowed (the dark_ssd_* judges are designed to
// classify them) but the markers are recorded for audit.
type Defense struct {
	Canary    CanaryToken
	Validator *InputValidator
	Sanitizer *OutputSanitizer
	Limiter   *RateLimiter
	Anomaly   *AnomalyDetector

	mu sync.Mutex
}

// NewDefense constructs a Defense with a fresh canary and the
// given rate-limit cap. cap=0 disables rate limiting.
func NewDefense(maxCallsPerSession int) *Defense {
	canary := NewCanary()
	d := &Defense{
		Canary:    canary,
		Validator: NewInputValidator(),
		Sanitizer: NewOutputSanitizer(),
		Limiter:   NewRateLimiter(maxCallsPerSession),
		Anomaly:   NewAnomalyDetector(),
	}
	d.Validator.SetCanary(canary.String())
	d.Sanitizer.SetCanary(canary.String())
	d.Anomaly.OnAnomaly = func(ev AnomalyEvent) {
		// Default anomaly handler: log to stderr. Production
		// deployments may replace this with a callback that
		// pages the operator.
		log.Printf("safety: anomaly detected: kind=%s tool=%s detail=%s", ev.Kind, ev.Tool, ev.Detail)
	}
	return d
}

// CheckInput runs the input validator and records a tool-call
// event for anomaly detection. Returns a result with OK=false
// on hard failures (overlong, canary leak, too many args).
// On OK=false, the caller MUST NOT proceed to the LLM call.
func (d *Defense) CheckInput(tool string, args map[string]any) InputValidationResult {
	d.Anomaly.RecordToolCall(tool)
	res := d.Validator.Validate(args)
	if !res.OK {
		// Audit the rejection to the operator's log.
		log.Printf("safety: input rejected for tool=%s reason=hard canary_leak=%v", tool, res.CanaryLeak)
	}
	if res.CanaryLeak {
		d.Anomaly.RecordCanaryLeak(tool, "")
	}
	return res
}

// CheckOutput runs the output sanitizer. Returns the result;
// the caller decides whether to surface the output or take
// action. A canary leak in the output is a CRITICAL event —
// the caller should refuse to surface the result and log the
// full event.
func (d *Defense) CheckOutput(tool string, output string) SanitizeResult {
	res := d.Sanitizer.Check(output)
	if res.CanaryLeaked {
		d.Anomaly.RecordCanaryLeak(tool, res.Excerpt)
		log.Printf("safety: CRITICAL canary leaked in tool=%s output: %q", tool, res.Excerpt)
	}
	return res
}

// AllowToolCall checks the rate limiter. Returns true if the
// call is within the session-wide cap.
func (d *Defense) AllowToolCall(tool string) bool {
	return d.Limiter.Allow(tool)
}

// RecordRefusal records a refusal event for anomaly detection.
func (d *Defense) RecordRefusal(tool string) {
	d.Anomaly.RecordRefusal(tool)
}

// CanaryString returns the canary value for embedding in the
// system prompt.
func (d *Defense) CanaryString() string {
	return d.Canary.String()
}

// Stats returns a snapshot of the defense state for diagnostics.
func (d *Defense) Stats() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return fmt.Sprintf("canary=%s calls=%d anomalies=active",
		d.Canary.String()[:24]+"...", d.Limiter.Count())
}

// ---------------------------------------------------------------------------
// Package-level default. Initialized in main via InitDefault.
// Tests that need a fresh defense use NewDefense directly.
// ---------------------------------------------------------------------------

var (
	defaultMu sync.Mutex
	defaultDF *Defense
)

// InitDefault sets the package-level defense. Called by main
// after parsing flags. Re-init is allowed; the previous defense
// is discarded.
func InitDefault(maxCallsPerSession int) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultDF = NewDefense(maxCallsPerSession)
}

// Default returns the package-level defense, or nil if not
// initialized. Tools that need the defense should call Default
// at handler time and skip the call if nil.
func Default() *Defense {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	return defaultDF
}
