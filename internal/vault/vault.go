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