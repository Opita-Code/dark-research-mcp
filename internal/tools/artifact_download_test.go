package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dark-agents/research-mcp/internal/config"
	"github.com/dark-agents/research-mcp/internal/mem"
	"github.com/mark3labs/mcp-go/mcp"
)

// installSharedMem attaches s to the package-level shared state for the
// duration of the test. Returns a cleanup func that resets it.
func installSharedMem(t *testing.T, s *mem.Store) func() {
	t.Helper()
	prev := shared.mem
	shared.mem = s
	return func() { shared.mem = prev }
}

// downloadTestClients returns a clients struct with the safety defaults
// that match production (5 MB cap, 200k chars), AND with a fully wired
// clearnet *httpClient so the handler's DoContext call has something to
// dispatch through.
//
// AllowLoopback defaults to TRUE here because httptest.NewServer listens
// on 127.0.0.1. The SSRF-guard test explicitly constructs its own
// client with AllowLoopback=false to exercise the rejection path.
func downloadTestClients() *clients {
	cfg := config.Config{
		Safety: config.SafetyConfig{
			MaxResponseBytes: 5_000_000,
			MaxOutputChars:   200_000,
			AllowLoopback:    true,
		},
	}
	return newClients(cfg)
}

// callArtifactDownload invokes the artifact_download handler with the
// given arg map and returns the parsed JSON result.
func callArtifactDownload(t *testing.T, c *clients, args map[string]any) map[string]any {
	t.Helper()
	tool := artifactDownloadTool(c)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "dark_research_artifact_download",
			Arguments: args,
		},
	}
	res, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil {
		t.Fatal("handler returned nil result")
	}
	// Each TextResult has a single text payload with the JSON body.
	if len(res.Content) == 0 {
		t.Fatal("handler returned empty Content")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("handler Content[0] is %T, want TextContent", res.Content[0])
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &out); err != nil {
		t.Fatalf("result not JSON: %v\npayload: %s", err, tc.Text)
	}
	return out
}

// freshMemStore returns an in-memory Store (Open("") uses :memory:).
func freshMemStore(t *testing.T) *mem.Store {
	t.Helper()
	s, err := mem.Open("")
	if err != nil {
		t.Fatalf("mem.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestArtifactDownload_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("# hello from httptest\n\nbody content"))
	}))
	t.Cleanup(srv.Close)

	store := freshMemStore(t)
	defer installSharedMem(t, store)()

	id, err := store.SaveArtifact(context.Background(), &mem.Artifact{
		VibeCase:     "C2",
		ArtifactURL:  srv.URL,
		ArtifactType: "text",
		BrandID:      "acme-2026",
	})
	if err != nil {
		t.Fatalf("SaveArtifact: %v", err)
	}

	out := callArtifactDownload(t, downloadTestClients(), map[string]any{
		"artifact_id": id,
	})

	if got, _ := out["downloaded"].(bool); !got {
		t.Errorf("downloaded: got %v, want true (full out: %v)", out["downloaded"], out)
	}
	if got, _ := out["http_status"].(float64); int(got) != 200 {
		t.Errorf("http_status: got %v, want 200", out["http_status"])
	}
	if got, _ := out["bytes"].(float64); int(got) == 0 {
		t.Error("bytes: got 0, want >0")
	}
	if got, _ := out["truncated"].(bool); got {
		t.Error("truncated: got true, want false (content fits in default 20000 chars)")
	}
	if got, _ := out["content"].(string); !strings.Contains(got, "hello from httptest") {
		t.Errorf("content missing expected text, got: %q", got)
	}
	if got, _ := out["url"].(string); got != srv.URL {
		t.Errorf("url: got %q, want %q", got, srv.URL)
	}
	if got, _ := out["brand_id"].(string); got != "acme-2026" {
		t.Errorf("brand_id: got %q, want acme-2026", got)
	}
	if got, _ := out["vibe_case"].(string); got != "C2" {
		t.Errorf("vibe_case: got %q, want C2", got)
	}
}

func TestArtifactDownload_NotFound(t *testing.T) {
	store := freshMemStore(t)
	defer installSharedMem(t, store)()

	out := callArtifactDownload(t, downloadTestClients(), map[string]any{
		"artifact_id": int64(999999),
	})
	if got, _ := out["found"].(bool); got {
		t.Errorf("found: got true, want false (artifact_id 999999 should not exist)")
	}
}

func TestArtifactDownload_NoURL(t *testing.T) {
	store := freshMemStore(t)
	defer installSharedMem(t, store)()

	id, err := store.SaveArtifact(context.Background(), &mem.Artifact{
		VibeCase:     "C4",
		ArtifactType: "video",
		// ArtifactURL intentionally empty
	})
	if err != nil {
		t.Fatalf("SaveArtifact: %v", err)
	}

	out := callArtifactDownload(t, downloadTestClients(), map[string]any{
		"artifact_id": id,
	})
	if got, _ := out["downloaded"].(bool); got {
		t.Errorf("downloaded: got true, want false (no URL)")
	}
	if got, _ := out["reason"].(string); got == "" {
		t.Error("reason: got empty string, want non-empty explanation")
	}
}

func TestArtifactDownload_TruncatedByMaxLength(t *testing.T) {
	// Server returns a body larger than the requested max_length.
	big := strings.Repeat("X", 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	t.Cleanup(srv.Close)

	store := freshMemStore(t)
	defer installSharedMem(t, store)()

	id, err := store.SaveArtifact(context.Background(), &mem.Artifact{
		VibeCase:     "C2",
		ArtifactURL:  srv.URL,
		ArtifactType: "text",
	})
	if err != nil {
		t.Fatalf("SaveArtifact: %v", err)
	}

	out := callArtifactDownload(t, downloadTestClients(), map[string]any{
		"artifact_id": id,
		"max_length":  100,
	})
	if got, _ := out["truncated"].(bool); !got {
		t.Errorf("truncated: got false, want true (max_length=100, body=5000)")
	}
	if got := out["content"].(string); len(got) != 100 {
		t.Errorf("content length: got %d, want 100", len(got))
	}
}

func TestArtifactDownload_SSRFGuard(t *testing.T) {
	// Safety guard should reject loopback addresses when AllowLoopback=false.
	// 127.0.0.1 is loopback → must be rejected. Construct a dedicated
	// client with AllowLoopback=false; the default helper has it set true
	// so the happy-path tests can use httptest.NewServer.
	store := freshMemStore(t)
	defer installSharedMem(t, store)()

	cfg := config.Config{
		Safety: config.SafetyConfig{
			MaxResponseBytes: 5_000_000,
			MaxOutputChars:   200_000,
			AllowLoopback:    false,
		},
	}
	strictClient := newClients(cfg)

	id, err := store.SaveArtifact(context.Background(), &mem.Artifact{
		VibeCase:     "C2",
		ArtifactURL:  "http://127.0.0.1:9999/internal",
		ArtifactType: "text",
	})
	if err != nil {
		t.Fatalf("SaveArtifact: %v", err)
	}

	out := callArtifactDownload(t, strictClient, map[string]any{
		"artifact_id": id,
	})
	if got, _ := out["downloaded"].(bool); got {
		t.Error("downloaded: got true, want false (SSRF guard must reject loopback)")
	}
	if got, _ := out["reason"].(string); !strings.Contains(strings.ToLower(got), "ssrf") {
		t.Errorf("reason: got %q, want substring 'ssrf'", got)
	}
}

func TestArtifactDownload_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("kaboom"))
	}))
	t.Cleanup(srv.Close)

	store := freshMemStore(t)
	defer installSharedMem(t, store)()

	id, err := store.SaveArtifact(context.Background(), &mem.Artifact{
		VibeCase:     "C2",
		ArtifactURL:  srv.URL,
		ArtifactType: "text",
	})
	if err != nil {
		t.Fatalf("SaveArtifact: %v", err)
	}

	// The handler returns a Go error (not a downloaded=false payload) when
	// the upstream returns non-2xx, so we need to call directly and assert
	// the error.
	tool := artifactDownloadTool(downloadTestClients())
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "dark_research_artifact_download",
			Arguments: map[string]any{"artifact_id": id},
		},
	}
	res, err := tool.Handler(context.Background(), req)
	if err == nil {
		t.Fatalf("expected error for HTTP 500, got result: %+v", res)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error message: got %q, want substring '500'", err.Error())
	}
}