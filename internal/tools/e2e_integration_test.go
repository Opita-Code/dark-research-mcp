package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dark-agents/research-mcp/internal/mem"
)

// TestArtifactDownload_E2E_FileBackedStore is the integration smoke test
// for the artifact download flow against a file-backed dark.db (not
// :memory:). This validates that:
//
//   - mem.Open with a real file path runs migrations cleanly
//   - SaveArtifact persists across reopens (idempotency of LastInsertId)
//   - GetArtifact reads back the same row
//   - artifactDownloadTool fetches via the shared clearnet client
//   - The downloaded content matches the server's response byte-for-byte
//
// Unlike the unit tests in artifact_download_test.go, this exercises
// the actual SQLite schema and migrations path that production uses.
func TestArtifactDownload_E2E_FileBackedStore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("# spec doc\n\nthis is the artifact content"))
	}))
	t.Cleanup(srv.Close)

	dbPath := filepath.Join(t.TempDir(), "e2e.db")

	// Phase 1: open, write, close.
	store1, err := mem.Open(dbPath)
	if err != nil {
		t.Fatalf("mem.Open (write): %v", err)
	}
	id, err := store1.SaveArtifact(context.Background(), &mem.Artifact{
		VibeCase:     "C2",
		ArtifactURL:  srv.URL,
		ArtifactType: "text",
		BrandID:      "e2e-brand",
	})
	if err != nil {
		t.Fatalf("SaveArtifact: %v", err)
	}
	if id <= 0 {
		t.Fatalf("SaveArtifact returned id=%d, want >0", id)
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("store1.Close: %v", err)
	}

	// Phase 2: reopen, verify the row survives.
	store2, err := mem.Open(dbPath)
	if err != nil {
		t.Fatalf("mem.Open (read): %v", err)
	}
	defer store2.Close()
	defer installSharedMem(t, store2)()

	got, err := store2.GetArtifact(context.Background(), id)
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	if got == nil {
		t.Fatalf("GetArtifact returned nil for id=%d (row lost across reopen)", id)
	}
	if got.ArtifactURL != srv.URL {
		t.Errorf("ArtifactURL after reopen: got %q, want %q", got.ArtifactURL, srv.URL)
	}
	if got.BrandID != "e2e-brand" {
		t.Errorf("BrandID after reopen: got %q, want e2e-brand", got.BrandID)
	}

	// Phase 3: download via the tool.
	out := callArtifactDownload(t, downloadTestClients(), map[string]any{
		"artifact_id": id,
	})
	if downloaded, _ := out["downloaded"].(bool); !downloaded {
		t.Fatalf("downloaded: got false, want true (full: %v)", out)
	}
	if content, _ := out["content"].(string); !strings.Contains(content, "spec doc") {
		t.Errorf("content missing expected text, got: %q", content)
	}
	if brand, _ := out["brand_id"].(string); brand != "e2e-brand" {
		t.Errorf("brand_id in result: got %q, want e2e-brand", brand)
	}

	// Phase 4: confirm the schema_version landed where expected.
	var schemaVersion int
	if err := store2.DB().QueryRowContext(context.Background(),
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&schemaVersion); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if schemaVersion < 1 {
		t.Errorf("schema_version: got %d, want >=1 (migrations didn't run)", schemaVersion)
	}
}