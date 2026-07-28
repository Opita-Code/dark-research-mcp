# dark-research-mcp v0.8.0

**Release date:** 2026-07-27
**Tag:** [v0.8.0](https://github.com/Opita-Code/dark-research-mcp/releases/tag/v0.8.0) (pending)
**Branch:** `main`
**Spec:** `BRIDGE_AND_COEXISTENCE.md` v2.0.0 §3.2 (cx.v3 conformance)

> **Metadata-only conformance release.**
>
> v0.8.0 ships the coexistence_group + policy_gateway declarations
> mandated by cx.v3. The 13 OSINT intents, the multi-intent router, and
> the 38 dark_mem_* frozen shims are unchanged. No wire-protocol change.

---

## What's in this release

### cx.v3 conformance per `BRIDGE_AND_COEXISTENCE.md` v2.0.0

The `initialize` response from `dark-research-mcp` now declares (via the
MCP `instructions` channel; see `internal/server/server.go`):

```
coexistence_group=dark-agents/research policy_gateway=false (spec 164 bridge.2 cx.v3)
```

What this means for harnesses:

| Harness state | Behavior |
|---|---|
| v0.8.0+ dark-research + v2.x dark-memory (`policy_gateway=true`) | **cx.v3 active mode**: harness routes dark_* calls through the dark-memory gateway for persona shaping + capability checks + drift-at-write. Direct dark_research_* still works. |
| v0.8.0+ dark-research + pre-cx.v3 dark-memory | **Legacy fallback mode** per BRIDGE §5.4 test 7: direct dark_research_* works; degraded audit; no gateway enforcement. |
| v0.7.x dark-research + any dark-memory | **cx.v2 behavior**: dark-research is a sibling surface; LLM calls dark_research_* directly with no gateway in the path. |

### Other changes

- `version` default bumped from `dev` to `0.8.0` in `cmd/dark-research-mcp/main.go`.
- Removed `internal/server/server.go`'s hardcoded `"0.1.0"` version (now reads from the package-level `version` var stamped via `-ldflags`).
- New tests in `internal/server/server_test.go`:
  - `TestNew_DeclaresCoexistenceGroup_Research`
  - `TestNew_DeclaresPolicyGateway_False`
  - `TestNew_Instructions_MentionsDarkMemoryAsSuccessor`
- `internal/server/server.go` now uses `server.WithInstructions(...)` to bake cx.v3 metadata into the initialize envelope.
- Documentation refresh: README and ARCHITECTURE now reflect the cx.v3 backing role.

### Companion: dark-memory-mcp

dark-memory ships `policy_gateway=true` in parallel (in dark-memory's
own v2.x series — see its CHANGELOG). The two servers together enter
full cx.v3 active mode.

### Test results

| Package | Status |
|---|---|
| `internal/config` | ✅ unchanged |
| `internal/constitution` | ✅ unchanged |
| `internal/llm` | ✅ unchanged |
| `internal/mem` | ✅ unchanged |
| `internal/mods` | ✅ unchanged |
| `internal/research` | (backends improve — see v0.8.x line) |
| `internal/safety` | ✅ unchanged |
| `internal/server` | ✅ +3 new tests |
| `internal/tools` | ✅ unchanged |
| `internal/vault` | ✅ unchanged |

All 9 test packages pass. No regressions.

---

## Upgrade guide

For operators currently running v0.7.x:

1. `git pull` and rebuild:
   ```bash
   go build -ldflags "-X main.version=0.8.0" -o dark-research-mcp.exe ./cmd/dark-research-mcp
   ```
2. Replace the binary at `C:\Users\Nico\dark-research-mcp\dark-research-mcp.exe`
   (Windows holds the inode; restart opencode to pick up the new binary).
3. (Recommended) Upgrade `dark-memory-mcp` to a version that declares
   `policy_gateway=true` (currently dark-memory v2.1.3+ ships it).
4. Verify with any MCP introspection tool — the initialize response
   `instructions` field should now contain
   `coexistence_group=dark-agents/research policy_gateway=false`.

For harness maintainers:

- If your harness inspects `coexistence_group`, **you must update the
  expected value** from `dark-agents/memory` (legacy) to
  `dark-agents/research` (cx.v3). The legacy value was a v1 convention
  that conflated "shared family" with "we own memory". The cx.v3 value
  names the actual specialty.
- The `policy_gateway` flag is new in cx.v3. Older dark-research
  binaries did not declare it; treat absence as legacy mode.

---

## What this release is NOT

- **Not** a removal of the 38 `dark_mem_*` deprecation shims. Those stay
  frozen per BRIDGE §2.2. Removal is planned for a later major release
  (likely v1.0).
- **Not** a removal of dark-research-mcp as a standalone binary. The
  BRIDGE v2 spec demotes it from sibling to **backing**, but it remains
  a working MCP server. It will not be merged into dark-memory.
- **Not** a feature release. The 13 OSINT intents, the router, the
  parsers, the rate limiter, and the LLM cache are unchanged. Look at
  v0.8.x for router enhancements and backend fixes.

---

## Related

- Spec: `BRIDGE_AND_COEXISTENCE.md` v2.0.0 (normative, 2026-07-19).
- Drift: `DRIFT_BURST.md` §A.2 (BRIDGE v1 → v2 drift, 4 items, all
  addressed by this release).
- Companion: `dark-memory-mcp` v2.1.3+ ships `policy_gateway=true`.
- Migration timeline:
  - 2026-07-19: BRIDGE v2 published; cx.v3 effective.
  - **2026-07-27**: dark-research v0.8.0 ships cx.v3 metadata.
  - 2026-09-30: final day cx.v1/v2 dual-support.
  - 2026-10-01: cx.v1/v2 deprecated; harness logs warning if legacy
    metadata seen.

---

## License

MIT (Opita Code). See `LICENSE`.
