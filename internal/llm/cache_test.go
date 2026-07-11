package llm

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestCache_roundtrip verifies Get returns the value Set stored.
func TestCache_roundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	c, err := NewCache(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Clear()

	if err := c.Set("m1", "sys", "user1", "hello"); err != nil {
		t.Fatal(err)
	}
	got, hit, err := c.Get("m1", "sys", "user1")
	if err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("expected cache hit")
	}
	if got != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

// TestCache_miss_returnsFalse ensures Get returns hit=false on miss.
func TestCache_miss_returnsFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	c, _ := NewCache(path, time.Hour)
	defer c.Clear()

	_, hit, _ := c.Get("m1", "sys", "different")
	if hit {
		t.Error("expected miss")
	}
}

// TestCache_ttl_expired verifies entries older than TTL are evicted.
func TestCache_ttl_expired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	c, _ := NewCache(path, 10*time.Millisecond)
	defer c.Clear()

	_ = c.Set("m", "s", "u", "v")
	time.Sleep(50 * time.Millisecond)
	_, hit, _ := c.Get("m", "s", "u")
	if hit {
		t.Error("expected expired entry to miss")
	}
	st := c.Stats()
	if st.Evictions == 0 {
		t.Error("expected eviction count > 0")
	}
}

// TestCache_persists_across_reopen verifies the file survives a close+reopen.
func TestCache_persists_across_reopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	c1, err := NewCache(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := c1.Set("m", "s", "u", "value-persisted"); err != nil {
		t.Fatal(err)
	}

	// Reopen and verify the entry survived.
	c2, err := NewCache(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Clear()
	got, hit, _ := c2.Get("m", "s", "u")
	if !hit || got != "value-persisted" {
		t.Errorf("expected persisted value, got hit=%v got=%q", hit, got)
	}
}

// TestCache_disabled_when_path_empty verifies nil path returns nil cache.
func TestCache_disabled_when_path_empty(t *testing.T) {
	c, err := NewCache("", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if c != nil {
		t.Error("expected nil cache for empty path")
	}
}

// TestCache_concurrent_safe fires many goroutines at the same cache to
// ensure no data races (run with `go test -race`).
func TestCache_concurrent_safe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	c, _ := NewCache(path, time.Hour)
	defer c.Clear()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = c.Set("m", "s", "user", "value")
			_, _, _ = c.Get("m", "s", "user")
		}(i)
	}
	wg.Wait()
	st := c.Stats()
	if st.Sets == 0 {
		t.Error("expected sets > 0")
	}
}

// TestCache_stats_track verifies hits/misses/sets are counted.
func TestCache_stats_track(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	c, _ := NewCache(path, time.Hour)
	defer c.Clear()

	_ = c.Set("m", "s", "u", "v")
	_, _, _ = c.Get("m", "s", "u")        // hit
	_, _, _ = c.Get("m", "s", "u")        // hit
	_, _, _ = c.Get("m", "s", "different") // miss

	st := c.Stats()
	if st.Hits != 2 {
		t.Errorf("Hits = %d, want 2", st.Hits)
	}
	if st.Misses != 1 {
		t.Errorf("Misses = %d, want 1", st.Misses)
	}
	if st.Sets != 1 {
		t.Errorf("Sets = %d, want 1", st.Sets)
	}
}

// TestKey_deterministic verifies the same input produces the same key.
func TestKey_deterministic(t *testing.T) {
	k1 := Key("m", "s", "u")
	k2 := Key("m", "s", "u")
	if k1 != k2 {
		t.Errorf("keys differ: %s vs %s", k1, k2)
	}
	if k1 == Key("m", "s", "u2") {
		t.Error("different user produced same key")
	}
	if k1 == Key("m2", "s", "u") {
		t.Error("different model produced same key")
	}
}