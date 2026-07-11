package vault

import (
	"testing"
)

// Pure helpers (platform-independent).

func TestMask(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "<empty>"},
		{"abc", "***"},
		{"abcdefgh", "***"},
		{"abcdefghi", "abcd...fghi"},
		{"sk-1234567890abcdef", "sk-1...cdef"},
	}
	for _, c := range cases {
		got := Mask(c.in)
		if got != c.want {
			t.Errorf("Mask(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidateName(t *testing.T) {
	if err := ValidateName(""); err == nil {
		t.Error("empty name should fail")
	}
	if err := ValidateName("MINIMAX_API_KEY"); err != nil {
		t.Errorf("good name failed: %v", err)
	}
	if err := ValidateName("a/b"); err == nil {
		t.Error("name with / should fail")
	}
	if err := ValidateName("a\\b"); err == nil {
		t.Error("name with \\ should fail")
	}
	if err := ValidateName("a\nb"); err == nil {
		t.Error("name with newline should fail")
	}
}

func TestParseVaultList(t *testing.T) {
	input := `=== dark-agents-v2 vault diagnostic ===
Target prefix: dark-agents-v2/
Log file:      C:\Users\...\vault.log
Stored secrets:
  - MINIMAX_API_KEY
    preview: MINI...M3-K
  - BRAVE_API_KEY
    preview: BRAV...XY12
Last log lines:
  (no log yet)
`
	got := parseVaultList(input)
	want := []string{"MINIMAX_API_KEY", "BRAVE_API_KEY"}
	if len(got) != len(want) {
		t.Fatalf("parseVaultList: got %d names, want %d", len(got), len(want))
	}
	for i, n := range want {
		if got[i] != n {
			t.Errorf("name[%d] = %q, want %q", i, got[i], n)
		}
	}
}

func TestParseVaultList_empty(t *testing.T) {
	if got := parseVaultList("nothing here"); len(got) != 0 {
		t.Errorf("expected empty list, got %v", got)
	}
}

func TestOpen_returnsNonNil(t *testing.T) {
	v := Open()
	if v == nil {
		t.Fatal("Open() returned nil")
	}
}