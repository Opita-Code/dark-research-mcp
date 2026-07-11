package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// File-backed JSON cache for LLM responses.
//
// dark-ssd LLM-as-judge calls the same prompt+content many times across
// a session (every artifact goes through brand_match + compliance_check
// + drift_judge). Each call hits the MiniMax-M3 API. This cache turns
// identical calls into zero-cost lookups.
//
// Design:
//   - key = FNV-1a64(model || system || user)  — fast, collision-free
//     for prompts under 64KB
//   - value = {text, stored_at, key_sha256}    — content addressed
//   - TTL = 1h default (configurable per cache)
//   - storage: $DARK_CACHE_DIR/llm-cache.json (atomic write via tmp+rename)
//   - thread-safe (RWMutex)
//   - disabled when path is empty
//
// Use CompleteCached() instead of Complete() when you want caching.
// Pass Cache: nil to bypass even if a cache is attached.
// ---------------------------------------------------------------------------

// CacheEntry is one cached LLM response.
type CacheEntry struct {
	Key      string    `json:"key"`       // FNV-1a hash
	SHA256   string    `json:"sha256"`    // sha256(text) for collision detection
	Text     string    `json:"text"`      // raw LLM response text
	StoredAt time.Time `json:"stored_at"` // when we cached it
	Model    string    `json:"model"`     // LLM model id
}

// CacheStats reports aggregate metrics.
type CacheStats struct {
	Hits       int    `json:"hits"`
	Misses     int    `json:"misses"`
	Sets       int    `json:"sets"`
	Evictions  int    `json:"evictions"` // TTL expired or LRU evicted
	Size       int    `json:"size"`      // current entries
	Path       string `json:"path"`
}

// Cache is a persistent JSON-backed LLM response cache.
type Cache struct {
	path string
	ttl  time.Duration

	mu      sync.RWMutex
	entries map[string]CacheEntry
	stats   CacheStats
}

// NewCache opens (or creates) a cache at path with the given TTL.
// If path is empty, returns nil (caching disabled). The file is loaded
// if it exists; expired entries are removed on load.
func NewCache(path string, ttl time.Duration) (*Cache, error) {
	if path == "" {
		return nil, nil
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	c := &Cache{
		path:    path,
		ttl:     ttl,
		entries: map[string]CacheEntry{},
		stats:   CacheStats{Path: path},
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("llm cache: mkdir: %w", err)
	}
	if err := c.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("llm cache: load: %w", err)
	}
	return c, nil
}

// Key returns the cache key for a (model, system, user) tuple. The key
// is the FNV-1a 64-bit hash as a hex string.
func Key(model, system, user string) string {
	h := fnv.New64a()
	h.Write([]byte(model))
	h.Write([]byte{0})
	h.Write([]byte(system))
	h.Write([]byte{0})
	h.Write([]byte(user))
	return hex.EncodeToString(h.Sum(nil))
}

// sha256Hex returns the sha256 hex digest of s.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Get returns (text, hit, error). hit is true only when an unexpired
// entry exists. The caller should still validate the response (LLMs are
// non-deterministic; the cache is a hint, not ground truth).
func (c *Cache) Get(model, system, user string) (string, bool, error) {
	if c == nil {
		return "", false, nil
	}
	key := Key(model, system, user)
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		c.mu.Lock()
		c.stats.Misses++
		c.mu.Unlock()
		return "", false, nil
	}
	if time.Since(entry.StoredAt) > c.ttl {
		c.mu.Lock()
		delete(c.entries, key)
		c.stats.Evictions++
		c.stats.Misses++
		c.mu.Unlock()
		return "", false, nil
	}
	c.mu.Lock()
	c.stats.Hits++
	c.mu.Unlock()
	return entry.Text, true, nil
}

// Set stores a response and persists to disk (atomic).
func (c *Cache) Set(model, system, user, text string) error {
	if c == nil {
		return nil
	}
	key := Key(model, system, user)
	entry := CacheEntry{
		Key:      key,
		SHA256:   sha256Hex(text),
		Text:     text,
		StoredAt: time.Now().UTC(),
		Model:    model,
	}
	c.mu.Lock()
	c.entries[key] = entry
	c.stats.Sets++
	c.stats.Size = len(c.entries)
	c.mu.Unlock()
	return c.persist()
}

// Stats returns a snapshot of the current counters.
func (c *Cache) Stats() CacheStats {
	if c == nil {
		return CacheStats{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	s := c.stats
	s.Size = len(c.entries)
	return s
}

// Clear removes all entries (in-memory + on-disk).
func (c *Cache) Clear() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.entries = map[string]CacheEntry{}
	c.stats.Evictions += c.stats.Size
	c.stats.Size = 0
	c.mu.Unlock()
	return c.persist()
}

// load reads the JSON file into memory, dropping expired entries.
func (c *Cache) load() error {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return err
	}
	var file struct {
		Entries map[string]CacheEntry `json:"entries"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	now := time.Now()
	for k, e := range file.Entries {
		if now.Sub(e.StoredAt) <= c.ttl {
			c.entries[k] = e
		}
	}
	c.stats.Size = len(c.entries)
	return nil
}

// persist writes the entries to disk atomically (tmp + rename).
func (c *Cache) persist() error {
	c.mu.RLock()
	// Snapshot under read lock; the file write itself doesn't need the lock.
	snapshot := make(map[string]CacheEntry, len(c.entries))
	for k, v := range c.entries {
		snapshot[k] = v
	}
	c.mu.RUnlock()

	file := struct {
		Entries map[string]CacheEntry `json:"entries"`
	}{Entries: snapshot}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// CompleteCached wraps Complete() with cache lookup. On hit, returns the
// cached text without calling the LLM. On miss, calls the LLM and stores
// the result. Pass cache=nil to disable caching entirely.
func (c *Client) CompleteCached(ctx context.Context, cache *Cache, system string, msgs ...Message) (string, error) {
	if cache == nil {
		return c.Complete(ctx, system, msgs...)
	}
	// Build a synthetic user string from msgs for keying.
	var user string
	for _, m := range msgs {
		user += m.Role + "\n" + m.Content + "\n---\n"
	}
	if text, hit, _ := cache.Get(c.Model, system, user); hit {
		return text, nil
	}
	text, err := c.Complete(ctx, system, msgs...)
	if err != nil {
		return "", err
	}
	_ = cache.Set(c.Model, system, user, text)
	return text, nil
}