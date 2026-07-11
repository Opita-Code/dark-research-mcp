package safety

import (
	"errors"
	"testing"
	"time"
)

func TestValidateURL_blocks_metadata_endpoint(t *testing.T) {
	// 169.254/16 covers AWS/GCP/Azure metadata service. Even if a public
	// search engine returns a link to it, we refuse to fetch.
	_, err := ValidateURL("http://169.254.169.254/latest/meta-data/", false)
	if err == nil {
		t.Fatal("expected error for metadata endpoint, got nil")
	}
	if !errors.Is(err, ErrBlockedIP) && !errors.Is(err, ErrPrivateLiteral) {
		t.Fatalf("expected ErrBlockedIP or ErrPrivateLiteral, got %v", err)
	}
}

func TestValidateURL_blocks_loopback(t *testing.T) {
	_, err := ValidateURL("http://127.0.0.1/admin", false)
	if err == nil {
		t.Fatal("expected error for loopback, got nil")
	}
}

func TestValidateURL_allows_loopback_when_opted_in(t *testing.T) {
	// Debug-only path; allow_loopback = true.
	if _, err := ValidateURL("http://127.0.0.1/", true); err != nil {
		t.Fatalf("loopback should be allowed when allow_loopback=true: %v", err)
	}
}

func TestValidateURL_blocks_file_scheme(t *testing.T) {
	_, err := ValidateURL("file:///etc/passwd", false)
	if !errors.Is(err, ErrSchemeBlocked) {
		t.Fatalf("expected ErrSchemeBlocked, got %v", err)
	}
}

func TestValidateURL_blocks_javascript_scheme(t *testing.T) {
	_, err := ValidateURL("javascript:alert(1)", false)
	if !errors.Is(err, ErrSchemeBlocked) {
		t.Fatalf("expected ErrSchemeBlocked, got %v", err)
	}
}

func TestValidateURL_blocks_embedded_credentials(t *testing.T) {
	_, err := ValidateURL("https://user:pass@example.com/", false)
	if !errors.Is(err, ErrEmbeddedCreds) {
		t.Fatalf("expected ErrEmbeddedCreds, got %v", err)
	}
}

func TestValidateURL_blocks_rfc1918(t *testing.T) {
	for _, u := range []string{
		"http://10.0.0.1/",
		"http://192.168.1.1/",
		"http://172.16.0.1/",
	} {
		if _, err := ValidateURL(u, false); err == nil {
			t.Errorf("expected RFC1918 IP %s to be blocked, got nil", u)
		}
	}
}

func TestValidateURL_allows_public(t *testing.T) {
	// example.com resolves to public IPs (Cloudflare anycast).
	if _, err := ValidateURL("https://example.com/", false); err != nil {
		t.Fatalf("public URL should validate, got %v", err)
	}
}

func TestWrapUntrusted_encloses_content(t *testing.T) {
	out := WrapUntrusted("https://x.com", time.Now().UTC(), "hello")
	for _, want := range []string{`<fetched_content`, `source="https://x.com"`, `trust="untrusted"`, `hello`, `</fetched_content>`} {
		if !contains(out, want) {
			t.Errorf("missing %q in wrapped output: %s", want, out)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}