package safety

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// SSRF blocklist logic (safety.go) — strong, exact assertions.
//
// These tests are the mutation-testing target for the safety package:
// every blocked CIDR is exercised with its EXACT expected error kind so a
// mutation that removes a range, weakens a comparison, or skips the
// classification branch gets killed.
// ---------------------------------------------------------------------------

// blockedV4Cases maps a representative IP inside each blockedV4 CIDR to
// the exact error kind checkIP must return. The classification branch
// (safety.go checkIP: IsLoopback/IsPrivate/IsLinkLocal*) decides
// ErrPrivateLiteral vs ErrBlockedIP.
var blockedV4Cases = []struct {
	name string
	ip   string
	want error
}{
	{"0.0.0.0/8", "http://0.0.0.1/", ErrBlockedIP},
	{"10.0.0.0/8", "http://10.1.2.3/", ErrPrivateLiteral},
	{"100.64.0.0/10 (CGNAT)", "http://100.64.0.1/", ErrBlockedIP},
	{"127.0.0.0/8 loopback", "http://127.0.0.1/", ErrPrivateLiteral},
	{"169.254.0.0/16 metadata", "http://169.254.169.254/latest/meta-data/", ErrPrivateLiteral},
	{"172.16.0.0/12", "http://172.31.255.255/", ErrPrivateLiteral},
	{"192.0.0.0/24", "http://192.0.0.9/", ErrBlockedIP},
	{"192.0.2.0/24 TEST-NET-1", "http://192.0.2.1/", ErrBlockedIP},
	{"192.88.99.0/24", "http://192.88.99.1/", ErrBlockedIP},
	{"192.168.0.0/16", "http://192.168.0.1/", ErrPrivateLiteral},
	{"198.18.0.0/15", "http://198.18.0.1/", ErrBlockedIP},
	{"198.51.100.0/24 TEST-NET-2", "http://198.51.100.1/", ErrBlockedIP},
	{"203.0.113.0/24 TEST-NET-3", "http://203.0.113.1/", ErrBlockedIP},
	{"224.0.0.0/4 multicast", "http://224.0.0.1/", ErrPrivateLiteral},
	{"240.0.0.0/4 reserved", "http://240.0.0.1/", ErrBlockedIP},
}

var blockedV6Cases = []struct {
	name string
	ip   string
	want error
}{
	{"::/128 unspecified", "http://[::]/", ErrBlockedIP},
	{"::1/128 loopback", "http://[::1]/", ErrPrivateLiteral},
	{"64:ff9b::/96 NAT64", "http://[64:ff9b::1]/", ErrBlockedIP},
	{"100::/64 discard", "http://[100::1]/", ErrBlockedIP},
	{"2001::/23", "http://[2001:2::1]/", ErrBlockedIP},
	{"2001:db8::/32 doc", "http://[2001:db8::1]/", ErrBlockedIP},
	{"fc00::/7 ULA", "http://[fc00::1]/", ErrPrivateLiteral},
	{"fe80::/10 link-local", "http://[fe80::1]/", ErrPrivateLiteral},
	{"ff00::/8 multicast", "http://[ff02::1]/", ErrPrivateLiteral},
}

func TestValidateURL_blocksEveryBlockedV4CIDR(t *testing.T) {
	for _, tc := range blockedV4Cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateURL(tc.ip, false)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ValidateURL(%q) err = %v, want errors.Is(%v)", tc.ip, err, tc.want)
			}
		})
	}
}

func TestValidateURL_blocksEveryBlockedV6CIDR(t *testing.T) {
	for _, tc := range blockedV6Cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateURL(tc.ip, false)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ValidateURL(%q) err = %v, want errors.Is(%v)", tc.ip, err, tc.want)
			}
		})
	}
}

// TestValidateURL_allowsPublicIPs pins the negative space: public
// addresses must NOT be blocked. A mutation that makes the blocklist
// match everything (e.g. dropping the To4 conversion for IPv4-mapped
// IPv6) breaks these.
func TestValidateURL_allowsPublicIPs(t *testing.T) {
	for _, tc := range []struct{ name, ip string }{
		{"v4 public", "http://8.8.8.8/"},
		{"v4 public cloudflare", "http://1.1.1.1/"},
		{"v6 public", "http://[2606:4700:4700::1111]/"},
		{"v4-mapped public", "http://[::ffff:8.8.8.8]/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateURL(tc.ip, false); err != nil {
				t.Fatalf("ValidateURL(%q) unexpectedly blocked: %v", tc.ip, err)
			}
		})
	}
}

// TestValidateURL_ipv4MappedIPv6_keepsV4Semantics is the regression
// guard for the To4() conversion in checkIP. Without it,
// ::ffff:0:0/96 in a naive blocklist would match every IPv4 address —
// including perfectly safe public ones.
func TestValidateURL_ipv4MappedIPv6_keepsV4Semantics(t *testing.T) {
	// Public v4 as IPv4-mapped IPv6 → allowed.
	if _, err := ValidateURL("http://[::ffff:8.8.8.8]/", false); err != nil {
		t.Fatalf("public v4-mapped must be allowed, got %v", err)
	}
	// Private v4 as IPv4-mapped IPv6 → still blocked as private.
	if _, err := ValidateURL("http://[::ffff:10.0.0.1]/", false); !errors.Is(err, ErrPrivateLiteral) {
		t.Fatalf("private v4-mapped must be ErrPrivateLiteral, got %v", err)
	}
}

// TestValidateURL_allowLoopback_onlyRelaxesLoopback pins the debug
// switch: it must NOT open the door to all private ranges.
func TestValidateURL_allowLoopback_onlyRelaxesLoopback(t *testing.T) {
	for _, tc := range []struct {
		name, ip string
		want     error
	}{
		{"v4 loopback allowed", "http://127.0.0.1/", nil},
		{"v6 loopback allowed", "http://[::1]/", nil},
		{"v4 private STILL blocked", "http://10.0.0.1/", ErrPrivateLiteral},
		{"v6 ULA STILL blocked", "http://[fd00::1]/", ErrPrivateLiteral},
		{"link-local STILL blocked", "http://169.254.169.254/", ErrPrivateLiteral},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateURL(tc.ip, true)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ValidateURL(%q, true) err = %v, want %v", tc.ip, err, tc.want)
			}
		})
	}
}

// TestValidateURL_schemeCaseInsensitive pins the strings.ToLower in the
// scheme switch: "HTTP://" must be accepted; removing ToLower makes it
// a blocked scheme.
func TestValidateURL_schemeCaseInsensitive(t *testing.T) {
	if _, err := ValidateURL("HTTP://8.8.8.8/", false); err != nil {
		t.Fatalf("uppercase HTTP scheme must be accepted, got %v", err)
	}
	if _, err := ValidateURL("HtTpS://8.8.8.8/", false); err != nil {
		t.Fatalf("mixed-case HTTPS scheme must be accepted, got %v", err)
	}
	for _, u := range []string{"ftp://8.8.8.8/", "file:///etc/passwd", "data:text/plain,hi", "javascript:alert(1)"} {
		if _, err := ValidateURL(u, false); !errors.Is(err, ErrSchemeBlocked) {
			t.Errorf("ValidateURL(%q) err = %v, want ErrSchemeBlocked", u, err)
		}
	}
}

// TestValidateURL_noHost pins the ErrNoHost branch (u.Hostname()=="").
func TestValidateURL_noHost(t *testing.T) {
	for _, u := range []string{"http:///path", "http://", "https://?q=1"} {
		if _, err := ValidateURL(u, false); !errors.Is(err, ErrNoHost) {
			t.Errorf("ValidateURL(%q) err = %v, want ErrNoHost", u, err)
		}
	}
}

// TestValidateURL_embeddedCredentials pins the Password() check: only
// user:pass@ (a password present) is rejected; a bare username is a
// non-blocking oddity.
func TestValidateURL_embeddedCredentials(t *testing.T) {
	if _, err := ValidateURL("https://user:pass@8.8.8.8/", false); !errors.Is(err, ErrEmbeddedCreds) {
		t.Fatalf("user:pass@ must be ErrEmbeddedCreds, got %v", err)
	}
	if _, err := ValidateURL("https://user@8.8.8.8/", false); err != nil {
		t.Fatalf("bare username should not be rejected, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Domain resolution (checkResolved) — deterministic via lookupHost stub.
// ---------------------------------------------------------------------------

// withLookupHost swaps the package lookupHost var and restores it.
func withLookupHost(t *testing.T, fn func(host string) ([]string, error)) {
	t.Helper()
	prev := lookupHost
	lookupHost = fn
	t.Cleanup(func() { lookupHost = prev })
}

func TestValidateURL_domainResolvesToBlockedIP(t *testing.T) {
	withLookupHost(t, func(host string) ([]string, error) {
		if host != "internal.example" {
			t.Fatalf("lookupHost called with %q, want internal.example", host)
		}
		return []string{"10.0.0.1"}, nil
	})
	_, err := ValidateURL("http://internal.example/", false)
	if !errors.Is(err, ErrPrivateLiteral) {
		t.Fatalf("domain resolving to 10.0.0.1 must be ErrPrivateLiteral, got %v", err)
	}
}

func TestValidateURL_domainResolvesToPublic(t *testing.T) {
	withLookupHost(t, func(host string) ([]string, error) {
		return []string{"8.8.8.8", "1.1.1.1"}, nil
	})
	if _, err := ValidateURL("https://public.example/", false); err != nil {
		t.Fatalf("domain resolving to public IPs must pass, got %v", err)
	}
}

// TestValidateURL_domainOneBadAddressAmongGood pins the loop: a single
// blocked address among public ones must still reject the URL.
func TestValidateURL_domainOneBadAddressAmongGood(t *testing.T) {
	withLookupHost(t, func(host string) ([]string, error) {
		return []string{"8.8.8.8", "169.254.169.254", "1.1.1.1"}, nil
	})
	_, err := ValidateURL("http://mixed.example/", false)
	if !errors.Is(err, ErrPrivateLiteral) {
		t.Fatalf("one link-local among public must reject, got %v", err)
	}
}

// TestValidateURL_domainLookupError pins the ErrNoHost wrap when DNS
// itself fails.
func TestValidateURL_domainLookupError(t *testing.T) {
	withLookupHost(t, func(host string) ([]string, error) {
		return nil, errors.New("nxdomain")
	})
	_, err := ValidateURL("http://nonexistent.example/", false)
	if !errors.Is(err, ErrNoHost) {
		t.Fatalf("DNS failure must wrap ErrNoHost, got %v", err)
	}
}

// TestCheckResolved_skipsNonIPEntries pins the `if ip == nil { continue }`
// guard directly.
func TestCheckResolved_skipsNonIPEntries(t *testing.T) {
	if err := checkResolved([]string{"not-an-ip", "8.8.8.8"}, false); err != nil {
		t.Fatalf("garbage entry must be skipped, got %v", err)
	}
	if err := checkResolved([]string{"not-an-ip", "10.0.0.1"}, false); !errors.Is(err, ErrPrivateLiteral) {
		t.Fatalf("blocked IP after garbage must be caught, got %v", err)
	}
	if err := checkResolved(nil, false); err != nil {
		t.Fatalf("empty addrs must pass, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Exact-output contracts.
// ---------------------------------------------------------------------------

// TestWrapUntrusted_exactFormat pins the full wire format of the trust
// boundary: source quoted, fetched_at as RFC3339 UTC, content on its own
// line, closing tag. Substring checks can't catch a broken newline or a
// dropped .UTC().
func TestWrapUntrusted_exactFormat(t *testing.T) {
	at := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	got := WrapUntrusted("https://x.com", at, "hello")
	want := "<fetched_content source=\"https://x.com\" fetched_at=\"2026-08-07T12:00:00Z\" trust=\"untrusted\">\nhello\n</fetched_content>"
	if got != want {
		t.Fatalf("WrapUntrusted mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestWrapUntrusted_utcNormalization pins the .UTC() call: a local-time
// input must be rendered as UTC RFC3339.
func TestWrapUntrusted_utcNormalization(t *testing.T) {
	local := time.Date(2026, 8, 7, 7, 0, 0, 0, time.FixedZone("UTC-5", -5*3600))
	got := WrapUntrusted("s", local, "x")
	if !strings.Contains(got, `fetched_at="2026-08-07T12:00:00Z"`) {
		t.Fatalf("expected UTC-normalized timestamp, got: %s", got)
	}
}

// TestSafetyError_ErrorFormat pins SafetyError.Error()'s exact string.
func TestSafetyError_ErrorFormat(t *testing.T) {
	e := &SafetyError{Reason: "blocked", Detail: "http://x"}
	if got, want := e.Error(), "safety: blocked: http://x"; got != want {
		t.Fatalf("SafetyError.Error() = %q, want %q", got, want)
	}
}

// TestAllBlocked_containsV4ThenV6 pins allBlocked() to the full list in
// order — a mutation that drops a range changes the length/order.
func TestAllBlocked_containsV4ThenV6(t *testing.T) {
	got := allBlocked()
	if len(got) != len(blockedV4)+len(blockedV6) {
		t.Fatalf("allBlocked() length = %d, want %d", len(got), len(blockedV4)+len(blockedV6))
	}
	for i, c := range blockedV4 {
		if got[i] != c {
			t.Errorf("allBlocked()[%d] = %q, want %q (v4 block)", i, got[i], c)
		}
	}
	for i, c := range blockedV6 {
		if got[len(blockedV4)+i] != c {
			t.Errorf("allBlocked()[%d] = %q, want %q (v6 block)", len(blockedV4)+i, got[len(blockedV4)+i], c)
		}
	}
}

// TestValidateURL_returnsParsedURL pins that ValidateURL returns the
// parsed URL (not just nil) on success — callers use it to build the
// fetch request.
func TestValidateURL_returnsParsedURL(t *testing.T) {
	withLookupHost(t, func(host string) ([]string, error) { return []string{"8.8.8.8"}, nil })
	u, err := ValidateURL("https://example.com/path?q=1#frag", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.String() != "https://example.com/path?q=1#frag" {
		t.Fatalf("returned URL = %q, want original round-trip", u.String())
	}
	if u.Hostname() != "example.com" {
		t.Fatalf("returned URL hostname = %q, want example.com", u.Hostname())
	}
}

// TestValidateURL_malformedURL_returnsErrInvalidURL exercises the
// url.Parse failure branch. Without a test that triggers a real parse
// error, a mutation blanking that branch survives (the error return is
// never executed).
func TestValidateURL_malformedURL_returnsErrInvalidURL(t *testing.T) {
	for _, u := range []string{"://bad", "http://[::1", "http://%zz"} {
		if _, err := ValidateURL(u, false); !errors.Is(err, ErrInvalidURL) {
			t.Errorf("ValidateURL(%q) err = %v, want ErrInvalidURL", u, err)
		}
	}
}

// TestValidateURL_noHost_stillFailsWithResolvingStub pins the ErrNoHost
// branch independent of DNS: even when the resolver stub succeeds, a
// host-less URL must fail with ErrNoHost — NOT fall through to the
// domain path. Without this, a mutation blanking the ErrNoHost return
// survives because the real DNS path wraps ErrNoHost and errors.Is
// still matches.
func TestValidateURL_noHost_stillFailsWithResolvingStub(t *testing.T) {
	withLookupHost(t, func(host string) ([]string, error) {
		t.Fatalf("lookupHost must not be called for a host-less URL, got %q", host)
		return nil, nil
	})
	for _, u := range []string{"http:///path", "http://", "https://?q=1"} {
		if _, err := ValidateURL(u, false); !errors.Is(err, ErrNoHost) {
			t.Errorf("ValidateURL(%q) err = %v, want ErrNoHost", u, err)
		}
	}
}

// TestValidateURL_ipLiteral_blockedBeforeDns pins that IP literals are
// blocked by checkIP BEFORE any resolution: even when the resolver stub
// would return a public address, a private literal must still fail. A
// mutation blanking the IP-literal branch falls through to the stub and
// returns nil — this test kills it.
func TestValidateURL_ipLiteral_blockedBeforeDns(t *testing.T) {
	withLookupHost(t, func(host string) ([]string, error) {
		t.Fatalf("lookupHost must not be called for an IP literal, got %q", host)
		return []string{"8.8.8.8"}, nil
	})
	for _, u := range []string{"http://10.0.0.1/", "http://[fd00::1]/"} {
		if _, err := ValidateURL(u, false); !errors.Is(err, ErrPrivateLiteral) {
			t.Errorf("ValidateURL(%q) err = %v, want ErrPrivateLiteral (must be caught pre-DNS)", u, err)
		}
	}
}
