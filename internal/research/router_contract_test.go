package research

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRouter_URLhausPOST verifies the abuse.ch URLhaus contract:
// the host lookup is an HTTP POST with a form-encoded body and the
// `Auth-Key` header. The official API docs (urlhaus-api.abuse.ch)
// require the header on every endpoint and the host is sent as POST
// data, not as a path segment.
func TestRouter_URLhausPOST(t *testing.T) {
	var gotMethod, gotPath, gotAuthKey, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuthKey = r.Header.Get("Auth-Key")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query_status": "ok",
			"urlhaus_reference": "https://urlhaus.abuse.ch/host/example.com/",
			"host": "example.com",
			"firstseen": "2026-01-02 03:04:05 UTC",
			"url_count": "1",
			"urls": [
				{
					"id": "123456",
					"url": "http://example.com/payload.exe",
					"url_status": "online",
					"date_added": "2026-02-03 04:05:06 UTC",
					"threat": "malware_download",
					"reporter": "someone",
					"tags": ["exe"]
				}
			]
		}`))
	}))
	defer srv.Close()

	orig := Getenv
	Getenv = func(k string) string {
		if k == "URLHAUS_AUTH_KEY" {
			return "test-key"
		}
		return orig(k)
	}
	defer func() { Getenv = orig }()

	// Build a registry whose only backend is the URLhaus one,
	// pointed at the test server.
	reg := &Registry{backends: map[Intent][]Backend{}}
	be := threatBackends()[0]
	be.BaseURL = srv.URL + "/v1/host/"
	be.URLForQuery = func(string) string { return srv.URL + "/v1/host/" }
	reg.add(IntentThreat, be)

	r := NewRouter(reg, srv.Client())
	r.AllowLoopback = true
	res, err := r.Route(context.Background(), "example.com", IntentThreat)
	if err != nil {
		t.Fatalf("route: %v (errors=%+v)", err, res.Errors)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method: got %q, want POST", gotMethod)
	}
	if gotPath != "/v1/host/" {
		t.Errorf("path: got %q, want /v1/host/", gotPath)
	}
	if gotAuthKey != "test-key" {
		t.Errorf("Auth-Key header: got %q, want %q", gotAuthKey, "test-key")
	}
	if !strings.Contains(gotBody, "host=example.com") {
		t.Errorf("body: got %q, want it to contain host=example.com", gotBody)
	}
	// The Authorization header must NOT be used for this backend.
	if len(res.Items) == 0 {
		t.Fatalf("expected items, got none (errors=%+v)", res.Errors)
	}
	if res.Items[1].Title != "http://example.com/payload.exe" {
		t.Errorf("url item title: %q", res.Items[1].Title)
	}
}

// TestRouter_BackendSkip verifies that a backend can opt out of a
// query it cannot serve. The Wayback CDX backend archives URLs, so it
// must not attempt a topical keyword query.
func TestRouter_BackendSkip(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	reg := &Registry{backends: map[Intent][]Backend{}}
	reg.add(IntentNews, Backend{
		Name:        "skipped",
		Method:      "GET",
		URLForQuery: func(string) string { return srv.URL },
		Skip:        func(q string) bool { return q == "skip me" },
		Parse:       func([]byte) ([]Item, error) { return []Item{{Title: "x"}}, nil },
	})

	r := NewRouter(reg, srv.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := r.Route(ctx, "skip me", IntentNews)
	if err == nil {
		t.Fatal("expected an error: the only backend was skipped")
	}
	if called {
		t.Error("the skipped backend must not have been called")
	}
	if len(res.BackendsTried) != 0 {
		t.Errorf("expected no backends tried, got %v", res.BackendsTried)
	}
}
