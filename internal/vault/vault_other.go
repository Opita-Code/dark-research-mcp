//go:build !windows

// Stub vault backend for non-Windows platforms. The current
// `dark-agents-vault.psm1` is Windows-only (DPAPI + Credential Manager).
// On *nix, implement one of:
//
//   - Keyring  (github.com/zalando/go-keyring — uses libsecret on Linux,
//                Keychain on macOS)
//   - age-encrypted file (filippo.io/age)
//   - HashiCorp Vault / 1Password CLI
//
// Then add it to openPlatform() below and replace the stub methods.
package vault

type stubVault struct{}

func openPlatform() Vault { return &stubVault{} }

func (stubVault) Save(string, string) error   { return ErrNotImplemented }
func (stubVault) Get(string) (string, error)  { return "", ErrNotImplemented }
func (stubVault) List() ([]string, error)     { return nil, ErrNotImplemented }
func (stubVault) Remove(string) error         { return nil } // idempotent on a stub
}