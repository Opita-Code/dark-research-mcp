package tools

import (
	"path/filepath"
	"testing"

	"github.com/dark-agents/research-mcp/internal/config"
	"github.com/dark-agents/research-mcp/internal/mem"
	"github.com/mark3labs/mcp-go/server"
)

// Regression: dark-research finding #465 (2026-08-07) — v0.8.1 cache +
// synthesis shipped but were dead code in production.
//
// Root cause: tools.Register only called SetMem + SetSession on the
// singleton router. The v0.8.1 setters (SetMemStore, SetEnableCache,
// SetLLMClient, SetEnableSynthesize) were never invoked from any
// production call-site, so the deployed binary always ran with
// EnableCache=false and EnableSynthesize=false. The release notes said
// "wire them where you build the router", but the only place the router
// is built for production (Register) never did it.
//
// Fix: Register now wires MemStore + EnableCache from
// cfg.Research.EnableCache (default true) and LLMClient +
// EnableSynthesize from cfg.Research.EnableSynthesize (default false).
//
// Date: 2026-08-07 | Fix commit: (pending)
func TestRegression_RegisterWiresCacheByDefault(t *testing.T) {
	prev := shared
	t.Cleanup(func() { shared = prev })

	store := newTestMemStore(t)
	defer store.Close()

	srv := server.NewMCPServer("test", "test", server.WithToolCapabilities(true))
	cfg := config.Defaults()
	if !cfg.Research.EnableCache {
		t.Fatal("test precondition: config.Defaults() must have EnableCache=true")
	}
	if err := Register(srv, cfg, Deps{Mem: store, Session: "sess-test"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	r := sharedRouter()
	if r.MemStore == nil {
		t.Error("router.MemStore is nil after Register: within-TTL cache lookup can never hit")
	}
	if !r.EnableCache {
		t.Error("router.EnableCache is false after Register with default config: persistence-aware recall is dead in production")
	}
	if r.EnableSynthesize {
		t.Error("router.EnableSynthesize should default to false (LLM synthesis is opt-in)")
	}
}

// TestRegression_RegisterWiresSynthesisWhenEnabled asserts the opt-in
// path works: when config enables synthesis AND an API key is reachable,
// the router gets both the LLM client and the enable flag.
func TestRegression_RegisterWiresSynthesisWhenEnabled(t *testing.T) {
	prev := shared
	t.Cleanup(func() { shared = prev })

	t.Setenv("SDD_LLM_API_KEY", "regression-test-key")
	t.Setenv("SDD_LLM_BASE_URL", "https://regression.invalid")

	store := newTestMemStore(t)
	defer store.Close()

	srv := server.NewMCPServer("test", "test", server.WithToolCapabilities(true))
	cfg := config.Defaults()
	cfg.Research.EnableSynthesize = true
	if err := Register(srv, cfg, Deps{Mem: store, Session: "sess-test"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	r := sharedRouter()
	if !r.EnableSynthesize {
		t.Error("router.EnableSynthesize is false despite cfg.Research.EnableSynthesize=true")
	}
	if r.LLMClient == nil {
		t.Error("router.LLMClient is nil despite an API key being reachable")
	}
}

// TestRegression_RegisterWiresSynthesisGracefulNoKey asserts that
// flipping the flag without an API key does NOT error — the router
// simply leaves synthesis off (EnableSynthesize stays false, client is
// nil), matching the v0.8.1 graceful-degradation contract. No error to
// the caller either way.
func TestRegression_RegisterWiresSynthesisGracefulNoKey(t *testing.T) {
	prev := shared
	t.Cleanup(func() { shared = prev })

	// Ensure no key is reachable regardless of the host env.
	t.Setenv("SDD_LLM_API_KEY", "")
	t.Setenv("MINIMAX_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("SDD_LLM_BASE_URL", "")
	t.Setenv("DARK_SCRAPPER_URL", "")

	store := newTestMemStore(t)
	defer store.Close()

	srv := server.NewMCPServer("test", "test", server.WithToolCapabilities(true))
	cfg := config.Defaults()
	cfg.Research.EnableSynthesize = true
	if err := Register(srv, cfg, Deps{Mem: store, Session: "sess-test"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	r := sharedRouter()
	if r.EnableSynthesize {
		t.Error("router.EnableSynthesize should stay false when no API key is reachable (graceful degradation)")
	}
	if r.LLMClient != nil {
		t.Error("router.LLMClient should be nil when no API key is reachable")
	}
}

// TestRegression_RegisterRespectsCacheOptOut asserts operators can turn
// the cache off explicitly.
func TestRegression_RegisterRespectsCacheOptOut(t *testing.T) {
	prev := shared
	t.Cleanup(func() { shared = prev })

	store := newTestMemStore(t)
	defer store.Close()

	srv := server.NewMCPServer("test", "test", server.WithToolCapabilities(true))
	cfg := config.Defaults()
	cfg.Research.EnableCache = false
	if err := Register(srv, cfg, Deps{Mem: store, Session: "sess-test"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	r := sharedRouter()
	if r.EnableCache {
		t.Error("router.EnableCache is true despite cfg.Research.EnableCache=false")
	}
	if r.MemStore == nil {
		t.Error("router.MemStore should still be wired (cache lookup is skipped when EnableCache=false)")
	}
}

// newTestMemStore opens a throwaway SQLite store under a temp dir. The
// caller must Close() it; the temp dir is cleaned by t.TempDir().
func newTestMemStore(t *testing.T) *mem.Store {
	t.Helper()
	store, err := mem.Open(filepath.Join(t.TempDir(), "regression.db"))
	if err != nil {
		t.Fatalf("mem.Open: %v", err)
	}
	return store
}
