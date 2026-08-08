package vault

import (
	"errors"
	"strings"
	"testing"
)

// fakeVault is a deterministic Vault implementation for policy tests.
type fakeVault struct {
	secrets map[string]string
	errs    map[string]error
}

func (f *fakeVault) Save(name, value string) error { f.secrets[name] = value; return nil }
func (f *fakeVault) Get(name string) (string, error) {
	if e, ok := f.errs[name]; ok {
		return "", e
	}
	if v, ok := f.secrets[name]; ok {
		return v, nil
	}
	return "", ErrNotFound
}
func (f *fakeVault) List() ([]string, error) {
	var out []string
	for k := range f.secrets {
		out = append(out, k)
	}
	return out, nil
}
func (f *fakeVault) Remove(name string) error { delete(f.secrets, name); return nil }

// TestLoadIntoEnvWith_policy is the table-driven policy test for the
// env-loading contract. Every branch of loadIntoEnvWith is exercised
// deterministically (injectable vault + getenv/setenv), which kills the
// loop/error mutants (continue->break/empty, error-return blanking,
// ErrNotImplemented->false).
func TestLoadIntoEnvWith_policy(t *testing.T) {
	cases := []struct {
		name    string
		v       Vault
		initial map[string]string // pre-populated env (caller-provided wins)
		names   []string
		want    map[string]string // expected env after the call
	}{
		{
			name:    "caller wins over vault",
			v:       &fakeVault{secrets: map[string]string{"KEY": "from-vault"}},
			initial: map[string]string{"KEY": "from-caller"},
			names:   []string{"KEY"},
			want:    map[string]string{"KEY": "from-caller"},
		},
		{
			name:  "set from vault when env empty",
			v:     &fakeVault{secrets: map[string]string{"KEY": "from-vault"}},
			names: []string{"KEY"},
			want:  map[string]string{"KEY": "from-vault"},
		},
		{
			name:  "silent skip on ErrNotFound",
			v:     &fakeVault{},
			names: []string{"MISSING"},
			want:  map[string]string{},
		},
		{
			name:  "silent skip on ErrNotImplemented",
			v:     &fakeVault{errs: map[string]error{"KEY": ErrNotImplemented}},
			names: []string{"KEY"},
			want:  map[string]string{},
		},
		{
			name:    "continue loads later names after caller-provided",
			v:       &fakeVault{secrets: map[string]string{"A": "va", "B": "vb"}},
			initial: map[string]string{"A": "from-caller"},
			names:   []string{"A", "B"},
			want:    map[string]string{"A": "from-caller", "B": "vb"},
		},
		{
			name:  "continue loads later names after not-found skip",
			v:     &fakeVault{secrets: map[string]string{"B": "vb"}},
			names: []string{"A", "B"},
			want:  map[string]string{"B": "vb"},
		},
		{
			name:  "multiple loads in one pass",
			v:     &fakeVault{secrets: map[string]string{"A": "va", "B": "vb", "C": "vc"}},
			names: []string{"A", "B", "C"},
			want:  map[string]string{"A": "va", "B": "vb", "C": "vc"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{}
			for k, v := range tc.initial {
				env[k] = v
			}
			getenv := func(k string) string { return env[k] }
			setenv := func(k, v string) error { env[k] = v; return nil }

			if err := loadIntoEnvWith(tc.v, tc.names, getenv, setenv); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for k, wantV := range tc.want {
				if env[k] != wantV {
					t.Errorf("env[%q] = %q, want %q", k, env[k], wantV)
				}
			}
		})
	}
}

// TestLoadIntoEnvWith_getErrorPropagates pins that a non-skip Get error
// is returned (wrapped with the failing name).
func TestLoadIntoEnvWith_getErrorPropagates(t *testing.T) {
	v := &fakeVault{errs: map[string]error{"KEY": errors.New("backend down")}}
	getenv := func(string) string { return "" }
	setenv := func(k, v string) error { return nil }
	err := loadIntoEnvWith(v, []string{"KEY"}, getenv, setenv)
	if err == nil {
		t.Fatal("expected get error to propagate, got nil")
	}
	if !strings.HasPrefix(err.Error(), "vault: load ") || !strings.Contains(err.Error(), "KEY") {
		t.Fatalf("error should be wrapped as vault: load KEY, got %v", err)
	}
}

// TestLoadIntoEnvWith_setenvErrorPropagates pins that a Setenv failure
// is returned (wrapped with the failing name).
func TestLoadIntoEnvWith_setenvErrorPropagates(t *testing.T) {
	v := &fakeVault{secrets: map[string]string{"KEY": "v"}}
	getenv := func(string) string { return "" }
	setenv := func(k, v string) error { return errors.New("setenv refused") }
	err := loadIntoEnvWith(v, []string{"KEY"}, getenv, setenv)
	if err == nil {
		t.Fatal("expected setenv error to propagate, got nil")
	}
	if !strings.HasPrefix(err.Error(), "vault: setenv ") || !strings.Contains(err.Error(), "KEY") {
		t.Fatalf("error should be wrapped as vault: setenv KEY, got %v", err)
	}
}

// TestValidateName_whitespaceOnly pins the TrimSpace branch: a name
// that is only whitespace must be rejected as empty. Without this, a
// mutation blanking `name = strings.TrimSpace(name)` survives.
func TestValidateName_whitespaceOnly(t *testing.T) {
	for _, n := range []string{"   ", "\t", "\n", " \t "} {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) should fail (whitespace-only -> empty)", n)
		}
	}
	// Trimmed names with invalid chars still fail after trim.
	if err := ValidateName("  a/b  "); err == nil {
		t.Error("ValidateName(\"  a/b  \") should fail after trim")
	}
	// Trimmed good name passes.
	if err := ValidateName("  GOOD_KEY  "); err != nil {
		t.Errorf("ValidateName(\"  GOOD_KEY  \") should pass after trim, got %v", err)
	}
}
