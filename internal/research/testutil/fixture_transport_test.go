package testutil

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFixturePathFor verifies the deterministic URL → path mapping.
func TestFixturePathFor(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "simple path",
			url:  "https://api.osv.dev/v1/vulns/CVE-2024-3094",
			want: filepath.Join("base", "api.osv.dev", "v1", "vulns", "CVE-2024-3094.http"),
		},
		{
			name: "root path",
			url:  "https://example.com/",
			want: filepath.Join("base", "example.com", "index.http"),
		},
		{
			name: "query string appended",
			url:  "https://api.openalex.org/works?search=go&per_page=1",
			want: filepath.Join("base", "api.openalex.org",
				"works__search-go_per_page-1.http"),
		},
		{
			name: "host with port stripped",
			url:  "https://1.1.1.1:443/dns-query?name=example.com&type=A",
			want: filepath.Join("base", "1.1.1.1",
				"dns-query__name-example.com_type-A.http"),
		},
		{
			name: "colon in path escaped",
			url:  "https://api.example.com/lookup:CVE-2024-3094",
			want: filepath.Join("base", "api.example.com",
				"lookup_CVE-2024-3094.http"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", c.url, nil)
			if err != nil {
				t.Fatalf("parse url: %v", err)
			}
			got := FixturePathFor(req, "base")
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestRecordReplayRoundTrip writes a fixture, then replays it through
// a fresh transport and asserts status, headers, and body match.
func TestRecordReplayRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Record.
	record := &RecordingTransport{BaseDir: dir, Mode: ModeRecord}
	recReq, _ := http.NewRequest("GET",
		"https://api.osv.dev/v1/vulns/CVE-2024-3094", nil)
	recResp, err := record.RoundTrip(recReq)
	if err != nil {
		// The CI sandbox may not have network. Skip cleanly.
		if isNetworkError(err) {
			t.Skipf("no network in this environment: %v", err)
		}
		t.Fatalf("record: %v", err)
	}
	if recResp.StatusCode != 200 {
		t.Fatalf("expected 200 from OSV, got %d", recResp.StatusCode)
	}
	recResp.Body.Close()

	// Verify a fixture file was written.
	entries, err := os.ReadDir(filepath.Join(dir, "api.osv.dev"))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no fixture files written")
	}

	// Replay.
	replay := &RecordingTransport{BaseDir: dir, Mode: ModeReplay}
	repReq, _ := http.NewRequest("GET",
		"https://api.osv.dev/v1/vulns/CVE-2024-3094", nil)
	repResp, err := replay.RoundTrip(repReq)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	defer repResp.Body.Close()
	if repResp.StatusCode != 200 {
		t.Errorf("replay status: got %d, want 200", repResp.StatusCode)
	}
}

// TestReplay_MissingFixture fails the roundtrip cleanly when the
// fixture file is absent.
func TestReplay_MissingFixture(t *testing.T) {
	dir := t.TempDir()
	tr := &RecordingTransport{BaseDir: dir, Mode: ModeReplay}
	req, _ := http.NewRequest("GET", "https://api.osv.dev/missing", nil)
	_, err := tr.RoundTrip(req)
	if err == nil {
		t.Fatalf("expected error for missing fixture, got nil")
	}
	if !strings.Contains(err.Error(), "fixture not found") {
		t.Errorf("expected 'fixture not found' error, got %v", err)
	}
}

// TestReplay_FromHandWrittenFixture uses a hand-crafted envelope to
// confirm the parser handles the documented format.
func TestReplay_FromHandWrittenFixture(t *testing.T) {
	dir := t.TempDir()
	// Build a minimal valid envelope.
	hostDir := filepath.Join(dir, "example.com")
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		t.Fatal(err)
	}
	envelope := "HTTP/1.1 200 OK\r\n" +
		"Content-Type: application/json\r\n" +
		"X-Custom: hello\r\n" +
		"\r\n" +
		`{"hello":"world"}`
	path := filepath.Join(hostDir, "hello.http")
	if err := os.WriteFile(path, []byte(envelope), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := &RecordingTransport{BaseDir: dir, Mode: ModeReplay}
	req, _ := http.NewRequest("GET", "https://example.com/hello", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Custom"); got != "hello" {
		t.Errorf("X-Custom: got %q, want %q", got, "hello")
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type: got %q", got)
	}
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	if string(buf[:n]) != `{"hello":"world"}` {
		t.Errorf("body: got %q, want %q", buf[:n], `{"hello":"world"}`)
	}
}

// TestScrubHeaders verifies that HeaderScrub removes the named
// header from recorded fixtures.
func TestScrubHeaders(t *testing.T) {
	dir := t.TempDir()
	rec := &RecordingTransport{
		BaseDir:     dir,
		Mode:        ModeRecord,
		HeaderScrub: []string{"date", "set-cookie"},
	}
	recReq, _ := http.NewRequest("GET", "https://example.com/scrub", nil)
	recResp, err := rec.RoundTrip(recReq)
	if err != nil {
		if isNetworkError(err) {
			t.Skipf("no network: %v", err)
		}
		t.Fatalf("record: %v", err)
	}
	recResp.Body.Close()

	// Read the fixture and assert no Date/Set-Cookie lines.
	hostDir := filepath.Join(dir, "example.com")
	entries, _ := os.ReadDir(hostDir)
	if len(entries) == 0 {
		t.Fatal("no fixture")
	}
	data, _ := os.ReadFile(filepath.Join(hostDir, entries[0].Name()))
	body := string(data)
	if strings.Contains(strings.ToLower(body), "date:") {
		t.Errorf("Date header not scrubbed: %s", body)
	}
	if strings.Contains(strings.ToLower(body), "set-cookie:") {
		t.Errorf("Set-Cookie header not scrubbed: %s", body)
	}
}

// TestPassthroughMode bypasses the recorder entirely.
func TestPassthroughMode(t *testing.T) {
	tr := &RecordingTransport{BaseDir: "/nonexistent", Mode: ModePassthrough}
	req, _ := http.NewRequest("GET", "https://example.com/passthrough", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		if isNetworkError(err) {
			t.Skipf("no network: %v", err)
		}
		t.Fatalf("passthrough: %v", err)
	}
	resp.Body.Close()
}

// TestUnknownMode is defensive against typos in Mode.
func TestUnknownMode(t *testing.T) {
	tr := &RecordingTransport{BaseDir: "/tmp", Mode: "bogus"}
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	if _, err := tr.RoundTrip(req); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

// isNetworkError returns true for errors that look like a missing
// network connection. Tests use this to skip cleanly in airgapped CI.
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "no such host") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "network is unreachable") ||
		strings.Contains(s, "dial tcp: lookup")
}

// Compile-time check that http.Request construction works under
// url.Parse (catches accidental import drift).
var _ = url.Parse
