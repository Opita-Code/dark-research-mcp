# dark-research-mcp v0.4.0-rc.5

**Bug hunt fixes for v0.4.0-rc.4.** 8 issues found by auditing the code with fresh eyes. None of them touch the contract — light mode is still byte-equivalent to pre-Fase-1 — but one of them (BUG #1) was a real path-traversal hole, so this rc is worth tagging before going to v0.4.0.

## What changed

### BUG #1 (HIGH, security): `validateModPath` accepted `foo/../bar` and `foo/bar/..`

The mod loader's path-traversal check used a string-prefix test on the cleaned path. `filepath.Clean("foo/../bar")` returns `"bar"`, which passes the string check. A mod could declare `[knowledge] prompt_injections = ["../../../etc/passwd"]`, the path would clean to a benign-looking name, and the validation would let it through. The `readModFile` defense-in-depth check in some cases caught it (via `HasPrefix(absFull, absRoot)`), but the check was incomplete on Windows because of the leading-separator convention.

**Fix:** split on the OS separator and check for `..` as a complete segment, before any `filepath.Clean`. Add a separate pass for forward slashes on Windows so `"foo/../bar"` is also caught regardless of how the path was written in the manifest.

**Files:** `internal/mods/loader.go`
**Tests:** `internal/mods/bug_hunt_validate_test.go` (3 new tests).

### BUG #2 (LOW, dead code): orphan `cacheKey` in `CompleteJSONWithRetry`

The retry chain used to compute `cacheKey := fmt.Sprintf("%d", attempt)` and immediately discard it with `_ = cacheKey`. Leftover from an earlier design that tried to inject the attempt index into the cache key. The system prompt already varies per attempt (it has the retry directive appended), so the existing FNV-1a key over `(model, system, user)` naturally produces distinct keys per attempt. Removed the dead code and added a comment explaining the design.

**Files:** `internal/llm/refusal_interceptor.go`

### BUG #3 (LOW, duplication): `stripCodeFences` in two places

`internal/llm/client.go` and `internal/tools/ssd.go` each had their own copy of the JSON-fence-stripping function. Drift risk if one changed and the other didn't. Exported `llm.StripCodeFencesForTools` and routed the tools package through it. The old tools-local function is now a one-line wrapper.

**Files:** `internal/llm/client.go`, `internal/tools/ssd.go`

### BUG #4 (MEDIUM, panic): `persistRefusal` nil-deref

If `r` is nil, the function panicked on `r.Attempts` and `r.FinalRefusal.Excerpt`. Now both reads are guarded with `r == nil` checks; the function uses local variables for the audit values so a nil result produces a clean error message rather than a panic.

**Files:** `internal/tools/ssd.go`

### BUG #5 (HIGH, audit gap): `judgeConsensus` did not persist refusal metadata

Every `dark_ssd_*` single-shot tool persisted `refused_attempts` and `refusal_pattern` to `sdd_evaluations` on every call. The consensus tool (which runs N samples) discarded the `*RefusalResult` and never wrote a row. So a user running `dark_ssd_consensus` on a content that the LLM refused for sample 2 of 3 would see a refusal-exhausted error but no audit row. Now consensus routes through `persistRefusal` on `ErrRefusalExhausted`, with `target_id` formatted as `"<sample>/<N>"` (e.g. `"2/3"`) so the audit trail tells you which sample failed.

**Files:** `internal/tools/ssd.go`

### BUG #6 (MEDIUM, audit incorrectness): refusal label used raw weight, not net

`DetectRefusal` decides which pattern label to record by comparing the new pattern's net weight against the OLD pattern's raw weight. If a high-weight pattern was fully suppressed (net 0) and a lower-weight pattern had positive net, the audit log incorrectly attributed the refusal to the suppressed pattern. The fix: track the net weight alongside the label as the comparison key. The label now records the pattern that actually contributed to the refusal signal.

**Files:** `internal/llm/refusal_detect.go`
**Tests:** `internal/llm/bug_hunt_detect_test.go` (2 new tests covering the contract).

### BUG #7 (LOW, false positive): `validateModPath` rejected legit names starting with `..`

`"..hidden/file.md"` and `"..bar.md"` are legitimate paths (a hidden directory, a file literally named `..bar.md`). The string-prefix check rejected them. The new segment-based check is precise: only segments equal to `..` are rejected.

**Files:** `internal/mods/loader.go` (same fix as BUG #1; both issues were in the same function and were fixed together).
**Tests:** `internal/mods/bug_hunt_validate_test.go` (covered by `TestValidateModPath_LegitimateDotDotNames`).

### BUG #8 (LOW, debt): duplicate `func init()` in `loader.go`

`internal/constitution/loader.go` had two `func init()` blocks, both parsing the light constitution. The first was the live one, the second was a stale copy from a previous revision. Go allows multiple `init()` in one file (they run in order), so this was functionally a no-op — but it was confusing and indicated the comment had been updated without the duplicate removed. The duplicate is gone; a comment records why it was there.

**Files:** `internal/constitution/loader.go`

## Test coverage additions

8 new tests, all green in both build modes (stock and `-tags allow_builtin_dark`):

| File | Tests | What it covers |
|---|---:|---|
| `internal/mods/bug_hunt_validate_test.go` | 3 | True positives (`../etc/passwd`, `foo/../bar`, `foo/bar/..`), legitimate names (`..hidden`, `..bar.md`), Windows drive letters |
| `internal/llm/bug_hunt_detect_test.go` | 2 | Label tracking uses net weight, contract for `Pattern` when not a refusal |
| `internal/llm/bug_hunt_concurrent_test.go` | 2 | 16 goroutines on cache (RWMutex correctness), 8 goroutines on retry chain with per-goroutine httptest servers |

The concurrent test is a workaround for the CGO-free build: `-race` requires CGO, so the test exercises the synchronization via load rather than via the race detector. The cache's `RWMutex` and the retry chain's per-call state are both safe — the test fails (panic on concurrent map write) if either is wrong.

## Security boundary (still intact)

```
$ dark-research-mcp -constitution dark
exit=2
constitution error: dark.toml is not embedded in this binary (rebuild with -tags allow_builtin_dark, or supply a user file at user/dark)

$ dark-research-mcp -tags allow_builtin_dark -constitution dark
constitution=dark-research/aggressive@1.0.0 source=builtin:dark
```

The stock public binary still contains zero bytes of dark content. The tagged build still loads `dark-research/aggressive@1.0.0` as the active constitution. The path-traversal fix in BUG #1 only changes how user-supplied mod paths are validated; it does not affect the constitution boundary.

## Test counts (240+ total, +8 vs v0.4.0-rc.4)

| Package | Tests | Note |
|---|---:|---|
| `internal/llm` | 33 | +2: bug_hunt_detect |
| `internal/mem` | 44 | unchanged |
| `internal/mods` | 28 | +3: bug_hunt_validate |
| `internal/tools` | 21 | unchanged |
| All other packages | 114 | unchanged |

## Assets

(SHA-256s in `SHA256SUMS.txt`; binaries are stock builds, dark is opt-in via build tag.)

| File | Size |
|---|---:|
| `dark-research-mcp-windows-amd64.exe` | 12 MB |
| `dark-research-mcp-linux-amd64` | 12 MB |
| `dark-research-mcp-linux-arm64` | 11 MB |
| `dark-research-mcp-darwin-amd64` | 12 MB |
| `dark-research-mcp-darwin-arm64` | 11 MB |

## Install / upgrade

```sh
gh release download v0.4.0-rc.5 --repo Opita-Code/dark-research-mcp
sha256sum -c SHA256SUMS.txt --ignore-missing
./dark-research-mcp-linux-amd64 -version
# dark-research-mcp 0.4.0-rc.5
```

## What's next

If no regressions are reported against v0.4.0-rc.5, the next tag is `v0.4.0` (no `-rc`). The 0.4 line is feature-complete: Fase 0 (schema), Fase 1 (constitution), Fase 2 (mods), Fase 3 (refusal interceptor), and the bug-hunt fixes from this rc.

After v0.4.0:
- **v0.5.0** — Go-plugin mods (Fase 6). The manifest's `capabilities.tools/parsers/backends` fields are reserved for this.
- **v0.6.0** — web-of-mods registry (Fase 7).
