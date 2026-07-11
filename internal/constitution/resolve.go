package constitution

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ---------------------------------------------------------------------------
// Active constitution resolver.
//
// The binary exposes a single "active constitution" — the one
// BuildSystemPrompt uses for every LLM call. Resolve() implements
// the precedence rules:
//
//  1. --constitution CLI flag (passed via SetActiveFromFlag).
//  2. DARK_CONSTITUTION env var (read by SetActiveFromEnv).
//  3. User file at ~/.dark-research/constitutions/<id>.toml if it
//     exists.
//  4. Built-in dark (if available, i.e. binary built with
//     `-tags allow_builtin_dark`).
//  5. Built-in light (always available — the public default).
//
// The resolver is intentionally simple. It does NOT cache user
// files between invocations; SetActive must be called at startup.
// The "active" set is process-global; concurrent reads after
// SetActive are safe (the pointer is immutable).
// ---------------------------------------------------------------------------

// userDir is the conventional per-user location for constitution
// files. Lives under the home directory so the same set of
// constitutions follows the user across machines. The directory is
// created on demand the first time we write to it; we never
// require it to exist at startup.
func userDir() (string, error) {
	// We deliberately do NOT use os.UserHomeDir here: we want
	// $DARK_RESEARCH_HOME if set, then $XDG_CONFIG_HOME on
	// non-Windows, then the platform-specific default. Mirrors
	// the convention used by the rest of the binary.
	if v := os.Getenv("DARK_RESEARCH_HOME"); v != "" {
		return filepath.Join(v, "constitutions"), nil
	}
	// Fall back to ~/.dark-research/constitutions.
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("constitution: cannot resolve user home: %w", err)
	}
	return filepath.Join(home, ".dark-research", "constitutions"), nil
}

var (
	activeMu sync.RWMutex
	active   *Constitution
)

// SetActive installs c as the active constitution. Safe to call
// once at startup. Passing nil restores the light default (used by
// tests that want to exercise the pre-Fase-1 path explicitly).
func SetActive(c *Constitution) {
	activeMu.Lock()
	active = c
	activeMu.Unlock()
}

// Active returns the currently active constitution. If SetActive
// has not been called, returns Light (the built-in default). Never
// returns nil — BuildSystemPrompt relies on this contract.
func Active() *Constitution {
	activeMu.RLock()
	c := active
	activeMu.RUnlock()
	if c != nil {
		return c
	}
	return Light
}

// SetActiveFromFlag is the resolver entry point used by main.go
// after parsing CLI flags. `spec` is one of:
//
//   - "light" or "builtin:light"         → Light
//   - "dark"  or "builtin:dark"          → Dark (may be nil)
//   - "user/<id>" or "user:<id>"         → ~/.dark-research/constitutions/<id>.toml
//   - an absolute path ending in .toml   → Load(path)
//
// Returns the resolved constitution and a flag indicating whether
// the spec referred to a user file. The boolean exists so the
// caller can log the chosen source.
func SetActiveFromFlag(spec string) (*Constitution, error) {
	c, _, err := Resolve(spec)
	if err != nil {
		return nil, err
	}
	SetActive(c)
	return c, nil
}

// Resolve implements the precedence rules above WITHOUT mutating
// the active singleton. Used by SetActiveFromFlag and by tests.
// Returns (constitution, builtin-light-was-used, error).
func Resolve(spec string) (*Constitution, bool, error) {
	// Empty spec → light default.
	if spec == "" {
		return Light, true, nil
	}

	// 1. Explicit built-in alias.
	switch spec {
	case "light", "builtin:light", "dark-research/light", "dark-research/light@1.0.0":
		return Light, true, nil
	case "dark", "builtin:dark", "dark-research/aggressive", "dark-research/aggressive@1.0.0":
		if Dark == nil {
			return nil, false, fmt.Errorf("constitution: dark.toml is not embedded in this binary (rebuild with -tags allow_builtin_dark, or supply a user file at user/dark)")
		}
		return Dark, true, nil
	}

	// 2. Absolute path.
	if filepath.IsAbs(spec) || looksLikePath(spec) {
		c, err := Load(spec)
		if err != nil {
			return nil, false, err
		}
		return c, false, nil
	}

	// 3. User alias: "user/<id>" or "user:<id>" — strip the prefix
	// and look in the user dir.
	if id, ok := stripUserPrefix(spec); ok {
		dir, err := userDir()
		if err != nil {
			return nil, false, err
		}
		path := filepath.Join(dir, id+".toml")
		if _, statErr := os.Stat(path); statErr == nil {
			c, err := Load(path)
			if err != nil {
				return nil, false, err
			}
			return c, false, nil
		}
		return nil, false, fmt.Errorf("constitution: user file not found at %s", path)
	}

	// 4. Unrecognized spec.
	return nil, false, fmt.Errorf("constitution: unrecognized spec %q (want 'light', 'dark', 'user/<id>', or an absolute .toml path)", spec)
}

// stripUserPrefix returns ("<id>", true) if spec starts with
// "user/" or "user:". Otherwise returns ("", false).
func stripUserPrefix(spec string) (string, bool) {
	const slash = "user/"
	const colon = "user:"
	if len(spec) > len(slash) && spec[:len(slash)] == slash {
		return spec[len(slash):], true
	}
	if len(spec) > len(colon) && spec[:len(colon)] == colon {
		return spec[len(colon):], true
	}
	return "", false
}

// looksLikePath is a tiny heuristic for "is this a path?". It
// accepts anything containing a path separator or a trailing .toml.
// Avoids false positives on ids like "user/dark" by requiring the
// separator to be at position 0 (i.e. absolute) — the user alias
// case is handled separately by stripUserPrefix.
func looksLikePath(s string) bool {
	if len(s) == 0 {
		return false
	}
	if s[0] == '/' || s[0] == '\\' {
		return true
	}
	// Windows drive letter prefix.
	if len(s) >= 3 && s[1] == ':' && (s[2] == '/' || s[2] == '\\') {
		return true
	}
	return false
}
