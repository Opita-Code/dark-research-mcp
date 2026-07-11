// Command sha256sums reads every regular file under dist/ (except
// SHA256SUMS.txt itself), computes its SHA-256, and writes a sorted
// `dist/SHA256SUMS.txt` in the standard `sha256sum -a 256` format:
//
//   <hex-digest>  <filename>\n
//
// Used by the release workflow (see ../.github/workflows/go-test.yml
// and the cross-platform build script in the parent directory's
// release notes) so that anyone can verify the published binaries
// against a known checksum.
//
// Usage:
//
//   go run scripts/sha256sums.go
//
// Reads from ./dist, writes to ./dist/SHA256SUMS.txt.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	const (
		dir = "dist"
		out = "dist/SHA256SUMS.txt"
	)

	entries, err := os.ReadDir(dir)
	if err != nil {
		die("read %s: %v", dir, err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if n == "SHA256SUMS.txt" {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)

	var sb strings.Builder
	for _, name := range names {
		sum, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			die("hash %s: %v", name, err)
		}
		sb.WriteString(sum)
		sb.WriteString("  ")
		sb.WriteString(name)
		sb.WriteByte('\n')
	}

	if err := os.WriteFile(out, []byte(sb.String()), 0o644); err != nil {
		die("write %s: %v", out, err)
	}
	fmt.Fprintf(os.Stderr, "sha256sums: wrote %s (%d entries)\n", out, len(names))
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "sha256sums: "+format+"\n", args...)
	os.Exit(1)
}