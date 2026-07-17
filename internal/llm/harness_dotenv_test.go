// Tests for the harness .env loader and the Complete() fallback chain.
//
// These tests reset the dotenvCache via the test-only ResetDotenvCache()
// helper, so each test gets a fresh load. The harness config file
// detection is environment-dependent, so the .env tests use a temp
// directory; the harness config tests inject a fake home / APPDATA
// path through the same test-only hook.
package llm

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// resetDotenvCache clears the LoadHarnessDotenv memo. Tests call
// this before exercising the loader so each test gets a fresh read.
func resetDotenvCache() {
	dotenvOnce = sync.Once{}
	dotenvCache = nil
}

func TestLoadDotenvFile_KeyValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(`
# This is a comment
ANTHROPIC_API_KEY=sk-test-1234
OPENAI_API_KEY="sk-openai-5678"
MINIMAX_API_KEY='sk-minimax-9012'
EMPTY_KEY=
JUST_A_KEY
export EXPORTED_KEY=sk-exported
`), 0644); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	loadDotenvFile(path, out)

	if out["ANTHROPIC_API_KEY"] != "sk-test-1234" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want sk-test-1234", out["ANTHROPIC_API_KEY"])
	}
	if out["OPENAI_API_KEY"] != "sk-openai-5678" {
		t.Errorf("OPENAI_API_KEY = %q, want sk-openai-5678 (quotes stripped)", out["OPENAI_API_KEY"])
	}
	if out["MINIMAX_API_KEY"] != "sk-minimax-9012" {
		t.Errorf("MINIMAX_API_KEY = %q, want sk-minimax-9012 (single quotes stripped)", out["MINIMAX_API_KEY"])
	}
	if out["EMPTY_KEY"] != "" {
		t.Errorf("EMPTY_KEY = %q, want empty (empty values skipped)", out["EMPTY_KEY"])
	}
	if _, ok := out["JUST_A_KEY"]; ok {
		t.Errorf("JUST_A_KEY present, want absent (no = sign)")
	}
	if out["EXPORTED_KEY"] != "sk-exported" {
		t.Errorf("EXPORTED_KEY = %q, want sk-exported (export prefix stripped)", out["EXPORTED_KEY"])
	}
}

func TestLoadDotenvFile_Missing(t *testing.T) {
	out := map[string]string{}
	loadDotenvFile("/nonexistent/.env", out) // should not panic
	if len(out) != 0 {
		t.Errorf("missing file should leave out empty, got %v", out)
	}
}

func TestStripJSONCComments(t *testing.T) {
	in := `{
		// line comment
		"key1": "value1",
		"key2": "value2", // trailing
		/* block
		   comment */
		"key3": "value3"
	}`
	want := `{
		
		"key1": "value1",
		"key2": "value2", 
		
		"key3": "value3"
	}`
	got := stripJSONCComments(in)
	// Compare line-by-line to avoid whitespace fragility.
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(want, "\n")
	if len(gotLines) != len(wantLines) {
		t.Fatalf("line count: got %d, want %d\ngot:\n%s\nwant:\n%s", len(gotLines), len(wantLines), got, want)
	}
	for i, line := range gotLines {
		if strings.TrimSpace(line) != strings.TrimSpace(wantLines[i]) {
			t.Errorf("line %d:\n  got:  %q\n  want: %q", i, line, wantLines[i])
		}
	}
}

func TestIsNetworkError(t *testing.T) {
	tests := []struct {
		err      error
		netError bool
	}{
		{nil, false},
		{&net.OpError{Op: "dial", Err: errorString("connection refused")}, true},
		{errorString("dial tcp 127.0.0.1:8901: connectex: No connection could be made"), true},
		{errorString("Post \"http://x/v1\": dial tcp: lookup x: no such host"), true},
		{errorString("llm: http 401: invalid x-api-key"), false}, // 4xx is NOT a network error
		{errorString("llm: http 503: backend down"), false},     // 5xx is NOT a network error (caught by shouldFallback)
	}
	for _, tc := range tests {
		got := isNetworkError(tc.err)
		if got != tc.netError {
			t.Errorf("isNetworkError(%v) = %v, want %v", tc.err, got, tc.netError)
		}
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }

// TestComplete_FallsBackToHarnessOnNetworkError verifies the new
// ultimate-fallback path: when the scrapper daemon is down
// (connection refused) and the operator has a direct-provider key
// reachable via the parent harness's env, Complete() retries against
// the direct provider and returns the verdict.
func TestComplete_FallsBackToHarnessOnNetworkError(t *testing.T) {
	// Stand up a fake Anthropic-compatible endpoint that returns a
	// valid response. This is the "direct provider" path; the
	// scrapper path will be the dead test server below.
	var directCalled bool
	directSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"verdict: aligned"}]}`))
	}))
	defer directSrv.Close()

	// Stand up a "scrapper" that is OFFLINE (listener accepts then
	// immediately closes, so callers get connection refused). We use
	// httptest.NewUnstartedServer + manual listener control.
	dead := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead.Listener.Close() // force connection refused
	_ = dead              // never started; we only use it for the URL
	// The scrapper URL we point at must be a port that is NOT listening.
	// 127.0.0.1:1 is conventionally closed and refuses fast.
	scrapperURL := "http://127.0.0.1:1"

	c := &Client{
		BaseURL:  scrapperURL,
		APIKey:   "ds-managed",
		Model:    "MiniMax-M3",
		Provider: ProviderAnthropic,
		HTTP:     &http.Client{Timeout: 5 * 1e9}, // 5s in nanoseconds
		HarnessDotenvKey:      "sk-direct-1234",
		HarnessDotenvProvider: ProviderAnthropic,
	}
	// Override the direct provider's BaseURL via env so the test
	// doesn't hardcode api.anthropic.com (which would actually work
	// if the key were real, but here it's a fake). We do this by
	// reaching into the env-loaded client config: rewire after
	// NewFromEnv would normally do it. For this test, we construct
	// the Client manually and override the base URL on the local
	// copy inside Complete.
	os.Setenv("SDD_LLM_BASE_URL", directSrv.URL)
	defer os.Unsetenv("SDD_LLM_BASE_URL")

	text, err := c.Complete(context.Background(), "system", Message{Role: "user", Content: "ping"})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if !strings.Contains(text, "verdict: aligned") {
		t.Errorf("Complete() text = %q, want substring 'verdict: aligned'", text)
	}
	if !directCalled {
		t.Errorf("direct provider was never called; fallback chain didn't fire")
	}
}
