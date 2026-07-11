package constitution

import "testing"

// TestResolve_emptySpecReturnsLight is the no-arg path. Empty spec
// must resolve to Light, not error.
func TestResolve_emptySpecReturnsLight(t *testing.T) {
	c, usedBuiltin, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if c != Light {
		t.Errorf("Resolve(\"\") returned a different *Constitution than Light")
	}
	if !usedBuiltin {
		t.Error("usedBuiltin should be true for empty spec")
	}
}

// TestResolve_builtinAliases verifies the documented aliases for
// the built-in constitutions. Each one must return the same
// pointer as the global Light or Dark.
func TestResolve_builtinAliases(t *testing.T) {
	aliases := []string{"light", "builtin:light", "dark-research/light", "dark-research/light@1.0.0"}
	for _, a := range aliases {
		t.Run(a, func(t *testing.T) {
			c, usedBuiltin, err := Resolve(a)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", a, err)
			}
			if c != Light {
				t.Errorf("Resolve(%q) did not return Light", a)
			}
			if !usedBuiltin {
				t.Errorf("Resolve(%q): usedBuiltin = false, want true", a)
			}
		})
	}
}

// TestResolve_darkAliasesRejectOnStockBuild verifies that on a stock
// build (Dark == nil), the dark aliases return a clear error rather
// than silently falling back to Light. On a tagged build, Dark is
// non-nil and the same aliases resolve to Dark.
func TestResolve_darkAliasesRejectOnStockBuild(t *testing.T) {
	darkAliases := []string{"dark", "builtin:dark", "dark-research/aggressive", "dark-research/aggressive@1.0.0"}
	for _, a := range darkAliases {
		t.Run(a, func(t *testing.T) {
			c, _, err := Resolve(a)
			if Dark == nil {
				if err == nil {
					t.Errorf("Resolve(%q) on stock build: expected error, got %v", a, c)
				}
				return
			}
			// Tagged build: must resolve to Dark.
			if err != nil {
				t.Fatalf("Resolve(%q) on tagged build: %v", a, err)
			}
			if c != Dark {
				t.Errorf("Resolve(%q) returned %v, want Dark", a, c)
			}
		})
	}
}

// TestResolve_userAliasPathMissing is the error path for a
// "user/<id>" spec that has no file in the user dir.
func TestResolve_userAliasPathMissing(t *testing.T) {
	c, _, err := Resolve("user/this-does-not-exist-anywhere")
	if err == nil {
		t.Fatalf("expected error for missing user file, got c = %v", c)
	}
}

// TestResolve_unrecognizedSpec is the catch-all error path.
func TestResolve_unrecognizedSpec(t *testing.T) {
	c, _, err := Resolve("not-a-real-thing-at-all")
	if err == nil {
		t.Fatalf("expected error for unrecognized spec, got c = %v", c)
	}
}

// TestSetActiveAndGet verifies the singleton pattern: SetActive
// installs a constitution; Active returns it. Without SetActive,
// Active returns Light.
func TestSetActiveAndGet(t *testing.T) {
	// Initialize() is the production-time hook that populates Dark
	// from the build-tag-gated loadDark wire-up. main() always
	// calls it; tests must too if they want to exercise the dark
	// global.
	Initialize()

	// Reset to nil (the initial state).
	activeMu.Lock()
	prev := active
	activeMu.Unlock()
	t.Cleanup(func() { SetActive(prev) })

	// With nothing set, Active returns Light.
	SetActive(nil)
	if got := Active(); got != Light {
		t.Errorf("Active() with nil set = %v, want Light", got)
	}

	// SetActive(Dark) and verify Active returns Dark. Skipped on
	// stock builds where Dark is nil.
	if Dark != nil {
		SetActive(Dark)
		if got := Active(); got != Dark {
			t.Errorf("Active() after SetActive(Dark) = %v, want Dark", got)
		}
	} else {
		t.Log("Dark is nil (stock build); skipping SetActive(Dark) assertion")
	}

	// SetActive(Light) and verify.
	SetActive(Light)
	if got := Active(); got != Light {
		t.Errorf("Active() after SetActive(Light) = %v, want Light", got)
	}
}

// TestSetActiveFromFlag_isEquivalentToSetActive verifies the
// flag-resolved path is consistent with the direct SetActive path.
func TestSetActiveFromFlag_isEquivalentToSetActive(t *testing.T) {
	prev := Active()
	t.Cleanup(func() { SetActive(prev) })

	c, err := SetActiveFromFlag("")
	if err != nil {
		t.Fatal(err)
	}
	if c != Light {
		t.Errorf("SetActiveFromFlag(\"\") = %v, want Light", c)
	}
	if got := Active(); got != Light {
		t.Errorf("Active() after = %v, want Light", got)
	}
}
