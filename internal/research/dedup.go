// Package research — dedup.go (v0.8.0): canonical URL hashing and
// cross-backend item deduplication.
//
// The pre-v0.8.0 router returned whichever items the FIRST successful
// backend produced. That worked when each backend returned disjoint
// results, but in practice the same URL appears under slightly
// different shapes across backends (http vs https, www. prefix, query
// strings, fragments, trailing slash, mixed case host, tracking
// params). v0.8.0 introduces multi-backend merge; that merge is only
// useful if the dedup key normalizes these differences.
//
// Reference: BRIDGE_AND_COEXISTENCE.md v2.0.0 §3.3 (gateway composes
// backings — backings should be deduplication-aware).
package research

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
)

// CanonicalizeURL returns a normalized URL suitable for dedup keys.
//
// Transformations applied (in order):
//
//  1. Parse with net/url; if parse fails, return the original
//     (defensive — pre-v0.8.0 callers may pass unparseable garbage).
//  2. Lowercase the scheme and host.
//  3. Strip the default port for known schemes (http:80, https:443).
//  4. Drop tracking query params (utm_*, fbclid, gclid, ref, mc_*).
//  5. Sort remaining query params alphabetically for stable hashing.
//  6. Drop the fragment (browsing-only; doesn't identify content).
//  7. Drop trailing slash on the path (unless the path is just "/").
//
// Two URLs that differ only in these transformations are treated as the
// same dedup target. URLs that differ in path, registered domain, or
// non-tracking query params are distinct.
func CanonicalizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	// Drop default ports.
	if (u.Scheme == "http" && strings.HasSuffix(u.Host, ":80")) ||
		(u.Scheme == "https" && strings.HasSuffix(u.Host, ":443")) {
		u.Host = strings.TrimSuffix(strings.TrimSuffix(u.Host, ":80"), ":443")
	}

	// Drop tracking query params.
	q := u.Query()
	trackingPrefixes := []string{"utm_", "fbclid", "gclid", "ref", "mc_"}
	dropped := false
	for k := range q {
		for _, p := range trackingPrefixes {
			if k == p || strings.HasPrefix(k, p) {
				q.Del(k)
				dropped = true
				break
			}
		}
	}
	if dropped {
		u.RawQuery = q.Encode()
	}

	// Drop fragment.
	u.Fragment = ""

	// Drop trailing slash on path (except root).
	if len(u.Path) > 1 && strings.HasSuffix(u.Path, "/") {
		u.Path = strings.TrimRight(u.Path, "/")
	}

	// Sort query params alphabetically for stable hashing.
	if u.RawQuery != "" {
		parts := strings.Split(u.RawQuery, "&")
		sort.Strings(parts)
		u.RawQuery = strings.Join(parts, "&")
	}

	return u.String()
}

// DedupKey returns a stable hex-encoded SHA-256 hash of the canonical
// URL. Items with the same DedupKey are treated as duplicates. Used in
// Item.DedupKey (persisted in research_items for cross-run dedup).
func DedupKey(rawURL string) string {
	canonical := CanonicalizeURL(rawURL)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// DedupItems merges a list of item lists into a single list,
// deduplicating by DedupKey. Confidence is preserved as max() across
// corroborating sources. The first occurrence of a key wins on Title
// and Snippet (typically the highest-confidence backend). Empty URL
// items are kept as-is (they represent text-only findings with no
// linkable artifact).
//
// Order: stable across calls (sorted by descending confidence, then
// by first-seen index for ties). This makes test assertions reliable.
func DedupItems(buckets ...[]Item) []Item {
	type seen struct {
		merged Item
		idx    int // first-seen order
	}
	index := map[string]*seen{}
	order := []string{} // tracks insertion order

	for _, bucket := range buckets {
		for _, it := range bucket {
			key := it.DedupKey
			if key == "" {
				// No URL — keep as-is under a synthetic key so we
				// don't collapse distinct text-only findings.
				key = "txt:" + it.Title + "|" + it.Source
			}
			if existing, ok := index[key]; ok {
				// Corroboration: boost confidence slightly when a
				// second backend confirms the same URL.
				if it.Confidence > existing.merged.Confidence {
					existing.merged.Confidence = it.Confidence
				}
				// Append source tag so consumers see corroboration.
				if !strings.Contains(existing.merged.Source, it.Source) && it.Source != "" {
					existing.merged.Source = existing.merged.Source + "+" + it.Source
				}
				continue
			}
			index[key] = &seen{merged: it, idx: len(order)}
			order = append(order, key)
		}
	}

	// Materialize + sort by confidence desc, then first-seen index asc.
	out := make([]Item, 0, len(order))
	for _, k := range order {
		out = append(out, index[k].merged)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		// Tie-break by first-seen — preserve order from index map.
		ki := index[dedupKeyOf(out[i])].idx
		kj := index[dedupKeyOf(out[j])].idx
		return ki < kj
	})
	return out
}

// dedupKeyOf returns the dedup key an Item would have, used for the
// stable tie-break in DedupItems. Mirrors the key derivation in
// DedupItems exactly (txt: prefix for empty URLs).
func dedupKeyOf(it Item) string {
	if it.DedupKey != "" {
		return it.DedupKey
	}
	return "txt:" + it.Title + "|" + it.Source
}
