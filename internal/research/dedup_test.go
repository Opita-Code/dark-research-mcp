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

// TestDedupItems_SingleBucketKeepsBackendOrder documents that a
// single bucket keeps the order the backend returned. The router
// fills a bucket in backend order, and the confidence ranking is
// already expressed by the backend's own ordering.
//
// (Before v0.9.1 this test asserted a confidence re-sort. The
// re-sort was the bug: it starved every lower-confidence backend
// once the merged list was truncated to MaxItems. See
// TestDedupItems_InterleavesBackends.)
func TestDedupItems_SingleBucketKeepsBackendOrder(t *testing.T) {
	bucket := []Item{
		{Title: "Low", URL: "https://a.com", Confidence: 0.5},
		{Title: "High", URL: "https://b.com", Confidence: 0.95},
		{Title: "Mid", URL: "https://c.com", Confidence: 0.7},
	}
	merged := DedupItems(bucket)
	if len(merged) != 3 {
		t.Fatalf("want 3 items, got %d", len(merged))
	}
	if merged[0].URL != "https://a.com" {
		t.Errorf("expected first-seen order preserved, got %s", merged[0].URL)
	}
	if merged[2].URL != "https://c.com" {
		t.Errorf("expected first-seen order preserved, got %s", merged[2].URL)
	}
}

// TestDedupItems_InterleavesBackends is the regression guard for the
// starvation bug: with two backends of different confidence, a
// confidence sort followed by a MaxItems truncation filled every slot
// from the highest-confidence backend and made the other backend's
// successful results invisible. The merged list must interleave.
func TestDedupItems_InterleavesBackends(t *testing.T) {
	high := []Item{
		{Title: "high-1", URL: "https://high1.com", Confidence: 0.95},
		{Title: "high-2", URL: "https://high2.com", Confidence: 0.95},
	}
	low := []Item{
		{Title: "low-1", URL: "https://low1.com", Confidence: 0.92},
		{Title: "low-2", URL: "https://low2.com", Confidence: 0.92},
	}
	merged := DedupItems(high, low)
	if len(merged) != 4 {
		t.Fatalf("want 4 items, got %d", len(merged))
	}
	// Round-robin: one from each bucket in turn.
	if merged[0].URL != "https://high1.com" {
		t.Errorf("merged[0] = %s, want high1", merged[0].URL)
	}
	if merged[1].URL != "https://low1.com" {
		t.Errorf("merged[1] = %s, want low1 (lower-confidence backend must not be starved)", merged[1].URL)
	}
	if merged[2].URL != "https://high2.com" {
		t.Errorf("merged[2] = %s, want high2", merged[2].URL)
	}
	if merged[3].URL != "https://low2.com" {
		t.Errorf("merged[3] = %s, want low2", merged[3].URL)
	}
}

// TestDedupItems_InterleaveHandlesUnevenBuckets covers the case
// where one backend returns fewer items than another.
func TestDedupItems_InterleaveHandlesUnevenBuckets(t *testing.T) {
	a := []Item{
		{Title: "a1", URL: "https://a1.com", Confidence: 0.95},
		{Title: "a2", URL: "https://a2.com", Confidence: 0.95},
		{Title: "a3", URL: "https://a3.com", Confidence: 0.95},
	}
	b := []Item{
		{Title: "b1", URL: "https://b1.com", Confidence: 0.9},
	}
	merged := DedupItems(a, b)
	if len(merged) != 4 {
		t.Fatalf("want 4 items, got %d", len(merged))
	}
	if merged[0].URL != "https://a1.com" || merged[1].URL != "https://b1.com" {
		t.Errorf("expected interleaved start, got %s then %s", merged[0].URL, merged[1].URL)
	}
	// Once bucket b is exhausted the rest of bucket a follows.
	if merged[2].URL != "https://a2.com" || merged[3].URL != "https://a3.com" {
		t.Errorf("expected remaining items from bucket a, got %s then %s", merged[2].URL, merged[3].URL)
	}
}
