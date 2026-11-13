//go:build ignore

// Command trim-fixture reduces a recorded HTML fixture to its first N
// <li class="result"> blocks.
//
// Some upstreams return an unbounded result list (Ahmia returns ~1900
// results for a single query, 1.5 MB of HTML). Committing that verbatim
// bloats the repository for no testing benefit: the fixtures only need
// to prove that the parser handles the real markup.
//
// The trimmed body keeps the page envelope intact (doctype, header,
// the <ol class="searchResults"> opening, the first N result blocks and
// the closing tags), so it exercises exactly the same parser path as
// the full response.
//
// Usage:
//
//	go run ./scripts/trim-fixture.go <file.http> <keepN>
package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var resultStart = regexp.MustCompile(`(?is)<li[^>]*class="result"[^>]*>`)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: trim-fixture <file.http> <keepN>")
		os.Exit(2)
	}
	keep, err := strconv.Atoi(os.Args[2])
	if err != nil || keep < 1 {
		fmt.Fprintln(os.Stderr, "keepN must be a positive integer")
		os.Exit(2)
	}

	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data := string(raw)

	// Split the HTTP envelope from the body.
	sep := "\r\n\r\n"
	idx := strings.Index(data, sep)
	if idx < 0 {
		sep = "\n\n"
		idx = strings.Index(data, sep)
	}
	if idx < 0 {
		fmt.Fprintln(os.Stderr, "no header/body separator")
		os.Exit(1)
	}
	header := data[:idx]
	body := data[idx+len(sep):]

	locs := resultStart.FindAllStringIndex(body, -1)
	if len(locs) <= keep {
		fmt.Printf("%s: already %d results, nothing to trim\n", os.Args[1], len(locs))
		return
	}

	// Keep the first `keep` result blocks.
	cut := locs[keep][0]
	tail := ""
	if i := strings.Index(body[cut:], "</ol>"); i >= 0 {
		tail = body[cut+i:]
	}
	trimmed := body[:cut] + tail

	// Rewrite the Content-Length header so the replayed response is
	// consistent with the trimmed body.
	trimmedBody := strings.ReplaceAll(trimmed, "\r\n", "\n")
	lines := strings.Split(header, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.ToLower(l), "content-length:") {
			lines[i] = fmt.Sprintf("Content-Length: %d", len(trimmedBody))
		}
	}
	newHeader := strings.Join(lines, "\n")

	out := newHeader + sep + trimmed
	if err := os.WriteFile(os.Args[1], []byte(out), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%s: trimmed %d -> %d results (%d bytes -> %d bytes)\n",
		os.Args[1], len(locs), keep, len(raw), len(out))
}
