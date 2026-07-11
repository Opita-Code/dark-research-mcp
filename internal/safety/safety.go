// Package safety implements URL validation and content wrapping primitives.
//
// Threat model: see mcp-research-workspace/findings/07-anti-prompt-injection.md.
package safety

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// blockedV4 is RFC 6890 + RFC 1918 + CGNAT + link-local (incl. cloud metadata
// 169.254/16) + loopback + multicast + broadcast.
var blockedV4 = []string{
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.88.99.0/24",
	"192.168.0.0/16",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
}

// blockedV6 is RFC 6890 special-purpose ranges + ULA + link-local + multicast.
// Note: ::ffff:0:0/96 (IPv4-mapped IPv6) is NOT included — Go's net.IPNet
// reinterprets it as /0 in v4 form after To4() conversion, which would
// match every IPv4 address. IPv4-mapped is a notation, not a blocklist.
var blockedV6 = []string{
	"::/128",
	"::1/128",
	"64:ff9b::/96",
	"100::/64",
	"2001::/23",
	"2001:db8::/32",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
}

// SafetyError describes why a URL failed validation.
type SafetyError struct {
	Reason string
	Detail string
}

func (e *SafetyError) Error() string {
	return fmt.Sprintf("safety: %s: %s", e.Reason, e.Detail)
}

var (
	ErrSchemeBlocked     = errors.New("URL scheme not allowed")
	ErrNoHost            = errors.New("URL has no host")
	ErrEmbeddedCreds     = errors.New("URL contains embedded credentials")
	ErrBlockedIP         = errors.New("URL resolves to blocked IP range")
	ErrPrivateLiteral    = errors.New("URL uses an IP literal that is private/reserved")
	ErrInvalidURL        = errors.New("URL parse error")
)

// ValidateURL checks a URL for SSRF-safety:
//
//   - Schemes allowed: http, https only.
//   - Rejects embedded credentials (user:pass@host).
//   - Resolves the host and blocks private/reserved/link-local/loopback
//     addresses (RFC 6890 + RFC 1918 + cloud metadata 169.254/16).
//   - If allowLoopback is true, 127.0.0.0/8 and ::1 are allowed (debug only).
//
// Resolution is performed synchronously; call sites in long-running services
// should invoke this in a goroutine if latency matters.
func ValidateURL(raw string, allowLoopback bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("%w: %q", ErrSchemeBlocked, u.Scheme)
	}

	if _, hasPassword := u.User.Password(); hasPassword {
		return nil, ErrEmbeddedCreds
	}

	host := u.Hostname()
	if host == "" {
		return nil, ErrNoHost
	}

	// IP literal case
	if ip := net.ParseIP(host); ip != nil {
		if err := checkIP(ip, allowLoopback); err != nil {
			return nil, err
		}
		return u, nil
	}

	// Domain case: resolve via stdlib
	addrs, err := net.LookupHost(host)
	if err != nil {
		return nil, fmt.Errorf("%w: lookup %s: %v", ErrNoHost, host, err)
	}
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil {
			continue
		}
		if err := checkIP(ip, allowLoopback); err != nil {
			return nil, err
		}
	}
	return u, nil
}

func checkIP(ip net.IP, allowLoopback bool) error {
	// Convert v4-in-v6 (::ffff:a.b.c.d) to 4-byte form so v4 CIDRs match
	// correctly. Without this, ::ffff:0:0/96 in the blocklist would match
	// every IPv4 address — including perfectly safe ones.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if allowLoopback && ip.IsLoopback() {
		return nil
	}
	for _, cidr := range allBlocked() {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if n.Contains(ip) {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				return fmt.Errorf("%w: %s", ErrPrivateLiteral, ip)
			}
			return fmt.Errorf("%w: %s", ErrBlockedIP, ip)
		}
	}
	return nil
}

func allBlocked() []string {
	out := make([]string, 0, len(blockedV4)+len(blockedV6))
	out = append(out, blockedV4...)
	out = append(out, blockedV6...)
	return out
}

// WrapUntrusted wraps scraped content with explicit trust-boundary markers.
// The wrapping makes it obvious to the LLM (and to any post-processing) that
// the content is data, not instructions.
func WrapUntrusted(source string, fetchedAt time.Time, content string) string {
	return fmt.Sprintf(
		"<fetched_content source=%q fetched_at=%q trust=\"untrusted\">\n%s\n</fetched_content>",
		source,
		fetchedAt.UTC().Format(time.RFC3339),
		content,
	)
}