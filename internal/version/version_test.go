package version

import (
	"encoding/json"
	"strings"
	"testing"
)

// withBuildVersion temporarily overrides the package-private buildVersion
// for the duration of a test. The override is reset via t.Cleanup so
// parallel tests do not see stale state.
func withBuildVersion(t *testing.T, v string) {
	t.Helper()
	prev := buildVersion
	buildVersion = v
	resetMemoization()
	t.Cleanup(func() {
		buildVersion = prev
		resetMemoization()
	})
}

func TestResolve_LDFlagsTakesPriority(t *testing.T) {
	withBuildVersion(t, "0.9.0")
	got := Resolve()
	if got.Version != "0.9.0" {
		t.Errorf("Version = %q, want %q", got.Version, "0.9.0")
	}
	if got.Source != "ldflags" {
		t.Errorf("Source = %q, want %q", got.Source, "ldflags")
	}
	if got.IsDev {
		t.Error("IsDev = true with ldflags injection, want false")
	}
}

func TestResolve_EmptyLDFlagsFallsBackToDev(t *testing.T) {
	withBuildVersion(t, "")
	got := Resolve()
	if got.Version != "dev" {
		t.Errorf("Version = %q, want %q", got.Version, "dev")
	}
	if got.Source != "dev" {
		t.Errorf("Source = %q, want %q", got.Source, "dev")
	}
	if !got.IsDev {
		t.Error("IsDev = false with empty ldflags, want true")
	}
}

func TestResolve_DevStringInLDFlagsStillFallsBackToDev(t *testing.T) {
	// "dev" is the documented sentinel. If someone injects the literal
	// "dev" via -ldflags we treat it like no injection: IsDev stays true
	// so the drift warning is still emitted.
	withBuildVersion(t, "dev")
	got := Resolve()
	if !got.IsDev {
		t.Error("IsDev = false with ldflags=dev, want true (sentinel)")
	}
	if got.Source != "dev" {
		t.Errorf("Source = %q, want %q", got.Source, "dev")
	}
}

func TestResolve_Memoized(t *testing.T) {
	withBuildVersion(t, "0.9.0")
	first := Resolve()
	second := Resolve()
	if first != second {
		t.Errorf("Resolve not memoized: first=%+v second=%+v", first, second)
	}
}

func TestResolved_String(t *testing.T) {
	if got := (Resolved{Version: "0.9.0"}).String(); got != "0.9.0" {
		t.Errorf("String = %q, want %q", got, "0.9.0")
	}
	if got := (Resolved{}).String(); got != "dev" {
		t.Errorf("String = %q on empty, want %q", got, "dev")
	}
}

func TestResolved_JSONShape(t *testing.T) {
	r := Resolved{Version: "0.9.0", Commit: "abc1234", Source: "ldflags"}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"version":"0.9.0"`) {
		t.Errorf("json missing version: %s", s)
	}
	if !strings.Contains(s, `"source":"ldflags"`) {
		t.Errorf("json missing source: %s", s)
	}
}

func TestBuildVersion_ExposesInjectedValue(t *testing.T) {
	withBuildVersion(t, "0.9.0-rc.1")
	if got := BuildVersion(); got != "0.9.0-rc.1" {
		t.Errorf("BuildVersion = %q, want %q", got, "0.9.0-rc.1")
	}
}
