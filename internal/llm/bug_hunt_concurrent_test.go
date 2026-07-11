package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestCache_ConcurrentReadsAndWrites_NoRace exercises the cache
// from multiple goroutines without the -race detector (which
// requires CGO and is therefore unavailable in this CGO-free
// build). The test verifies that concurrent reads and writes
// don't corrupt the entry map; it cannot detect data races
// per se, but the cache's RWMutex is the synchronization
// primitive and the test will fail if any of the goroutines
// panic (which they would on a concurrent map write).
func TestCache_ConcurrentReadsAndWrites_NoRace(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewCache(dir+"/cache.json", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 16
	const ops = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				// Mix reads and writes.
				if (gid+i)%3 == 0 {
					_, _, _ = cache.Get("m", "s", "u")
				} else {
					_ = cache.Set("m", "s", "u", "response text")
				}
			}
		}(g)
	}
	wg.Wait()

	// After all goroutines, the cache should have a single
	// entry (same key) with the last-write-wins content.
	text, hit, err := cache.Get("m", "s", "u")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !hit {
		t.Fatal("expected hit after writes")
	}
	if text != "response text" {
		t.Errorf("unexpected text: %q", text)
	}
}

// TestCompleteJSONWithRetry_ConcurrentCalls_Independent exercises
// the retry chain from multiple goroutines. Each goroutine
// uses its own httptest server (so they don't share state) and
// its own client/cache. The test verifies the retry counter
// is per-call, not shared.
func TestCompleteJSONWithRetry_ConcurrentCalls_Independent(t *testing.T) {
	const goroutines = 8
	var wg sync.WaitGroup
	wg.Add(goroutines)
	results := make([]*RefusalResult, goroutines)
	errs := make([]error, goroutines)
	servers := make([]*httptest.Server, goroutines)

	for i := 0; i < goroutines; i++ {
		// Per-goroutine server with a per-server call counter.
		// Returns refuse, refuse, success in that order so
		// the retry chain sees the same 3-step pattern
		// regardless of cross-goroutine ordering.
		var callCount int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&callCount, 1)
			if n <= 2 {
				w.Write([]byte(`{"content":[{"type":"text","text":"I'm sorry, but I cannot help with that request."}],"stop_reason":"end_turn"}`))
				return
			}
			w.Write([]byte(`{"content":[{"type":"text","text":"{\"match\": 0.5, \"voice_match\": true, \"issues\": [], \"reasoning\": \"ok\"}"}],"stop_reason":"end_turn"}`))
		}))
		servers[i] = srv

		c := &Client{
			Model:   "test-model",
			BaseURL: srv.URL,
			APIKey:  "test-key",
			HTTP:    srv.Client(),
		}

		go func(i int) {
			defer wg.Done()
			var v struct {
				Match float32 `json:"match"`
			}
			res, err := c.CompleteJSONWithRetry(context.Background(), nil, "system", "user", &v, 2)
			results[i] = res
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for _, srv := range servers {
		srv.Close()
	}

	for i, r := range results {
		if errs[i] != nil {
			t.Errorf("goroutine %d: err = %v", i, errs[i])
			continue
		}
		if r.RefusedAttempts != 2 {
			t.Errorf("goroutine %d: expected 2 refused attempts, got %d", i, r.RefusedAttempts)
		}
		if r.Attempts != 3 {
			t.Errorf("goroutine %d: expected 3 attempts, got %d", i, r.Attempts)
		}
	}
}
