package tools

import (
	"regexp"
	"strings"
)

// RE2 (Go's regex engine) does not support backreferences, so each block
// tag gets its own pre-compiled pattern. stripBlocks iterates them all.
var reStripBlocks = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`),
	regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style\s*>`),
	regexp.MustCompile(`(?is)<noscript\b[^>]*>.*?</noscript\s*>`),
}

var reTag = regexp.MustCompile(`(?s)<[^>]*>`)
var reWS = regexp.MustCompile(`\s+`)

func stripBlocks(s string, names []string) string {
	// Build a dedicated regex per name. Cheaper than one giant alternation
	// when the call site passes a few tags.
	for _, n := range names {
		pattern := `(?is)<` + regexp.QuoteMeta(n) + `\b[^>]*>.*?</` + regexp.QuoteMeta(n) + `\s*>`
		re := regexp.MustCompile(pattern)
		s = re.ReplaceAllString(s, "")
	}
	return s
}

// indexCI is a case-insensitive strings.Index starting at from.
func indexCI(s, substr string, from int) int {
	if from < 0 {
		from = 0
	}
	if from >= len(s) {
		return -1
	}
	low := strings.ToLower(s[from:])
	lowSub := strings.ToLower(substr)
	return strings.Index(low, lowSub)
}

func stripTags(s string) string {
	return reTag.ReplaceAllString(s, "")
}

func collapseWS(s string) string {
	return strings.TrimSpace(reWS.ReplaceAllString(s, " "))
}