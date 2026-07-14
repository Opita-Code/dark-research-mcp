// Package vault is the cross-platform abstraction over dark-agents'
// secret store. On Windows it shells out to the PowerShell module
// `dark-agents-vault.psm1` (DPAPI at rest via Windows Credential Manager).
// On other platforms it returns ErrNotImplemented — fill in a real
// implementation (keyring, age-encrypted file, etc.) when needed.
//
// All operations use the prefix "dark-agents-v2/" for the vault target
// so secrets are namespaced away from other Windows credential entries.
//
// NEVER log the secret value. Use LogPreview() to get a masked form.
package vault

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Vault is the interface every backend implements.
type Vault interface {
	// Save persists a secret under name. Returns an error if the name
	// is empty or the backend rejects the value (e.g. too long).
	Save(name, value string) error

	// Get retrieves a secret by name. Returns ErrNotFound if absent.
	Get(name string) (string, error)

	// List returns the names of every secret stored under the prefix.
	List() ([]string, error)

	// Remove deletes a secret by name. Idempotent: removing an absent
	// secret is not an error.
	Remove(name string) error
}

// ErrNotFound is returned by Get when the secret does not exist.
var ErrNotFound = errors.New("vault: secret not found")

// ErrNotImplemented is returned by backends that are stubs on the
// current platform.
var ErrNotImplemented = errors.New("vault: backend not implemented on this platform")

// Prefix is the namespace for all dark-agents secrets. Mirrors the
// $VaultTargetPrefix constant in dark-agents-vault.psm1.
const Prefix = "dark-agents-v2/"

// Open returns the platform-default vault. On Windows it's the
// PowerShell-backed credential manager; on other platforms it returns
// a stub that errors with ErrNotImplemented.
func Open() Vault {
	return openPlatform()
}

// Mask returns a safe preview of a secret for logging. "abc123def"
// becomes "abc1...3def". Strings of 8 chars or fewer become "***".
// Empty strings become "<empty>".
func Mask(s string) string {
	if s == "" {
		return "<empty>"
	}
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "..." + s[len(s)-4:]
}

// ValidateName returns an error if name is empty or contains characters
// unsafe for the underlying credential manager (e.g. /, \, null).
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("vault: name is empty")
	}
	if strings.ContainsAny(name, "/\\\x00\n\r") {
		return fmt.Errorf("vault: name contains invalid characters")
	}
	return nil
}

// LoadIntoEnv loads each named secret from the vault into the process
// environment via os.Setenv. The policy is:
//
//   - If os.Getenv(name) is already non-empty (parent harness or shell
//     provided it), the vault value is NOT used. The caller wins.
//   - If os.Getenv(name) is empty or unset, the vault value is set.
//   - If the secret is not in the vault (ErrNotFound), the name is
//     silently skipped — missing credentials are not a fatal error at
//     boot; tools that need them surface their own errors when called.
//   - If the platform has no vault implementation (ErrNotImplemented,
//     the *nix stub), LoadIntoEnv returns nil immediately. The binary
//     still boots; callers without credentials fail at the tool level,
//     not the boot level.
//
// This makes dark-research-mcp standalone: the binary looks up
// credentials from the vault when the parent harness (opencode,
// Claude Code, Cursor, Aider, Cline, ...) didn't pre-populate the
// env. See docs/HARNESSES.md for the install pattern across 5
// popular harnesses. On non-Windows platforms where the vault is
// not yet implemented, the binary still boots — graceful degradation
// is the contract here, not full functionality.
func LoadIntoEnv(names []string) error {
	v := Open()
	for _, name := range names {
		if os.Getenv(name) != "" {
			continue // caller-provided, don't override
		}
		val, err := v.Get(name)
		if err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotImplemented) {
				continue
			}
			return fmt.Errorf("vault: load %s: %w", name, err)
		}
		if err := os.Setenv(name, val); err != nil {
			return fmt.Errorf("vault: setenv %s: %w", name, err)
		}
	}
	return nil
}

// parseVaultList extracts the secret names from a Test-DarkAgentVault
// diagnostic blob. The format is:
//
//	=== dark-agents-v2 vault diagnostic ===
//	Target prefix: dark-agents-v2/
//	Log file:      ...
//	Stored secrets:
//	  - NAME1
//	    preview: ...
//	  - NAME2
//	    preview: ...
//
// Only the "  - " lines are parsed; everything else is ignored.
//
// Lives in vault.go (no build tag) so cross-platform tests can exercise
// the parser without needing the Windows-only winVault backend.
func parseVaultList(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			n := strings.TrimSpace(strings.TrimPrefix(line, "- "))
			if n != "" {
				names = append(names, n)
			}
		}
	}
	return names
}