//go:build windows

// Windows vault backend: shells out to the PowerShell module
// `dark-agents-vault.psm1` for Save / Get / List / Remove operations.
// Secrets live in the Windows Credential Manager (DPAPI at rest).
package vault

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// psmPath is the on-disk location of the PowerShell module. Set via
// the DARK_VAULT_MODULE env var to override (useful for tests).
// Default: $env:LOCALAPPDATA\dark-agents\vault\dark-agents-vault.psm1
// or the convention C:\Users\Nico\Documents\dark-agents\vault\...
func defaultPSMPath() string {
	return `C:\Users\Nico\Documents\dark-agents\vault\dark-agents-vault.psm1`
}

type winVault struct {
	psm string
}

func openPlatform() Vault {
	psm := defaultPSMPath()
	return &winVault{psm: psm}
}

// invoke runs a PowerShell function from the vault module and returns
// stdout. The module is imported into the caller's session via
// Import-Module, so Save-DarkAgentSecret / Get-DarkAgentSecret become
// available as cmdlets.
func (v *winVault) invoke(ctx context.Context, snippet string) (string, error) {
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// -NoProfile: faster; the module is imported by the same command.
	// -ExecutionPolicy Bypass: required for the C# Add-Type block.
	// -Command: build the import + function call as one pipeline.
	cmd := exec.CommandContext(c, "powershell.exe",
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-Command",
		fmt.Sprintf(`Import-Module '%s' -Force; %s`, v.psm, snippet),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("vault: powershell: %w (stderr: %s)", err, stderr.String())
	}
	return stdout.String(), nil
}

// Save persists the secret via Save-DarkAgentSecret.
func (v *winVault) Save(name, value string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	// Single-quote both args, escape embedded single quotes by doubling.
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	snippet := fmt.Sprintf(`Save-DarkAgentSecret -Name '%s' -Value '%s' -ErrorAction Stop`,
		esc(name), esc(value))
	_, err := v.invoke(context.Background(), snippet)
	return err
}

// Get returns the secret via Get-DarkAgentSecret. Returns ErrNotFound
// when the module throws "not found".
func (v *winVault) Get(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	snippet := fmt.Sprintf(`$s = Get-DarkAgentSecret -Name '%s' -ErrorAction Stop; Write-Output $s`, esc(name))
	out, err := v.invoke(context.Background(), snippet)
	if err != nil {
		// Match either "not found" or "Cannot find" in the error text.
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "not found") || strings.Contains(low, "cannot find") {
			return "", ErrNotFound
		}
		return "", err
	}
	return strings.TrimRight(out, "\r\n"), nil
}

// List returns every secret name under the prefix. The PowerShell
// module exposes Test-DarkAgentVault which prints a diagnostic; we
// parse the names from its output.
func (v *winVault) List() ([]string, error) {
	snippet := `Test-DarkAgentVault 2>&1 | Out-String`
	out, err := v.invoke(context.Background(), snippet)
	if err != nil {
		return nil, err
	}
	return parseVaultList(out), nil
}

// Remove deletes via Remove-DarkAgentSecret. Idempotent.
func (v *winVault) Remove(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	snippet := fmt.Sprintf(`Remove-DarkAgentSecret -Name '%s' -ErrorAction SilentlyContinue`, esc(name))
	_, err := v.invoke(context.Background(), snippet)
	return err
}

// parseVaultList extracts the secret names from Test-DarkAgentVault's
// output. The format is:
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
// We only care about the "  - " lines.
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