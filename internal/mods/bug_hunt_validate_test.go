package mods

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateModPath_LegitimateDotDotNames_ documents that the
// validator must accept paths whose name components contain ".."
// as a prefix but are not ".." themselves (e.g. "..hidden" or
// "..bar.md"). Found during bug hunt v0.4.0-rc.4: the original
// implementation rejected these by string-prefix match.
func TestValidateModPath_LegitimateDotDotNames(t *testing.T) {
	cases := []string{
		"..hidden/file.md",
		"..bar.md",
		"..foo/..bar",
		"knowledge/..notes.md",
	}
	for _, c := range cases {
		if err := validateModPath(c); err != nil {
			t.Errorf("validateModPath(%q) = %v; want accept (legit name)", c, err)
		}
	}
}

// TestValidateModPath_TruePositives_ documents the cases that MUST
// be rejected: actual directory traversal. Found during bug hunt
// v0.4.0-rc.4: "foo/../bar" and "foo/bar/.." used to pass
// filepath.Clean and slip through (filepath.Clean collapses the
// ".." before the string check ran).
func TestValidateModPath_TruePositives(t *testing.T) {
	cases := []string{
		"..",
		"../etc/passwd",
		"../../etc/passwd",
		"foo/../../etc/passwd",
		"foo/../bar",
		"foo/bar/..",
		"foo/bar/../baz",
		"a/b/c/../d",
		// On Windows, also check backslash variants. (On Linux,
		// "foo/..\bar" is a single filename containing a backslash,
		// which is legal and is the correct behavior — we only
		// care about escape, not naming.)
	}
	for _, c := range cases {
		err := validateModPath(c)
		if err == nil {
			t.Errorf("validateModPath(%q) = nil; want reject", c)
			continue
		}
		if !strings.Contains(err.Error(), "escapes") {
			t.Errorf("validateModPath(%q) = %v; want 'escapes' message", c, err)
		}
	}
}

// TestValidateModPath_WindowsAbsolute_ documents behavior on
// Windows-style absolute paths (drive letter).
func TestValidateModPath_WindowsAbsolute(t *testing.T) {
	if filepath.Separator != '\\' {
		t.Skip("Windows-specific test")
	}
	cases := []string{
		`C:\Windows\System32`,
		`C:/foo/bar`,
	}
	for _, c := range cases {
		err := validateModPath(c)
		if err == nil {
			t.Errorf("validateModPath(%q) = nil; want reject (Windows absolute)", c)
		}
	}
}
