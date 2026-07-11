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
	"strings"
	"syscall"
	"time"

	"github.com/dark-agents/research-mcp/internal/config"
	"github.com/dark-agents/research-mcp/internal/constitution"
	"github.com/dark-agents/research-mcp/internal/llm"
	"github.com/dark-agents/research-mcp/internal/mem"
	"github.com/dark-agents/research-mcp/internal/mods"
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
	modsSpec := flag.String("mods", "", "comma-separated mod_ids to activate at startup (e.g. 'user/osint-cve-deepdive,user/red-team-arsenal'). Also reads $DARK_MODS. If empty, mods listed in ~/.dark-research/mods/active.toml and mods with auto_load=true are activated.")
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

	// Initialize the mods registry. The store is best-effort
	// (mod_loads rows are audit, not blocking); a nil store is
	// acceptable in tests that don't need persistence.
	modsStore := mods.NewStore(context.Background(), store.DB())
	modsRegistry := mods.NewRegistry(modsStore)
	modsRegistry.SetConstitutionID(active.ID())
	if err := initMods(modsRegistry, *modsSpec); err != nil {
		fmt.Fprintf(os.Stderr, "mods error: %v\n", err)
		os.Exit(2)
	}
	activeMods := modsRegistry.List()
	if len(activeMods) > 0 {
		ids := make([]string, 0, len(activeMods))
		for _, m := range activeMods {
			ids = append(ids, m.Manifest.Meta.ID)
		}
		log.Printf("dark-research-mcp: active_mods=%s", strings.Join(ids, ","))
	}

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

	srv, err := darkserver.New(cfg, store, filepath.Base(db), modsRegistry)
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

// initMods activates the mods the user wants at startup. The
// precedence order is:
//   1. --mods flag (comma-separated mod_ids)
//   2. DARK_MODS env var (comma-separated)
//   3. ~/.dark-research/mods/active.toml (one mod_id per line)
//   4. Mods with auto_load = true in their mod.toml
//
// Steps 1, 2, 3 are explicit user intent. Step 4 is opt-in
// convenience. We collect the union and activate each; load
// failures are logged to stderr but do not abort startup
// (one bad mod should not break the whole binary).
func initMods(reg *mods.Registry, flagSpec string) error {
	modsList := []string{}
	if flagSpec != "" {
		modsList = append(modsList, splitModIDs(flagSpec)...)
	}
	if v := os.Getenv("DARK_MODS"); v != "" {
		modsList = append(modsList, splitModIDs(v)...)
	}
	// active.toml: one mod_id per non-comment line.
	if path, err := activeModsPath(); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				modsList = append(modsList, line)
			}
		}
	}
	// Step 4: discover all mods and activate those with
	// auto_load = true. We peek at the manifest on disk to
	// decide; we do not Activate yet (the activate happens
	// in the loop below so a bad mod's load failure is
	// logged uniformly).
	discovered, _ := reg.Discover()
	for _, id := range discovered {
		loaded, err := mods.Load(modRootFromID(id))
		if err != nil {
			continue
		}
		if loaded.Manifest.Activation.AutoLoad {
			modsList = append(modsList, id)
		}
	}

	// Dedupe.
	seen := map[string]bool{}
	unique := []string{}
	for _, id := range modsList {
		if seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}

	// Activate each; failures are logged.
	for _, id := range unique {
		if _, err := reg.Activate(id); err != nil {
			log.Printf("dark-research-mcp: mod %q failed to load: %v", id, err)
		}
	}
	return nil
}

// splitModIDs parses a comma-separated list of mod IDs and
// trims whitespace. Empty entries are dropped.
func splitModIDs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// activeModsPath returns the path to the persistent active
// mod list (one mod_id per line).
func activeModsPath() (string, error) {
	if v := os.Getenv("DARK_RESEARCH_HOME"); v != "" {
		return filepath.Join(v, "mods", "active.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dark-research", "mods", "active.toml"), nil
}

// modRootFromID converts a mod_id ("user/foo") to the on-disk
// path by searching the configured mod search paths
// (DARK_MODS_PATH, DARK_RESEARCH_HOME/mods, ~/.dark-research/mods).
// Returns "" if no match is found. Mirrors the logic in
// mods.DefaultSearchPaths so the auto_load discovery and the
// registry Activate use the same path resolution.
func modRootFromID(id string) string {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		return ""
	}
	short := parts[1]
	paths, err := mods.DefaultSearchPaths()
	if err != nil {
		return ""
	}
	for _, root := range paths {
		candidate := filepath.Join(root, short)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
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