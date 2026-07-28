// Package research — cache.go (v0.8.1): persistence-aware recall.
//
// The router caches runs in research_runs and items in research_items.
// On a subsequent query, this file's LookupCached consults the
// persistence layer first and returns items from the most recent run
// for (query, intent) IF that run's created_at is within the intent's
// TTL window. Hot paths:
//
//   - Same session, same query, ≤TTL elapsed → no HTTP fetch, just
//     return the cached items. BackendUsed is stamped "cache" so
//     callers can tell.
//   - Different session, same query, ≤TTL elapsed → still cache hit
//     (cross-session dedup is allowed; the items are facts, not
//     session-scoped state).
//   - TTL elapsed → cache miss, router falls through to the normal
//     backend fan-out.
//
// Per-intent TTL table reflects how fast each domain moves:
//
//   - cve (security): 6h  — new CVEs ship daily; cache must refresh
//     overnight so today-old "fixed in v2.3.1" results don't mislead.
//   - web (search): 1h   — generic web results turn stale fast.
//   - news: 15min       — news by definition moves per hour.
//   - threat (URLhaus, OTX): 30min — IOC lists update frequently
//     but rate-limit ourselves so we don't hammer them.
//   - everything else: 24h — academic papers, code packages, domain
//     WHOIS, DNS records, IP geo, certificate transparency, geocoding
//     all move on weekly-or-slower cadences.
package research

import (
	"context"
	"fmt"
	"time"
)

// TTLByIntent is the per-intent cache window. Operators can override
// the table via Router.CacheTTL (map) at construction time; missing
// intents fall back to the default 24h.
var TTLByIntent = map[Intent]time.Duration{
	IntentCVE:    6 * time.Hour,
	IntentWeb:    1 * time.Hour,
	IntentNews:   15 * time.Minute,
	IntentThreat: 30 * time.Minute,
	// 24h defaults (omitted; lookup uses defaultTTL when not found):
	// IntentAcademic, IntentCode, IntentDomain, IntentDNS,
	// IntentCert, IntentIP, IntentEmail, IntentDark, IntentGeo
}

// defaultTTL is the fallback when an intent isn't in TTLByIntent.
const defaultTTL = 24 * time.Hour

// TTLFor returns the cache TTL for the given intent. Always returns
// a positive duration; missing intents get defaultTTL.
func TTLFor(intent Intent) time.Duration {
	if d, ok := TTLByIntent[intent]; ok {
		return d
	}
	return defaultTTL
}

// LookupCached returns items from the most recent research_runs row
// matching (query, intent) IF that row's created_at is within
// TTL(intent) of now. The cache consults the persistence layer via
// the PersistenceReader interface (mem.Store satisfies it).
//
// Returns:
//   - cached items (converted to research.Item) or nil if no cache hit
//   - true if cache hit, false otherwise
//   - error if the persistence layer fails (router falls through)
//
// If m is nil, returns (nil, false, nil) — caller falls through to
// the normal backend fan-out.
func LookupCached(ctx context.Context, m PersistenceReader, intent Intent, query string, maxItems int) ([]Item, bool, error) {
	if m == nil || query == "" {
		return nil, false, nil
	}
	run, err := m.LatestRunByQuery(ctx, query, string(intent))
	if err != nil {
		return nil, false, fmt.Errorf("cache: latest run: %w", err)
	}
	if run == nil {
		return nil, false, nil
	}
	created, err := time.Parse(time.RFC3339Nano, run.CreatedAt)
	if err != nil {
		// Malformed timestamp — treat as cache miss, not error.
		return nil, false, nil
	}
	if time.Since(created) > TTLFor(intent) {
		return nil, false, nil
	}
	cached, err := m.ListResearchItems(ctx, run.ID, "", maxItems)
	if err != nil {
		return nil, false, fmt.Errorf("cache: list items: %w", err)
	}
	items := make([]Item, len(cached))
	for i, c := range cached {
		items[i] = Item{
			Title:      c.Title,
			URL:        c.URL,
			Snippet:    c.Snippet,
			Source:     c.Source,
			Confidence: c.Confidence,
			Lang:       c.Lang,
			DedupKey:   c.DedupKey,
			FetchedAt:  created,
		}
		if c.FreshnessAt != "" {
			if f, err := time.Parse(time.RFC3339Nano, c.FreshnessAt); err == nil {
				items[i].FreshnessAt = f
			}
		}
	}
	return items, true, nil
}
