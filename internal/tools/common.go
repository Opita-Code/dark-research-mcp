package tools

import (
	"encoding/json"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/dark-agents/research-mcp/internal/safety"
	"github.com/mark3labs/mcp-go/mcp"
)

// bindArgs unmarshals the tool-call arguments into target. The mark3labs SDK
// hands us a CallToolRequest; we route through JSON to keep handlers
// independent of the SDK's argument representation.
func bindArgs(req mcp.CallToolRequest, target any) error {
	raw, err := json.Marshal(req.Params.Arguments)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

// jsonResult wraps v as a JSON-text tool result so the LLM gets a stable
// string payload. Structured content is set when the SDK supports it.
func jsonResult(v any) *mcp.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return textResult("internal error: " + err.Error())
	}
	res := mcp.NewToolResultText(string(b))
	return res
}

// textResult is the simple text wrapper.
func textResult(s string) *mcp.CallToolResult {
	return mcp.NewToolResultText(s)
}

// clampInt coerces v into [lo, hi], returning def when v is zero.
func clampInt(v, lo, hi, def int) int {
	if v == 0 {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// validateURL is a thin wrapper around safety.ValidateURL so tools can
// stay decoupled from the safety package's API surface.
func validateURL(raw string, allowLoopback bool) (*url.URL, error) {
	return safety.ValidateURL(raw, allowLoopback)
}

// getenv is a tiny indirection so tests can stub the environment without
// pulling in os.Setenv in every handler signature. The default impl reads
// from os.Getenv; tests override this var.
var getenv = os.Getenv

// ---- text_anonymize implementation ----

var (
	reEmail  = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	reIPv4   = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	rePhone  = regexp.MustCompile(`\b\+?\d[\d\s\-().]{7,}\d\b`)
	reDomain = regexp.MustCompile(`\b([a-zA-Z0-9\-]+\.)+[a-zA-Z]{2,}\b`)
)

func anonymizeText(text string, entityTypes []string) textAnonymizeOutput {
	want := func(name string) bool {
		if len(entityTypes) == 0 {
			return true
		}
		for _, e := range entityTypes {
			if strings.EqualFold(e, name) {
				return true
			}
		}
		return false
	}

	counts := map[string]int{}
	out := text

	if want("email") {
		out, counts["email"] = maskAll(out, reEmail, "[EMAIL]")
	}
	if want("ip") {
		out, counts["ip"] = maskAll(out, reIPv4, func(m string) string {
			// Don't touch things that look like version numbers or years.
			if looksLikeVersion(m) {
				return m
			}
			return "[IP]"
		})
	}
	if want("domain") {
		// Domain regex overlaps with email; run after email so emails are
		// already masked and won't be double-hit.
		out, counts["domain"] = maskAll(out, reDomain, "[DOMAIN]")
	}
	if want("phone") {
		out, counts["phone"] = maskAll(out, rePhone, "[PHONE]")
	}

	return textAnonymizeOutput{Anonymized: out, Replacements: counts}
}

func maskAll(s string, re *regexp.Regexp, replace any) (string, int) {
	n := 0
	out := re.ReplaceAllStringFunc(s, func(m string) string {
		n++
		if r, ok := replace.(string); ok {
			return r
		}
		if fn, ok := replace.(func(string) string); ok {
			return fn(m)
		}
		return m
	})
	return out, n
}

func looksLikeVersion(s string) bool {
	// "1.2.3", "10.0", etc. — segments all ≤ 999.
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return false
	}
	for _, p := range parts {
		if len(p) == 0 || len(p) > 3 {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}