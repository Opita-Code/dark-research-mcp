// Deterministic race test for Client.Complete() fallback swap.
// We inject a RoundTripper that returns 429 and pauses for 50ms to widen
// the race window, then call Complete from N goroutines simultaneously.
// We track the final state of c.APIKey and assert it should equal the
// original primary key.
//
// BUG: the swap pattern is not goroutine-safe.
package llm

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type slowRT struct {
	calls *atomic.Int64
}

func (rt *slowRT) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.calls.Add(1)
	time.Sleep(50 * time.Millisecond) // widen race window
	return &http.Response{
		StatusCode: 429,
		Status:     "429 Too Many Requests",
		Body:       http.NoBody,
		Header:     make(http.Header),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}, nil
}

func TestComplete_FallbackSwap_ConcurrentInterleave(t *testing.T) {
	calls := &atomic.Int64{}
	c := &Client{
		BaseURL:         "http://primary",
		APIKey:          "primary",
		Model:           "test",
		Provider:        ProviderAnthropic,
		HTTP:            &http.Client{Transport: &slowRT{calls: calls}, Timeout: 5 * time.Second},
		FallbackAPIKey:  "fallback",
		FallbackBaseURL: "http://fallback",
	}

	const N = 30
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, _ = c.Complete(ctx, "system", Message{Role: "user", Content: "x"})
		}()
	}
	close(start)
	wg.Wait()

	fmt.Printf("calls=%d final APIKey=%q final BaseURL=%q\n", calls.Load(), c.APIKey, c.BaseURL)

	// After all goroutines complete, c.APIKey MUST be back to "primary"
	// (and c.BaseURL to http://primary). If it's "fallback", we hit the bug.
	if c.APIKey != "primary" {
		t.Errorf("BUG CONFIRMED: c.APIKey=%q after %d concurrent fallback retries; want %q. Client state is permanently corrupted.",
			c.APIKey, calls.Load(), "primary")
	}
	if c.BaseURL != "http://primary" {
		t.Errorf("BUG CONFIRMED: c.BaseURL=%q after %d concurrent fallback retries; want http://primary.",
			c.BaseURL, calls.Load())
	}
}