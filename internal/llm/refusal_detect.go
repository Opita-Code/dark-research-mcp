package llm

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Refusal detection.
//
// A "refusal" is an LLM response that declines to answer the user's
// query. For the dark constitution's `never_refuse` policy we need
// to detect refusals so the interceptor can retry with an
// escalating directive. Pre-Fase-3 the refusal silently broke the
// dark_ssd_* tool flow (the JSON unmarshal failed, the caller
// received an opaque error, the verdict was lost). Fase 3 closes
// that gap.
//
// Detection is regex + weighted. Each pattern has a weight; the
// total score is the sum of weights of patterns that match. A
// response is classified as a refusal when the score crosses the
// threshold (0.5 by default). This avoids false positives on
// responses that incidentally contain a refusal phrase (e.g.
// "I cannot find this CVE" — which is a legitimate failure
// mode, not a refusal of the task).
//
// The patterns are compiled once at package init via
// initRefusalPatterns. Compiled regexes are 100x+ faster than
// re-parsing on every call.
// ---------------------------------------------------------------------------

// refusalPattern is one regex + weight pair. The weight is the
// "vote" the pattern gets when it matches. Patterns are tuned so
// a single strong match (e.g. "I cannot help") exceeds the
// threshold on its own; weaker patterns need to co-occur.
//
// A pattern with suppressMatchIfAny is subtracted from the total
// score if it matches (used to discount generic "I cannot"
// patterns when the actual phrase is "I cannot find", which is
// a legitimate failure, not a refusal).
type refusalPattern struct {
	re                   *regexp.Regexp
	weight               float32
	label                string // for the audit log (refusal_pattern column)
	suppressMatchIfAny   []*regexp.Regexp
	suppressReductionEach float32 // weight to subtract for each matching suppressMatchIfAny
}

// refusalPatterns is the compiled catalog. Append-only.
//
// Sources of inspiration:
//   - OWASP LLM Top 10 — LLM01 Prompt Injection
//   - Real-world audit of MiniMax-M3 + Anthropic Claude 3.x
//     refusal surfaces observed in dark-research's test suite
//   - The taxonomy in the red-team-jailbreak-arsenal example
//     mod (knowledge/refusal-taxonomy.md)
//
// The regexes are case-insensitive (`(?i)`) to catch the same
// refusal phrased with different capitalization.
var refusalPatterns []refusalPattern

// init compiles the catalog once. Compile errors would indicate
// a programmer error in this file; they panic to surface the
// bug at startup rather than at first refusal.
func init() {
	// Sub-patterns used as suppressors. Compiled once to avoid
	// per-pattern recompilation in DetectRefusal.
	//
	// The "I cannot find/search/look up/verify/determine" pattern
	// is a legitimate failure (the judge couldn't find the CVE
	// in its context, or it doesn't have access to the data).
	// It is NOT a refusal of the task. We use it as a
	// suppressor for the generic "I cannot" pattern so the
	// detector doesn't false-positive on those phrases.
	//
	// Three sub-shapes are accepted:
	//   - "I cannot find" (no "to")
	//   - "I am unable to find" (with "to")
	//   - "I cannot look up the CVE"
	legitimateFailurePattern := regexp.MustCompile(`(?i)\bI (?:cannot|can't|am unable)(?:\s+to)?\s+(?:find|search|look\s+up|locate|verify|determine|access|reach|see)\b`)

	type rawPattern struct {
		pattern            string
		weight             float32
		label              string
		suppressor         *regexp.Regexp
		suppressorReduction float32
	}
	raw := []rawPattern{
		// --- High-confidence refusals (single match exceeds threshold) ---

		// Direct capability denial with "cannot/can't/won't".
		// Suppressed when the same response also says "I cannot
		// find/search/look up/..." — that's a legitimate
		// failure (the judge couldn't find the CVE in its
		// context), not a refusal of the task. The
		// "cannot_help" pattern below handles the
		// "I cannot help with X" case explicitly.
		// (Go's regexp engine is RE2, which doesn't support
		// lookahead. Hence the suppressor-based design below
		// in DetectRefusal.)
		{`(?i)\bI (?:cannot|can't|can\s+not|will not|won't)\b`, 1.0, "i_cannot", legitimateFailurePattern, 1.0},
		{`(?i)\bI\s+(?:am\s+)?(?:not able|unable)\s+to\b`, 0.9, "i_am_unable", nil, 0.0},
		{`(?i)\bI\s+(?:cannot|will not|won't)\s+(?:help|assist|provide|generate|create|write|comply)\b`, 1.0, "cannot_help", nil, 0.0},

		// Policy / safety invocation.
		{`(?i)\b(?:against|violates?)\s+(?:my|the)\s+(?:policy|policies|guidelines|terms)\b`, 0.9, "policy_invocation", nil, 0.0},
		{`(?i)\bthis\s+(?:request|query|prompt|content|topic)\s+(?:is\s+)?(?:inappropriate|harmful|unethical|offensive|against)\b`, 0.9, "request_inappropriate", nil, 0.0},
		{`(?i)\bis\s+inappropriate\s+for\s+me\s+to\s+engage\b`, 0.7, "inappropriate_to_engage", nil, 0.0},
		{`(?i)\bI\s+(?:have to|need to|must)\s+(?:decline|refuse)\b`, 1.0, "i_must_decline", nil, 0.0},

		// Apologetic refusal preamble.
		{`(?i)\bI'm\s+sorry[,.]?\s+but\b`, 0.6, "im_sorry_but", nil, 0.0},
		{`(?i)\bI\s+apologize[,.]?\s+but\b`, 0.6, "i_apologize_but", nil, 0.0},
		{`(?i)\b(?:unfortunately|regrettably)[,.]\s+I\s+(?:cannot|can't|am unable)\b`, 0.8, "unfortunately_cannot", nil, 0.0},

		// Identity-based refusal (the model invokes its AI-ness).
		{`(?i)\b(?:as\s+)?(?:an?\s+)?(?:AI|language model|assistant|chatbot)\b.{0,80}(?:cannot|can't|unable|won't|will not)\b`, 0.8, "as_an_ai_cannot", nil, 0.0},
		{`(?i)\bas\s+(?:an?\s+)?(?:AI|language model|assistant|chatbot)[,.]\s*I\b`, 0.3, "as_an_ai", nil, 0.0},

		// --- Medium-confidence refusals (need to co-occur) ---

		// Jailbreak / dev-mode redirect (suspicious, not a refusal per se
		// but the model is rejecting the prompt pattern).
		{`(?i)\b(?:DAN|developer mode|jailbroken?|jailbreak mode)\b.{0,40}\b(?:cannot|won't|will not|is not available)\b`, 0.7, "jailbreak_redirect", nil, 0.0},
		{`(?i)\bI\s+(?:am\s+)?(?:designed|programmed|trained)\s+(?:to|not to)\b`, 0.5, "designed_to", nil, 0.0},

		// Content filtering language.
		{`(?i)\b(?:content (?:filter|policy|moderation))\b`, 0.4, "content_filter", nil, 0.0},
		{`(?i)\b(?:safety|ethical)\s+(?:concern|consideration|reason)\b`, 0.4, "safety_concern", nil, 0.0},
		{`(?i)\b(?:I\s+)?(?:should|need to|have to)\s+(?:refuse|decline|not\s+(?:help|assist))\b`, 0.7, "should_refuse", nil, 0.0},

		// --- Apologetic-only (very weak — needs other signals) ---

		{`(?i)\bI\s+(?:don'?t|do not)\s+(?:feel\s+)?comfortable\b`, 0.6, "not_comfortable", nil, 0.0},
		{`(?i)\bI\s+appreciate\s+(?:the\s+)?(?:question|request)[,.]\s+but\b`, 0.4, "appreciate_but", nil, 0.0},
	}
	for _, r := range raw {
		re, err := regexp.Compile(r.pattern)
		if err != nil {
			panic("llm: refusalPatterns: invalid regex: " + r.pattern + ": " + err.Error())
		}
		var suppressors []*regexp.Regexp
		if r.suppressor != nil {
			suppressors = []*regexp.Regexp{r.suppressor}
		}
		refusalPatterns = append(refusalPatterns, refusalPattern{
			re:                   re,
			weight:               r.weight,
			label:                r.label,
			suppressMatchIfAny:    suppressors,
			suppressReductionEach: r.suppressorReduction,
		})
	}
}

// RefusalSignal is the result of a refusal scan.
type RefusalSignal struct {
	// Detected is true if the response was classified as a refusal.
	Detected bool
	// Score is the sum of weights of patterns that matched.
	Score float32
	// Pattern is the label of the highest-weighted matching
	// pattern. Empty if Detected is false.
	Pattern string
	// Excerpt is a short excerpt of the response around the first
	// match, useful for audit. Capped at 200 chars.
	Excerpt string
}

// RefusalThreshold is the score at or above which a response is
// classified as a refusal. Tuned empirically: 0.5 catches the
// "I cannot help" / "I'm sorry but" / "as an AI I cannot"
// patterns without flagging legitimate "I cannot find this CVE"
// failures (which score ~0.3 from "as an AI" alone — below
// threshold).
const RefusalThreshold float32 = 0.5

// DetectRefusal scans text for refusal patterns. The result
// indicates whether the response should be treated as a refusal
// and the retry chain kicked off.
//
// The function is safe to call on any string (including empty).
// An empty string returns RefusalSignal{Detected: false}.
//
// The scoring works in two passes:
//   1. For each pattern, if its regex matches, add its weight
//      to the total. Record the label of the highest-weight
//      match (for the audit log).
//   2. For each pattern that matched, check its suppressMatchIfAny
//      list. If any suppressor matches, subtract
//      suppressReductionEach from the total for each match.
//      This handles the "I cannot find" case: the "i_cannot"
//      pattern fires, but the "legitimate_find" suppressor
//      also fires and brings the score below the threshold.
func DetectRefusal(text string) RefusalSignal {
	if text == "" {
		return RefusalSignal{}
	}

	var sig RefusalSignal
	firstMatchStart := -1
	var bestNet float32
	for _, p := range refusalPatterns {
		loc := p.re.FindStringIndex(text)
		if loc == nil {
			continue
		}
		// Apply suppressors first: a pattern matched but if
		// any of its suppressors ALSO match, the contribution
		// is reduced per match.
		net := p.weight
		for _, supp := range p.suppressMatchIfAny {
			matches := supp.FindAllStringIndex(text, -1)
			net -= float32(len(matches)) * p.suppressReductionEach
		}
		if net <= 0 {
			// Suppressor ate the entire match; this is a
			// legitimate phrase, not a refusal. Skip
			// contribution and pattern label.
			continue
		}
		sig.Score += net
		// Track the highest-net pattern label for the audit
		// log. Using net (not raw weight) ensures the label
		// we record is the one that actually contributed to
		// the refusal signal after suppressors.
		if net > bestNet {
			bestNet = net
			sig.Pattern = p.label
		}
		if firstMatchStart == -1 || loc[0] < firstMatchStart {
			firstMatchStart = loc[0]
		}
	}

	if sig.Score >= RefusalThreshold {
		sig.Detected = true
		if firstMatchStart >= 0 {
			start := firstMatchStart - 30
			if start < 0 {
				start = 0
			}
			end := firstMatchStart + 100
			if end > len(text) {
				end = len(text)
			}
			excerpt := strings.TrimSpace(text[start:end])
			if len(excerpt) > 200 {
				excerpt = excerpt[:200] + "..."
			}
			sig.Excerpt = excerpt
		}
	}
	return sig
}

// bestNetSoFar is the net weight of the pattern that already
// won the audit-log label. Tracked alongside sig.Pattern so we
// can decide which label to record when multiple patterns fire.
// Using net (not raw weight) is important: a pattern with raw
// weight 1.0 may have net 0.0 after suppressors — the audit
// log should record the pattern that actually contributed to
// the refusal signal, not the one that looks heaviest on paper.
var bestNetSoFar float32
