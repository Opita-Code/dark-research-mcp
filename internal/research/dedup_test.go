// dedup_test.go — tests for the v0.8.0 multi-backend dedup helpers.
package research

import (
	"strings"
	"testing"
	"time"
)

func TestCanonicalizeURL_Lowercase(t *testing.T) {
	got := CanonicalizeURL("HTTPS://Example.COM/Path")
	if got != "https://example.com/Path" {
		t.Errorf("CanonicalizeURL lowercased scheme/host incorrectly\n  got: %s", got)
	}
}

func TestCanonicalizeURL_StripsDefaultPort(t *testing.T) {
	got := CanonicalizeURL("https://example.com:443/foo")
	if strings.Contains(got, ":443") {
		t.Errorf("CanonicalizeURL didn't strip default https port\n  got: %s", got)
	}
}

func TestCanonicalizeURL_StripsUTM(t *testing.T) {
	got := CanonicalizeURL("https://example.com/foo?utm_source=x&id=42")
	if strings.Contains(got, "utm_source") {
		t.Errorf("CanonicalizeURL didn't strip utm_source\n  got: %s", got)
	}
	if !strings.Contains(got, "id=42") {
		t.Errorf("CanonicalizeURL stripped non-tracking param\n  got: %s", got)
	}
}

func TestCanonicalizeURL_StripsFragments(t *testing.T) {
	got := CanonicalizeURL("https://example.com/foo#bar")
	if strings.Contains(got, "#") {
		t.Errorf("CanonicalizeURL didn't strip fragment\n  got: %s", got)
	}
}

func TestCanonicalizeURL_SortsQueryParams(t *testing.T) {
	got1 := CanonicalizeURL("https://example.com/foo?b=2&a=1")
	got2 := CanonicalizeURL("https://example.com/foo?a=1&b=2")
	if got1 != got2 {
		t.Errorf("CanonicalizeURL didn't sort query params for stable hashing\n  got1: %s\n  got2: %s", got1, got2)
	}
}

func TestCanonicalizeURL_TrailingSlash(t *testing.T) {
	got := CanonicalizeURL("https://example.com/foo/")
	if strings.HasSuffix(got, "/foo/") {
		t.Errorf("CanonicalizeURL didn't strip trailing slash\n  got: %s", got)
	}
}

func TestDedupKey_StableForEquivalentURLs(t *testing.T) {
	k1 := DedupKey("https://example.com/foo?utm_source=x&id=42")
	k2 := DedupKey("https://example.com/foo?id=42")
	if k1 != k2 {
		t.Errorf("DedupKey not stable for equivalent URLs\n  k1: %s\n  k2: %s", k1, k2)
	}
}

func TestDedupKey_DifferentForDistinctURLs(t *testing.T) {
	k1 := DedupKey("https://example.com/foo")
	k2 := DedupKey("https://example.com/bar")
	if k1 == k2 {
		t.Errorf("DedupKey collided for distinct URLs\n  k1: %s\n  k2: %s", k1, k2)
	}
}

func TestDedupItems_MergesCorroboratingItems(t *testing.T) {
	now := time.Now().UTC()
	bucket1 := []Item{
		{Title: "Title A", URL: "https://example.com/a", Source: "osv", Confidence: 0.95, FetchedAt: now, DedupKey: DedupKey("https://example.com/a")},
	}
	bucket2 := []Item{
		{Title: "Title A (nvd copy)", URL: "https://example.com/a", Source: "nvd", Confidence: 0.97, FetchedAt: now, DedupKey: DedupKey("https://example.com/a")},
		{Title: "Title B", URL: "https://example.com/b", Source: "nvd", Confidence: 0.90, FetchedAt: now, DedupKey: DedupKey("https://example.com/b")},
	}

	merged := DedupItems(bucket1, bucket2)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged items, got %d", len(merged))
	}
	// The merged item for "a" should be ranked higher than "b" (higher confidence).
	if merged[0].URL != "https://example.com/a" {
		t.Errorf("expected 'a' first (higher confidence), got %s", merged[0].URL)
	}
	if merged[0].Source != "osv+nvd" {
		t.Errorf("expected corroboration tag 'osv+nvd', got %q", merged[0].Source)
	}
	if merged[0].Confidence != 0.97 {
		t.Errorf("expected max confidence 0.97 (nvd wins over osv), got %f", merged[0].Confidence)
	}
}

func TestDedupItems_PreservesEmptyURLItems(t *testing.T) {
	bucket1 := []Item{
		{Title: "Text-only finding", URL: "", Source: "ahmia", Confidence: 0.7},
		{Title: "Text-only finding", URL: "", Source: "ahmia", Confidence: 0.7},
	}
	merged := DedupItems(bucket1)
	if len(merged) != 1 {
		t.Errorf("expected text-only items to dedup, got %d", len(merged))
	}
}

func TestDedupItems_SortedByConfidence(t *testing.T) {
	bucket := []Item{
		{Title: "Low", URL: "https://a.com", Confidence: 0.5},
		{Title: "High", URL: "https://b.com", Confidence: 0.95},
		{Title: "Mid", URL: "https://c.com", Confidence: 0.7},
	}
	merged := DedupItems(bucket)
	if merged[0].URL != "https://b.com" {
		t.Errorf("expected highest confidence first, got %s", merged[0].URL)
	}
	if merged[1].URL != "https://c.com" {
		t.Errorf("expected mid confidence second, got %s", merged[1].URL)
	}
	if merged[2].URL != "https://a.com" {
		t.Errorf("expected lowest confidence third, got %s", merged[2].URL)
	}
}
