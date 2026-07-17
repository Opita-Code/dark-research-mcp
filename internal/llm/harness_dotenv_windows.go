//go:build windows

// Package llm: Windows user-level env reader.
//
// loadWindowsUserEnv reads the user-level environment variables from
// the Windows registry (HKCU\Environment) and merges them into out.
//
// Why this is needed
//
// On Windows, the user-level env (what the operator sets via
// "System → Advanced → Environment Variables") lives in the
// registry, not in the process env. When opencode (or any other
// harness) sanitises the env for the MCP child, those keys are
// lost — the MCP sees the harness's empty overrides, not the
// operator's real values. Reading the registry bypasses the
// sanitisation and lets the MCP find the operator's account key.
package llm

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

// loadUserLevelEnv is the Windows implementation: reads
// HKCU\Environment and merges any values from REG_EXPAND_SZ /
// REG_SZ into out. PATH-like variables (those ending in PATH) are
// skipped: the MCP doesn't need them, and a stale %USERPROFILE%
// token in a REG_EXPAND_SZ would require expansion that we don't
// bother doing here.
func loadUserLevelEnv(out map[string]string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()

	names, err := k.ReadValueNames(0)
	if err != nil {
		return
	}
	for _, name := range names {
		// Skip PATH-like variables; they're irrelevant for the LLM
		// client and a stale %USERPROFILE% token would just confuse
		// any future consumer.
		upper := asciiUpper(name)
		if upper == "PATH" || upper == "PATHEXT" || upper == "PSMODULEPATH" {
			continue
		}
		val, _, err := k.GetStringValue(name)
		if err != nil {
			// Non-string types (REG_DWORD, REG_BINARY) are ignored.
			continue
		}
		// REG_EXPAND_SZ values may contain %USERPROFILE% etc. We
		// expand them with the current process env so the operator
		// gets the resolved path.
		val = os.ExpandEnv(val)
		if val == "" {
			continue
		}
		out[name] = val
	}
}

func asciiUpper(s string) string {
	// ASCII-only upper to avoid pulling in unicode tables; env var
	// names are always ASCII on Windows in practice.
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
