// Command dark-research-mcp is the MCP server entry point.
//
// Logging goes to stderr (stdout is reserved for the JSON-RPC frames
// produced by the stdio transport).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dark-agents/research-mcp/internal/config"
	"github.com/dark-agents/research-mcp/internal/constitution"
	"github.com/dark-agents/research-mcp/internal/llm"
	"github.com/dark-agents/research-mcp/internal/mem"
	"github.com/dark-agents/research-mcp/internal/research"
	darkserver "github.com/dark-agents/research-mcp/internal/server"
	"github.com/dark-agents/research-mcp/internal/tools"
)

// version is set at link time via:
//
//	go build -ldflags "-X main.version=0.4.0-rc.2" ./cmd/dark-research-mcp
//
// The default "dev" is what you get from a plain `go build` for local
// development. CI release builds always set the real version.
var version = "dev"

func main() {
	cfgPath := flag.String("config", "", "path to config file (default: $DARK_RESEARCH_CONFIG or ./dark-research.toml)")
	logLevel := flag.String("log-level", "info", "tracing log level (debug|info|warn|error)")
	dbPath := flag.String("db", "", "path to dark.db (default: $DARK_DB or %LOCALAPPDATA%\\dark-agents\\dark.db)")
	cachePath := flag.String("cache", "", "path to LLM cache file (default: $DARK_SSD_CACHE_PATH or $DARK_DB_DIR/llm-cache.json; empty disables)")
	cacheTTL := flag.Duration("cache-ttl", time.Hour, "LLM cache TTL (e.g. 30m, 2h, 24h)")
	constitutionSpec := flag.String("constitution", "", "active constitution: 'light' (default), 'dark' (requires -tags allow_builtin_dark), 'user/<id>' (in ~/.dark-research/constitutions/), or an absolute .toml path. Also reads $DARK_CONSTITUTION.")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("dark-research-mcp %s\n", version)
		os.Exit(0)
	}

	// Stamp the research User-Agent with our build version.
	research.Version = version

	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(2)
	}

	// v0.1: log level is read but routing stays on the stdlib log package.
	_ = *logLevel

	// Open the research mem store. Default path mirrors dark-eval's
	// dark.db so research and eval findings share the same SQLite
	// file (different tables, no conflict).
	db := *dbPath
	if db == "" {
		db = os.Getenv("DARK_DB")
	}
	if db == "" {
		db = defaultDBPath()
	}
	store, err := mem.Open(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mem open error: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()
	log.Printf("dark-research-mcp: db=%s", db)

	// Resolve the active constitution. Precedence: --constitution
	// flag > $DARK_CONSTITUTION env var > default (light). The
	// dark constitution is only available in binaries built with
	// -tags allow_builtin_dark; referencing it on a stock build
	// is a clear error, not a silent fallback to light.
	constitution.Initialize()
	if err := initConstitution(*constitutionSpec); err != nil {
		fmt.Fprintf(os.Stderr, "constitution error: %v\n", err)
		os.Exit(2)
	}
	active := constitution.Active()
	log.Printf("dark-research-mcp: constitution=%s source=%s", active.ID(), active.Source)

	// Initialize the optional LLM response cache. Disabled unless an
	// explicit path is provided via --cache or $DARK_SSD_CACHE_PATH.
	// When the dark-ssd tools run with a cache attached, identical
	// brand_match / compliance_check / drift_judge / grounding_check
	// calls within the TTL window become zero-cost lookups.
	cache := mustInitCache(*cachePath, db, *cacheTTL)
	if cache != nil {
		log.Printf("dark-research-mcp: llm_cache=%s ttl=%s", cache.Stats().Path, *cacheTTL)
	}
	tools.AttachLLMCache(cache)

	srv, err := darkserver.New(cfg, store, filepath.Base(db))
	if err != nil {
		fmt.Fprintf(os.Stderr, "server init error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := darkserver.Serve(ctx, srv); err != nil {
		fmt.Fprintf(os.Stderr, "serve error: %v\n", err)
		os.Exit(1)
	}
}

// initConstitution resolves and activates the constitution chosen by
// the user. The precedence order is:
//   1. --constitution flag (passed in as flag)
//   2. DARK_CONSTITUTION env var
//   3. light default (built-in)
//
// On error, we fail loud and exit non-zero. A user who explicitly
// asked for "dark" on a stock build should see the error, not a
// silent fallback to light.
func initConstitution(flagSpec string) error {
	spec := flagSpec
	if spec == "" {
		spec = os.Getenv("DARK_CONSTITUTION")
	}
	_, err := constitution.SetActiveFromFlag(spec)
	return err
}

func defaultDBPath() string {
	d := os.Getenv("LOCALAPPDATA")
	if d == "" {
		d = os.Getenv("USERPROFILE")
	}
	if d == "" {
		home, _ := os.UserHomeDir()
		d = home
	}
	return filepath.Join(d, "dark-agents", "dark.db")
}

// mustInitCache resolves the cache path from flag → env → default next
// to dark.db. Returns nil if all sources are empty (caching disabled).
func mustInitCache(flagPath, dbPath string, ttl time.Duration) *llm.Cache {
	p := flagPath
	if p == "" {
		p = os.Getenv("DARK_SSD_CACHE_PATH")
	}
	if p == "" {
		// Default: sibling of dark.db
		if dbPath != "" {
			p = filepath.Join(filepath.Dir(dbPath), "llm-cache.json")
		}
	}
	if p == "" {
		return nil
	}
	c, err := llm.NewCache(p, ttl)
	if err != nil {
		log.Printf("dark-research-mcp: cache disabled (%v)", err)
		return nil
	}
	return c
}