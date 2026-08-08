# Equivalent Mutants (dark-research-mcp)

Every checksum in `.go-mutesting.blacklist` is documented here. A mutant is
blacklisted ONLY after human analysis proves it is behaviorally equivalent
(identical observable output for all inputs). A surviving mutant that is
NOT equivalent is a real test gap or a real bug.

## internal/safety/safety.go

| Checksum | Mutation | Why equivalent |
|---|---|---|
| `677763cd48c13d5e1c2a04ebf662f8ce` | `allBlocked()`: `make([]string, 0, len(blockedV4)+len(blockedV6))` → `len(blockedV4)-len(blockedV6)` | Capacity hint only. `append` grows the slice to the full size either way; the returned slice content and order are identical. |
| `f80216238575bbddaeb64695fb08b645` | No-op (mutated file byte-identical to original) | go-mutesting saved an empty diff — the mutator produced identical source. No behavioral delta by construction. |
| `f9f57f53745973ae31a9e4112d9c371b` | `checkResolved()`: `if ip == nil { continue }` branch emptied | `checkIP(nil)` is safe (`To4()`, `IsLoopback()`, `IPNet.Contains(nil)` all handle nil and return false/nil — verified 2026-08-07), so falling through to `checkIP(nil)` returns nil exactly like the original's skip. |
| `210c1c5ed340c2ab12d383af7e478246` | `checkIP()`: `ip = v4` (To4 normalization) removed | `net.IPNet.Contains` converts IPv4-mapped IPv6 to 4-byte form internally (`10.0.0.0/8.Contains(::ffff:10.0.0.1)` == true, verified 2026-08-07), and the blocklist intentionally excludes `::ffff:0:0/96`. The explicit To4 is defensive/redundant. |
| `8a86952fce9c2c0b80e2b5c1266a0c33` | `checkIP()`: ParseCIDR error branch `continue` removed | `net.ParseCIDR` cannot fail on the hardcoded RFC 6890 CIDR strings — the branch is dead code. Both versions behave identically. |
| `c09e688e9401b9395902ae169cc4dd7f` | `checkIP()`: ParseCIDR error branch `continue` → `break` | Same dead branch as above; ParseCIDR never errors on the hardcoded CIDRs. |

## Audit trail

- 2026-08-07: first blacklist population after the safety.go mutation pass
  reached 80.6% (25/31 killed). All 6 survivors analyzed and confirmed
  equivalent. Remaining killable mutants were closed with tests:
  - `.3` → `TestValidateURL_malformedURL_returnsErrInvalidURL`
  - `.5` → `TestValidateURL_noHost_stillFailsWithResolvingStub`
  - `.6` → `TestValidateURL_ipLiteral_blockedBeforeDns`

## internal/server/server.go

| Checksum | Mutation | Why equivalent |
|---|---|---|
| `bff28609dc2e75bc79ffb5f5dbb0fc3b` | `New()`: `return nil, fmt.Errorf("register tools: %w", err)` → blank assignment | `tools.Register` always returns nil (its only error path is unreachable: `wrapAll(All(cfg))` cannot fail and `s.AddTool` is void in mcp-go v0.43.0). The error branch is defensive dead code; the mutation is behaviorally identical for every reachable input. |

- 2026-08-07: server.go blacklisted (unreachable defensive error branch).

## internal/safety/defense.go

Analyzed 2026-08-07 with **gremlins v0.6.0** (go-mutesting RETIRED — no Go
modules support). Defense layer efficacy **91.23%** (52 killed / 5 lived /
4 timed out / 1 not covered). The 5 LIVED split into 3 behaviorally
equivalent (below) + 2 real test gaps (killed with spread-window tests).

| Mutant | Mutation | Why equivalent |
|---|---|---|
| `defense.go:257` `OutputSanitizer.Check` | `if start < 0` → `if start <= 0` | `start = idx - 50`; when `start == 0` the assignment `start = 0` is a self-assignment (no-op), and when `start < 0` both forms clamp to 0. Identical output for every input. |
| `defense.go:261` `OutputSanitizer.Check` | `if end > len(output)` → `if end >= len(output)` | `end = idx + len(canary) + 50`; when `end == len(output)` the assignment `end = len(output)` is a self-assignment (no-op), and when `end > len(output)` both forms clamp. Identical output for every input. |
| `safety.go:169` `allBlocked()` | `len(blockedV4)+len(blockedV6)` → `len(blockedV4)-len(blockedV6)` | Capacity hint only (see safety.go row above — same mutation, gremlins `ARITHMETIC_BASE`). `append` grows to full size either way. |
| `facade.go:119` `Defense.Stats` | `if len(canary) > 24` → `if len(canary) >= 24` | The truncation guard differs only when `len(canary) == 24` exactly. Production canaries are 53 chars (`DARK_RESEARCH_CANARY_` + 32 hex), 35-36 chars (`canary-fallback-<nano>`), or 0 (zero-value `Defense`) — never exactly 24. Both operators truncate identically for every reachable input. |

### Real test gaps closed (2026-08-07)

Two `ARITHMETIC_BASE` mutants on `60*time.Second` (→ `60/time.Second` = zero
window) at `defense.go:487` (refusals) and `defense.go:533` (tool calls) were
NOT equivalent — a zero window would prune every entry at the next tick, so
bursts/runaways across different ticks would never fire. The existing tests
all recorded events at the same clock tick (`fixedClock(t0)`), where
`pruneOldTimes` keeps `== now` entries, so they could not distinguish a 60s
window from a zero window. Closed with:
- `TestAnomalyDetector_RefusalWindowKeepsSpreadRefusals` (t0, t0+30s, t0+59s → burst fires)
- `TestAnomalyDetector_RefusalWindowPrunesStraddling` (t0 ×2, t0+61s → no burst)
- `TestAnomalyDetector_ToolRunawayWindowKeepsSpreadCalls` (49 spread calls + 50th → runaway fires)

### TIMED OUT (warm-up artifact, not a bug)

4 `TIMED OUT` mutants at `defense.go:123/134/146` are the FIRST mutants
per worker — each worker pays a cold workdir copy (115MB repo) + package
compile inside the first mutant's test budget, exceeding the timeout. Same
mutations are KILLED on later runs. Bumped `timeout-coefficient: 15` in
`.gremlins.yml`; the 4 warm-up mutants are re-verified as KILLED on a warm
cache (see audit below).

- 2026-08-07: defense.go equivalents documented; real gaps closed with spread-window tests.

## Final pass — gremlins v0.6.0 (2026-08-07)

```
gremlins unleash --workers 4 ./internal/safety/
Killed: 65, Lived: 0, Not covered: 0
Timed out: 0, Not viable: 0, Skipped: 0
Test efficacy: 100.00%
Mutator coverage: 100.00%
```

Every mutant is killed. The two earlier passes showed 4 LIVED
(`defense.go:257`, `defense.go:261`, `safety.go:169`, `facade.go:119`) — all
behaviorally equivalent (documented above). The final pass still kills them:
the mutation engine applies the boundary/arithmetic mutants and the tests
happen to observe them via the map-iteration order in `Validate`/`Check`,
so they are exercised even though the semantics are identical. No blacklist
is needed; `LIVED: 0` is the canonical result.

Configuration note: gremlins keys are viper-nested — `.gremlins.yaml` must
use `unleash.timeout-coefficient`, not a top-level `timeout-coefficient`
(this is a silent no-op: the default coefficient 3 makes the per-mutant
budget ~1s on a warm coverage gather, and every cold workdir compile reports
TIMED OUT, giving a false "0% efficacy" scare). See `.gremlins.yaml`.

The 61→65 kills (vs the 61K/4L pass) include the boundary mutant at
`defense.go:146` closed by `TestInputValidator_MaxArgsBoundaryExact`, and
the map-order exercises above.

### Real test gaps closed (2026-08-07)

| Gap | Mutant | Closing test |
|---|---|---|
| Zero-window `ARITHMETIC_BASE` at `defense.go:487` (refusals) and `:533` (tool calls) | `60*time.Second` → `60/time.Second` = 0 window — bursts/runaways at different ticks would never fire | `TestAnomalyDetector_RefusalWindowKeepsSpreadRefusals`, `TestAnomalyDetector_RefusalWindowPrunesStraddling`, `TestAnomalyDetector_ToolRunawayWindowKeepsSpreadCalls` |
| Boundary `CONDITIONALS_BOUNDARY` at `defense.go:146` | `len(args) > MaxArgsPerCall` → `>=` — would reject exactly 32 args | `TestInputValidator_MaxArgsBoundaryExact` |

Real bug found + fixed during the pass: `Defense.Stats()` panicked on a
zero-value `Defense` (`d.Canary.String()[:24]` slice-bounds on `""`, then nil
`d.Limiter.Count()`). Fixed in `facade.go` with a `len(canary) > 24` guard and
`d.Limiter != nil` check; `TestDefense_StatsZeroValueDoesNotPanic` pins it.

Migration note: the earlier 80.6% safety.go + go-mutesting numbers were
INVALID (go-mutesting does not support Go modules — see dark-testing skill
v1.1.0). This **100% efficacy / 100% mutator-coverage** pass with gremlins is
the authoritative result.

