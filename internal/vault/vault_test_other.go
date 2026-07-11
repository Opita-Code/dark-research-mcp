//go:build !windows

package vault

import (
	"errors"
	"testing"
)

// On non-Windows the vault is a stub returning ErrNotImplemented.

func TestStub_methods(t *testing.T) {
	v := stubVault{}
	if err := v.Save("x", "y"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Save should return ErrNotImplemented, got %v", err)
	}
	if _, err := v.Get("x"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Get should return ErrNotImplemented, got %v", err)
	}
	if _, err := v.List(); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("List should return ErrNotImplemented, got %v", err)
	}
	if err := v.Remove("x"); err != nil {
		t.Errorf("Remove should be idempotent on stub, got %v", err)
	}
}

var _ Vault = stubVault{}