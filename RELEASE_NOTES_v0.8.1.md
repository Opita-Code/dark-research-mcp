# dark-research-mcp v0.8.1

**Release date:** 2026-07-27
**Tag:** [v0.8.1](https://github.com/Opita-Code/dark-research-mcp/releases/tag/v0.8.1) (pending)
**Branch:** `main` (push target: `feat/vault-autoload`)
**Supersedes:** v0.8.0 (which shipped cx.v3 conformance + multi-backend merge + dedup)

> **Closes the v0.8.0 deferrals.**
>
> v0.8.1 ships the two persistence/intelligence features that were
> scoped for v0.8.x in the v0.8.0 release notes: persistence-aware
> recall (within-TTL caching of recent runs) and optional LLM
> synthesis (one-paragraph analyst summary for corroborating
> multi-backend findings). Pure additions; v0.8.0 wire contract
> unchanged.

---

## What's in this release

### Persistence-aware recall (`internal/research/cache.go`)

The router consults the persistence layer (`research_runs` +
`research_items`) BEFORE fanning out to backends. If the most recent
run for `(query, intent)` is within the intent's TTL window, the
cached items are returned and `BackendUsed` is stamped `"cache"`. No
HTTP. No rate-limit consumption on the backend.

Per-intent TTL table (matches how fast each domain moves):

| Intent | TTL | Why |
|---|---:|---|
| `cve` | 6h | New CVEs ship daily; cache must refresh overnight |
| `web` | 1h | Generic web results turn stale fast |
| `news` | 15min | News by definition moves per hour |
| `threat` | 30min | IOC lists update frequently; rate-limit ourselves |
| everything else | 24h | Academic / code / domain / DNS / cert / ip / email / dark / geo all move on weekly-or-slower cadences |

Activation:
```go
router := research.NewRouter(reg, nil)
router.SetMemStore(memStore)  // *mem.Store satisfies research.PersistenceReader
router.SetEnableCache(true)
```

Wired into `dark_research` router tool via `internal/tools/dark_research.go`
when `EnableCache=true`. Cache misses fall through to the normal
multi-backend merge path (no special error handling needed).

### Optional LLM synthesis (`internal/research/synthesize.go`)

When enabled (`EnableSynthesize=true`) AND an `LLMClient` is wired,
the router calls the LLM after the multi-backend merge with a
one-paragraph "OSINT analyst" system prompt. Result lands in
`Result.Summary`.

Thresholds (avoid wasted LLM calls on trivial results):
- ≥2 items (need corroboration)
- ≥2 distinct `Source` values (need cross-source confirmation)

Failure modes (all degrade gracefully, no error to caller):
- `LLMClient` is nil → no synthesis, no error
- `SDD_LLM_API_KEY` not set → no synthesis, no error
- LLM call fails (rate-limit, network, refusal) → `Summary` stays empty, no error
- Below corroboration threshold → no synthesis, no error

Activation:
```go
router.SetLLMClient(llmClient)   // *llm.Client satisfies research.LLMClient
router.SetEnableSynthesize(true)
```

System prompt: "You are an OSINT analyst. ... produce a one-paragraph
(3-5 sentences) summary. Cite the source of each claim inline,
e.g. (osv.dev) or (news: gdelt). Do not invent facts not present in
the findings. Output ONLY the paragraph, no preamble, no bullet points."

### Schema migration v4: `research_items.dedup_key`

`ALTER TABLE research_items ADD COLUMN dedup_key TEXT;`
`CREATE INDEX idx_research_items_dedup_key ON research_items(dedup_key);`

Populated by the router on every save. Indexed for fast `WHERE
dedup_key IN (...)` lookups (not used by v0.8.1 — the cache lookup
is by `(query, intent)` — but the column is here for v0.9.0's
planned dedup-key cross-run cache).

> **Migration note**: an earlier experiment (2026-07-13) recorded
> a v4 migration in `schema_migrations` whose SQL did NOT include
> `dedup_key`. On this release's smoke test the column was missing
> even though v4 was marked applied. The fix was a one-time manual
> `ALTER TABLE research_items ADD COLUMN dedup_key TEXT;` on the
> shared dark.db. Fresh installs go through the migration v4 cleanly.

### Other changes

- `internal/server/server.go`: default `version` var bumped 0.8.0 → 0.8.1
- `internal/research/backends.go`: `Result.Summary` field added (omitempty)
- `internal/research/router.go`: new fields `MemStore` (`PersistenceReader`),
  `LLMClient` (`LLMClient`), `EnableCache`, `EnableSynthesize`; setters
  `SetMemStore`, `SetEnableCache`, `SetLLMClient`, `SetEnableSynthesize`
- `internal/research/router.go`: cache lookup prepended before
  backend fan-out; synthesis call appended before persistence
- `internal/mem/types.go`: `Item.DedupKey` field added (omitempty)
- `internal/mem/recall.go`: INSERT into research_items now includes dedup_key
- `internal/mem/migrate.go`: migration v4 (research_items_dedup_key)

### Tests added (12 new)

| File | Tests |
|---|---|
| `internal/research/cache_test.go` (NEW) | `TestLookupCached_MissWhenNoPriorRun`, `TestLookupCached_HitWhenWithinTTL`, `TestLookupCached_MissWhenTTLExpired`, `TestLookupCached_NilStore`, `TestLookupCached_DifferentIntentSkipsHit`, `TestTTLFor` (6 tests) |
| `internal/research/synthesize_test.go` (NEW) | `TestSynthesize_SkipsWhenNoLLM`, `TestSynthesize_SkipsWhenTooFewItems`, `TestSynthesize_SkipsWhenOnlyOneBackend`, `TestSynthesize_CallsWhenEnabled`, `TestSynthesize_LLMErrorPropagates`, `TestDistinctSources` (6 tests) |

All 18 tests pass. Pre-existing fixture failures (TestRouter_ahmia,
TestRouter_gdelt — missing VCR fixtures on this checkout) are
unrelated to this release.

### Live smoke (post-build)

Initialize response carries:
```
coexistence_group=dark-agents/research policy_gateway=false
spec 164 bridge.2 cx.v3 ... Version=0.8.1
```

Migration v4 verified manually on shared dark.db.

---

## Upgrade guide

For operators currently on v0.8.0:

```bash
git pull
go build -ldflags "-X main.version=0.8.1" -o dark-research-mcp.exe ./cmd/dark-research-mcp
# Replace binary + restart opencode (Windows inode lock)
```

For operators on v0.7.x: same as v0.8.0 (already covered in v0.8.0 release notes).

### Activation in dark_research router tool

Cache + synthesis are OFF by default (opt-in). Wire them where you
build the router:

```go
router := research.NewRouter(reg, nil)
router.SetMemStore(store)             // enable persistence-aware recall
router.SetEnableCache(true)
router.SetLLMClient(llm.NewFromEnv())  // enable optional synthesis
router.SetEnableSynthesize(true)
```

If you don't wire these, behavior is identical to v0.8.0.

---

## What this release is NOT

- **Not** a removal of the 38 dark_mem_* deprecation shims.
- **Not** a new intent (still 13 OSINT intents).
- **Not** a federated cache (cross-process; v0.9.0 territory).
- **Not** a write-through LLM (synthesis is one-shot, not persisted).

---

## License

MIT (Opita Code). See [`LICENSE`](LICENSE).
