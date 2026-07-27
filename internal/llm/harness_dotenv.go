// Package llm: harness .env loader.
//
// LoadHarnessDotenv reads the env-var sources the parent AI-coding
// harness (OpenCode, Claude Code, Cursor, Aider, Cline) typically
// uses to feed API keys to the MCP, and returns the merged KEY=VALUE
// map. The caller merges these into the process env so that the LLM
// client has a working credential even when the harness sanitised
// the env (e.g. opencode.jsonc with explicit empty values wipes the
// parent shell's ANTHROPIC_API_KEY, leaving the MCP unconfigured).
//
// Why this exists
//
// Without LoadHarnessDotenv, the LLM client is stuck if:
//   - DARK_SCRAPPER_URL is set but the daemon is down (connection
//     refused is a network error, not a 4xx/5xx, so shouldFallback
//     in client.go does not catch it).
//   - The harness's opencode.jsonc / claude settings / .env explicitly
//     empties ANTHROPIC_API_KEY (the harness may do this to force
//     "use scrapper only"), but the operator also wants a fallback
//     path through their existing account key.
//
// With LoadHarnessDotenv, the LLM client can fall back to the
// operator's real account key (which lives in the user-level env,
// the harness's config file, or a project .env) instead of being
// permanently unconfigured.
//
// Source priority (first non-empty wins per KEY)
//
//   1. HKCU\Environment on Windows / $HOME env on Unix  (parent user-level)
//   2. $HOME/.env                                       (operator's global .env)
//   3. <project-root>/.env                              (project .env)
//   4. OpenCode config: $XDG_CONFIG_HOME/opencode/opencode.jsonc (or
//      %APPDATA%\opencode\opencode.jsonc on Windows). The `environment`
//      block. We accept non-empty values only — empty string is
//      treated as "wiped by harness" and skipped.
//   5. Claude Code config: $XDG_CONFIG_HOME/claude/settings.json (or
//      %APPDATA%\claude\settings.json on Windows). The `env` block
//      (Anthropic-Code-style flat object).
//
// The .env parser is intentionally minimal: KEY=VALUE, optional
// double-quotes, optional comments with #. We do NOT support
// multi-line values, variable expansion, or `export FOO=` syntax.
// That is enough for the operator's account-key use case; anything
// more complex should live in the harness's native config (which
// we also read in step 4 / 5).
package llm

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// dotenvCache memoises LoadHarnessDotenv. The harness config files
// don't change during a single MCP session, and reading the Windows
// registry is non-trivial (~5ms). We cache the merged result.
var (
	dotenvOnce  sync.Once
	dotenvCache map[string]string
)

// LoadHarnessDotenv returns the merged KEY=VALUE map from all
// harness .env sources. The result is process-cached.
//
// Callers should NEVER mutate the returned map.
func LoadHarnessDotenv() map[string]string {
	dotenvOnce.Do(func() {
		dotenvCache = loadHarnessDotenvUncached()
	})
	return dotenvCache
}

// loadHarnessDotenvUncached is the underlying loader, split out so
// tests can reset the cache and exercise the parsing paths.
func loadHarnessDotenvUncached() map[string]string {
	out := make(map[string]string)

	// 1. User-level env (Windows registry / Unix $HOME).
	//    loadUserLevelEnv is implemented in harness_dotenv_windows.go
	//    and harness_dotenv_unix.go (build-tag-gated). On Windows it
	//    reads HKCU\Environment; on Unix the process env already has
	//    user-level vars so this is a no-op.
	loadUserLevelEnv(out)

	// 2. $HOME/.env (operator's global .env).
	if home, err := os.UserHomeDir(); err == nil {
		loadDotenvFile(filepath.Join(home, ".env"), out)
	}

	// 3. <project-root>/.env. We walk up from cwd until we find a
	// .git or go.mod; that's the "project root" for .env purposes.
	if cwd, err := os.Getwd(); err == nil {
		loadProjectDotenv(cwd, out)
	}

	// 4. OpenCode config — only non-empty values are accepted.
	loadOpenCodeConfig(out)

	// 5. Claude Code config — only non-empty values are accepted.
	loadClaudeCodeConfig(out)

	return out
}

// loadDotenvFile parses a single .env file and merges non-empty
// KEY=VALUE pairs into out. Missing files and parse errors are
// silently skipped (the .env is best-effort, not required).
func loadDotenvFile(path string, out map[string]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip blank lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip optional `export ` prefix.
		line = strings.TrimPrefix(line, "export ")
		// Split on first `=`.
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		// Strip surrounding quotes (single or double).
		if len(val) >= 2 {
			first, last := val[0], val[len(val)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if key == "" || val == "" {
			continue
		}
		out[key] = val
	}
}

// loadProjectDotenv walks up from start looking for a .env file.
// Stops at the first .env found, or at the filesystem root.
func loadProjectDotenv(start string, out map[string]string) {
	dir := start
	for i := 0; i < 8; i++ { // cap at 8 levels to avoid runaway
		candidate := filepath.Join(dir, ".env")
		if _, err := os.Stat(candidate); err == nil {
			loadDotenvFile(candidate, out)
			return
		}
		// Stop at filesystem root.
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

// loadOpenCodeConfig reads the harness's opencode.jsonc and merges
// any non-empty values from the `environment` block into out.
//
// On Windows the default location is %APPDATA%\opencode\opencode.jsonc
// (or %USERPROFILE%\.config\opencode\opencode.jsonc); on Unix it's
// $XDG_CONFIG_HOME/opencode/opencode.jsonc (or ~/.config/opencode/...).
//
// .jsonc is a superset of JSON that allows // comments and trailing
// commas. We strip both before parsing.
func loadOpenCodeConfig(out map[string]string) {
	path := openCodeConfigPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	// Strip // line comments and block comments. Not a full .jsonc
	// parser; we just remove the common shapes that operators hit.
	stripped := stripJSONCComments(string(data))
	var parsed struct {
		Environment map[string]string `json:"environment"`
	}
	if err := json.Unmarshal([]byte(stripped), &parsed); err != nil {
		return
	}
	for k, v := range parsed.Environment {
		// Empty string means "harness wiped this on purpose" — skip
		// it so we don't override a real value from a higher-priority
		// source (Windows registry, $HOME/.env) with an empty one.
		if v == "" {
			continue
		}
		out[k] = v
	}
}

// loadClaudeCodeConfig reads Claude Code's settings.json and merges
// any non-empty values from the top-level `env` object into out.
//
// Claude Code uses a flat `env` object at the top of settings.json,
// not a nested `environment` block. The keys follow Anthropic env
// conventions: ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, etc.
func loadClaudeCodeConfig(out map[string]string) {
	path := claudeCodeConfigPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	stripped := stripJSONCComments(string(data))
	var parsed struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(stripped), &parsed); err != nil {
		return
	}
	for k, v := range parsed.Env {
		if v == "" {
			continue
		}
		out[k] = v
	}
}

// stripJSONCComments removes // line comments and /* block comments */
// from a JSONC blob. We do NOT handle comments inside string values
// (e.g. URLs containing //), which is the same trade-off that most
// simple .jsonc parsers make.
func stripJSONCComments(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	inString := false
	inLineComment := false
	inBlockComment := false
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case inLineComment:
			if c == '\n' {
				inLineComment = false
				out.WriteByte(c)
			}
		case inBlockComment:
			if c == '*' && i+1 < len(s) && s[i+1] == '/' {
				inBlockComment = false
				i++
			}
		case inString:
			out.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				out.WriteByte(s[i+1])
				i++
			} else if c == '"' {
				inString = false
			}
		default:
			if c == '"' {
				inString = true
				out.WriteByte(c)
			} else if c == '/' && i+1 < len(s) && s[i+1] == '/' {
				inLineComment = true
				i++
			} else if c == '/' && i+1 < len(s) && s[i+1] == '*' {
				inBlockComment = true
				i++
			} else {
				out.WriteByte(c)
			}
		}
		i++
	}
	return out.String()
}

// openCodeConfigPath returns the absolute path to opencode.jsonc
// for the current platform, or "" if the platform is unsupported.
func openCodeConfigPath() string {
	if runtime.GOOS == "windows" {
		// Try %APPDATA% first, then %USERPROFILE%\.config\opencode.
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			p := filepath.Join(appdata, "opencode", "opencode.jsonc")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		if home, err := os.UserHomeDir(); err == nil {
			p := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		return ""
	}
	// Unix-like: $XDG_CONFIG_HOME or $HOME/.config.
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		p := filepath.Join(xdg, "opencode", "opencode.jsonc")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// claudeCodeConfigPath returns the absolute path to Claude Code's
// settings.json for the current platform, or "" if unsupported.
func claudeCodeConfigPath() string {
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			p := filepath.Join(appdata, "claude", "settings.json")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".config", "claude", "settings.json")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		// Older Claude Code used ~/.claude/settings.json.
		p = filepath.Join(home, ".claude", "settings.json")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
