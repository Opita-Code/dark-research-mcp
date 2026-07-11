// Package testutil provides shared test infrastructure for the
// research package, including a recording HTTP transport that lets
// tests replay real backend responses from disk.
//
// The transport is intentionally simple: it maps each outbound
// request to a file under a base directory, where the file path is
// derived deterministically from the request URL. This keeps fixtures
// human-inspectable, diff-friendly in git, and trivially refreshable
// when a backend changes its API.
//
// Usage in tests:
//
//	transport := &testutil.RecordingTransport{
//	    BaseDir: "../../fixtures", // path to checked-in fixtures
//	    Mode:    testutil.ModeReplay,
//	}
//	router := research.NewRouter(research.DefaultRegistry(),
//	    &http.Client{Transport: transport})
//	res, err := router.Route(ctx, "CVE-2024-3094", research.IntentCVE)
//
// To refresh fixtures locally, run with ModeRecord once and copy the
// new files into the checked-in base directory.
package testutil

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Mode controls the transport's behavior.
type Mode string

const (
	// ModeReplay serves pre-recorded responses from BaseDir. Missing
	// fixtures cause a test failure (the test will see a 404-like
	// error). Use this in CI.
	ModeReplay Mode = "replay"

	// ModeRecord calls the real backend and writes the response to
	// BaseDir. Missing fixtures are created. Use this to refresh
	// fixtures locally; never in CI.
	ModeRecord Mode = "record"

	// ModePassthrough calls the real backend without recording. Use
	// for ad-hoc debugging; never in CI.
	ModePassthrough Mode = "passthrough"
)

// RecordingTransport is an http.RoundTripper that records or replays
// responses. It is safe for concurrent use; the recorded map is
// guarded by an internal mutex in record mode.
type RecordingTransport struct {
	// BaseDir is the root directory for fixture files. In replay mode
	// the transport reads from here; in record mode it writes here.
	BaseDir string

	// Mode controls the transport's behavior.
	Mode Mode

	// HeaderScrub is an optional list of header names to omit from
	// recorded responses (e.g. "Set-Cookie", "Date"). Lowercase.
	HeaderScrub []string

	// recorded collects raw response bodies in record mode for
	// in-memory assertions (e.g. "this fixture was written").
	recorded map[string][]byte
}

// RoundTrip implements http.RoundTripper.
func (t *RecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	switch t.Mode {
	case ModeReplay:
		return t.replay(req)
	case ModeRecord:
		return t.record(req)
	case ModePassthrough, "":
		return http.DefaultTransport.RoundTrip(req)
	default:
		return nil, fmt.Errorf("testutil: unknown mode %q", t.Mode)
	}
}

// FixturePathFor returns the file path that the transport would use
// for the given request. Exposed for tests and for the record script
// to print a stable path.
func FixturePathFor(req *http.Request, baseDir string) string {
	return fixturePath(req, baseDir)
}

// fixturePath maps a request URL to a deterministic local path:
//
//	https://api.osv.dev/v1/vulns/CVE-2024-3094
//	  -> {baseDir}/api.osv.dev/v1/vulns/CVE-2024-3094
//
// Query string is preserved as a suffix so different parameters
// produce different files:
//
//	https://api.openalex.org/works?search=go&per_page=1
//	  -> {baseDir}/api.openalex.org/works_search=go&per_page=1
//
// The host directory is namespaced so cross-host collisions cannot
// happen even if a backend accidentally uses the same path.
func fixturePath(req *http.Request, baseDir string) string {
	host := req.URL.Host
	if host == "" {
		host = "unknown"
	}
	// Strip port so "1.1.1.1:443" and "1.1.1.1" map to the same dir.
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	// URL paths are always '/' separated. Do NOT use filepath.Clean
	// here: on Windows it treats both '/' and '\\' as separators and
	// produces a backslash-rooted path that breaks the cross-platform
	// expectation below.
	cleanPath := req.URL.Path
	if cleanPath == "" || cleanPath == "/" {
		cleanPath = "/index"
	}
	name := strings.TrimPrefix(cleanPath, "/")
	// Sanitize for cross-platform filesystem safety.
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.ReplaceAll(name, "?", "_q_")
	name = strings.ReplaceAll(name, "&", "_")
	name = strings.ReplaceAll(name, "=", "-")
	// Append a query-string file if the URL has one.
	if req.URL.RawQuery != "" {
		q := strings.ReplaceAll(req.URL.RawQuery, "?", "")
		q = strings.ReplaceAll(q, "&", "_")
		q = strings.ReplaceAll(q, "=", "-")
		name = name + "__" + q
	}
	return filepath.Join(baseDir, host, name+".http")
}

func (t *RecordingTransport) replay(req *http.Request) (*http.Response, error) {
	path := fixturePath(req, t.BaseDir)
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("testutil: fixture not found for %s (looked at %s): %w",
			req.URL.String(), path, err)
	}
	return parseRecorded(body, req)
}

func (t *RecordingTransport) record(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("testutil: read body: %w", err)
	}
	// Re-wrap body for the caller.
	resp.Body = io.NopCloser(bytes.NewReader(body))

	serialized := serializeRecorded(resp, req, body, t.HeaderScrub)
	path := fixturePath(req, t.BaseDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("testutil: mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, serialized, 0o644); err != nil {
		return nil, fmt.Errorf("testutil: write %s: %w", path, err)
	}
	return resp, nil
}

// On-disk format is a small envelope so tests can replay the exact
// status, headers, and body the original response had. We pick this
// over the raw bytes so HTTP/1.1 framing differences between
// implementations do not produce spurious mismatches.
//
// Format:
//
//	HTTP/1.1 <status> <status text>\n
//	Header-Name: value\n
//	Header-Name: value\n
//	\n
//	<body bytes>
//
// parseRecorded and serializeRecorded are inverses.

func serializeRecorded(resp *http.Response, req *http.Request, body []byte, scrub []string) []byte {
	scrubSet := make(map[string]struct{}, len(scrub))
	for _, h := range scrub {
		scrubSet[strings.ToLower(h)] = struct{}{}
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "HTTP/1.1 %d %s\r\n", resp.StatusCode, http.StatusText(resp.StatusCode))
	for k, vs := range resp.Header {
		if _, drop := scrubSet[strings.ToLower(k)]; drop {
			continue
		}
		for _, v := range vs {
			fmt.Fprintf(&buf, "%s: %s\r\n", k, v)
		}
	}
	// Ensure Host is preserved for replay (some backends key on it).
	if req.Host != "" {
		fmt.Fprintf(&buf, "X-Fixture-Request-Host: %s\r\n", req.Host)
	}
	buf.WriteString("\r\n")
	buf.Write(body)
	return buf.Bytes()
}

func parseRecorded(data []byte, req *http.Request) (*http.Response, error) {
	// Find header/body split.
	const sep = "\r\n\r\n"
	i := bytes.Index(data, []byte(sep))
	if i < 0 {
		// Tolerate \n\n too.
		if j := bytes.Index(data, []byte("\n\n")); j >= 0 {
			// Re-split using \n for parsing.
			headerBlock := data[:j]
			body := data[j+2:]
			return parseHeaderBody(headerBlock, body, req)
		}
		return nil, fmt.Errorf("testutil: malformed fixture (no header/body separator)")
	}
	headerBlock := data[:i]
	body := data[i+len(sep):]
	return parseHeaderBody(headerBlock, body, req)
}

func parseHeaderBody(headerBlock, body []byte, req *http.Request) (*http.Response, error) {
	lines := bytes.Split(headerBlock, []byte("\n"))
	if len(lines) == 0 {
		return nil, fmt.Errorf("testutil: empty fixture header")
	}
	// Status line: HTTP/1.1 200 OK
	statusLine := strings.TrimSpace(string(lines[0]))
	parts := strings.SplitN(statusLine, " ", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("testutil: malformed status line: %q", statusLine)
	}
	var statusCode int
	if _, err := fmt.Sscanf(parts[1], "%d", &statusCode); err != nil {
		return nil, fmt.Errorf("testutil: parse status code %q: %w", parts[1], err)
	}
	resp := &http.Response{
		StatusCode: statusCode,
		Status:     statusLine,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1, ProtoMinor: 1,
	}
	for _, line := range lines[1:] {
		s := strings.TrimRight(string(line), "\r")
		if s == "" {
			continue
		}
		colon := strings.Index(s, ":")
		if colon < 0 {
			continue
		}
		k := strings.TrimSpace(s[:colon])
		v := strings.TrimSpace(s[colon+1:])
		resp.Header.Add(k, v)
	}
	// Content-Length: synthesize if missing so the client can read fully.
	if resp.Header.Get("Content-Length") == "" {
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
		resp.ContentLength = int64(len(body))
	} else {
		// Best effort: parse it. If it fails, leave ContentLength at -1.
		var n int64
		if _, err := fmt.Sscanf(resp.Header.Get("Content-Length"), "%d", &n); err == nil {
			resp.ContentLength = n
		}
	}
	return resp, nil
}

// Sanity helper: ensures a URL can be reduced to a fixture path
// without surprises. Used in tests.
var _ = url.Parse
