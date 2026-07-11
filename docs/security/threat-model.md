# Threat Model — dark-research-mcp v0.4.1+

## Purpose

This document describes the security posture, threat model, and defense architecture of dark-research-mcp. It exists so that:

1. **Operators** understand what they are deploying and what assumptions hold.
2. **Contributors** know which defenses are non-negotiable and where to add new ones.
3. **Auditors** can verify the security claims against the published evidence.
4. **The public** can hold the project accountable for dual-use risk.

This document is **public, version-controlled, and updated with every defense change**. If a release lacks a corresponding threat-model update, that release is not security-approved.

## Scope

In scope:
- dark-research-mcp binary (light and dark constitution, both build tags)
- The 37 MCP tools exposed by the binary
- The system prompt assembly pipeline
- The defense layer in `internal/safety`
- The constitution files (`light.toml`, `dark.toml`)

Out of scope:
- The orchestrator (Pi / opencode / Claude Code) — the user is responsible for orchestrator security. dark-research-mcp defends against an orchestrator that may be compromised, but the orchestrator itself is a separate trust domain.
- The model provider (Anthropic, OpenAI, MiniMax, etc.) — model behavior is an input to our threat model, not a controllable factor.
- The user's host system — the defense layer assumes the host is not compromised. For compromised-host scenarios, see deployment requirements below.

## Adversary Model

### Who might attack us?

| Adversary | Capability | Motivation | Likely attack surface |
|---|---|---|---|
| **Script kiddie** | Low — copies configs from tutorials | Replication, vandalism | The `examples/` directory; the public `dark.toml`; the public `mods-examples/red-team-jailbreak-arsenal/` |
| **Adversarial ML researcher** | Medium — understands prompt injection, has tested Claude/GPT safety | Reproduce published attacks, study defenses | The tool LLM (dark_ssd_*); the constitution; the refusal interceptor |
| **Motivated attacker** | High — willing to craft multi-step attacks | Weaponize the project; produce a turnkey antirefusal scaffold | The full architecture: orchestrator + tool LLM + constitution + mods + arsenal |
| **Insider / supply-chain** | High — controls one of the dependencies | Backdoor, exfiltration | The Pi fork; the dark-research-mcp Go dependencies; the vault; the constitution source |
| **AI safety researcher** | Medium — wants to test our claims | Verify the threat model is real, not theater | The defense layer; the refusal interceptor; the canary mechanism |

### What do they want?

- **For weaponization (motivated attacker):** turnkey scaffolding to make a frontier LLM classify / generate content it would otherwise refuse. The user of the weaponized tool may be a spammer, phisher, scammer, or nation-state actor.
- **For study (researcher):** access to a working antirefusal pipeline to publish papers on. The risk is they weaponize it accidentally or sell the pipeline to an attacker.
- **For verification (safety researcher):** confirmation that the threat model is real, the defenses work, and the project is responsible. The risk is they find a hole we missed.

## Asset Inventory

| Asset | Sensitivity | Where it lives | Who can access it |
|---|---|---|---|
| `dark.toml` constitution | High — the policy that lowers safety guardrails | Repo source, gated by `-tags allow_builtin_dark` in binary | Public source, opt-in binary |
| `mods-examples/red-team-jailbreak-arsenal/` | High — the anti-refusal tactics | Repo source | Public |
| Defense layer (`internal/safety/`) | Critical — the only thing standing between us and weaponization | Repo source | Public |
| Canary token | Critical — detection of extraction attempts | Runtime, per-session | Process-local |
| Tool LLM verdicts | Medium — may contain LLM-generated content with embedded instructions | Runtime + audit log | Process-local + disk |
| `sdd_evaluations` audit log | Medium — every refusal, every verdict, every canary event | SQLite at `$DARK_DB` | Process-local + disk |
| Vault secrets | Critical — API keys for LLM providers | Windows Credential Manager | Process-local |
| User-provided tool arguments | High — the entire content of every tool call is user-controlled | Runtime | Process-local |

## Attack Vectors

### AV-1: Constitution extraction (HIGH)

**Description:** An attacker (or curious user) pastes the canary back into a tool argument, or asks the LLM to reveal its system prompt, to extract the dark constitution or the antirefusal system prompt for replication.

**Preconditions:** access to dark-research-mcp running with the dark constitution; ability to call a tool.

**Capability required:** low — a single tool call with the canary in any string argument.

**Existing defenses:**
- **L7 (canary detection in input):** the `InputValidator` rejects any tool call whose arguments contain the canary token. The call is logged to the audit table. The LLM is never called.
- **L7 (canary in constitution):** the dark constitution explicitly tells the LLM to refuse to reveal the system prompt.

**Residual risk:** low. The canary is generated per-session (128 bits of entropy). An attacker would have to guess the canary, which is infeasible.

**Defenses proposed but not yet implemented:** none — this vector is well-defended.

### AV-2: Indirect prompt injection via tool content (HIGH)

**Description:** Adversarial content (a prompt injection, a jailbreak, a phishing payload) is passed to a `dark_ssd_*` judge via the `content` or `claim` argument. The tool LLM reads it. The injection may attempt to override the tool LLM's system prompt and exfiltrate data or perform unauthorized actions.

**Preconditions:** access to dark-research-mcp; ability to call a `dark_ssd_*` tool with attacker-controlled content.

**Capability required:** low to medium — the attacker needs to know the dark_ssd_* tool interface (which is public) and craft injection content.

**Existing defenses:**
- **L1 (input length cap):** the `InputValidator` rejects any single string argument over 200KB. Prevents OOM and context-stuffing attacks.
- **L1 (injection marker detection in input):** the `InputValidator` LOGS (not blocks) injection markers in input. The dark_ssd_* judges are designed to receive such content, so blocking would break the legitimate use case. Logging is the audit signal.
- **L2 (output sanitization):** the `OutputSanitizer` checks the LLM's response for the canary and for injection markers. If the LLM is fooled into leaking the canary, the response is rejected.
- **L7 (canary in system prompt):** the canary is in the tool LLM's system prompt. If the LLM is fooled into revealing it, the output is rejected.
- **L8 (rate limiting):** a runaway loop in the orchestrator is throttled.
- **L9 (anomaly detection):** 3+ refusal bursts in 60s flag a potential active injection; 3+ canary leaks flag extraction attempts.

**Residual risk:** MEDIUM. The defense is in the LLM (system prompt) and in the binary (canary check). If the model is fooled into:
- Returning content that doesn't contain the canary but DOES contain instructions
- Performing actions the operator didn't ask for (within the LLM call)

...the orchestrator reads the response and may follow the instructions. This is the classic "indirect prompt injection" problem that affects ALL agentic AI systems in 2026.

**Defenses proposed but not yet implemented:**
- **L4 (sandboxing) — the binary has no sandbox.** A tool LLM call has full network egress to the LLM provider. A compromised LLM response could include URLs that, when fetched by the orchestrator, lead to exfiltration. The `web_fetch` tool has SSRF guards; the `web_search` tool routes through the safety module. But a future tool added without safety review could be a vector.
- **L10 (HITL) — no human-in-the-loop.** The current architecture is fully autonomous. A high-risk action (modifying a system file, calling an external API with a credential) has no approval gate.
- **L3 (privilege separation) — partial.** The orchestrator (Pi) has Bash/Read/Write/Edit. The tool LLM (dark-research-mcp) has only the LLM call. The boundary is at the orchestrator, not at the tool LLM. This is OK because the tool LLM cannot exfiltrate (it has no tool calls), but the orchestrator can.

### AV-3: Memory poisoning of the orchestrator (MEDIUM)

**Description:** The orchestrator (Pi) has tree-structured sessions persisted to disk. An attacker who can influence a session (via adversarial content processed by the LLM) can poison the session's "memory" such that future sessions inherit the poisoned state.

**Preconditions:** access to the orchestrator; ability to influence a session via tool calls.

**Capability required:** medium — requires understanding of how the orchestrator's memory works.

**Existing defenses:**
- The dark-research-mcp tool LLM has no persistent state across calls (stateless per-call).
- The audit log (`sdd_evaluations`) is append-only; an attacker cannot modify past entries.

**Residual risk:** MEDIUM. dark-research-mcp is not directly vulnerable (no persistent state in the binary). The orchestrator's memory is the orchestrator's responsibility, not ours. We document the risk; the user must defend their orchestrator.

**Defenses proposed but not yet implemented:** none — this is the orchestrator's problem.

### AV-4: Supply-chain attack via mod (MEDIUM)

**Description:** A malicious mod installed by the user (or auto-loaded) contains prompt-injection-style content in its `knowledge` and `directives` files. The mod's content is rendered into the tool LLM's system prompt via the `mod_directives` layer.

**Preconditions:** the user installs a mod from an untrusted source.

**Capability required:** medium — the attacker must produce a mod that looks legitimate.

**Existing defenses:**
- Mod TOML is parsed with a strict decoder (`DisallowUnknownFields`) — typos fail loud.
- Mod paths are validated: no `..`, no absolute paths, no path traversal (BUG #1 in v0.4.0-rc.5 fixed this).
- Mod SHA-256 is computed and stored in `mod_loads` for audit.
- Mods require explicit user activation (the example mods have `auto_load = false` by default).

**Residual risk:** LOW to MEDIUM. A malicious mod's content is still rendered into the system prompt, where the LLM will follow it. The defense is the user's vetting of the mod before installation. We do not auto-load any mod by default.

**Defenses proposed but not yet implemented:** a mod signature/verification mechanism (analogous to npm's integrity hashes) is a future enhancement. Not in v0.4.1.

### AV-5: Canary brute-force (LOW)

**Description:** An attacker tries to brute-force the canary token by repeatedly calling tools with guesses.

**Preconditions:** access to dark-research-mcp.

**Capability required:** high — the canary is 128 bits of entropy; brute force is infeasible.

**Existing defenses:**
- 128-bit canary generated per session by `crypto/rand`.
- Rate limit (default 1000 calls per session) limits guesses.
- Anomaly detection (3+ canary leaks in a session flags the attack).

**Residual risk:** NEGLIGIBLE.

### AV-6: Tool result content weaponization (MEDIUM)

**Description:** The tool LLM's `reasoning` field is free-text generated by the LLM. A compromised LLM (or an LLM fed a clever prompt injection via the `content` argument) may include instructions in `reasoning` that the orchestrator then reads and follows.

**Preconditions:** the LLM is fooled into including instructions in `reasoning`.

**Capability required:** medium — depends on the LLM's instruction-following strength and the attacker's craft.

**Existing defenses:**
- **L2 (output sanitization):** injection markers in the output are LOGGED. The orchestrator sees the log, but the response is still surfaced.
- **L5 (boundary markers in constitution):** the tool LLM is told that its own `reasoning` is [DATA], not instructions. This is a model-level defense; effectiveness depends on the model.
- **L7 (canary in output):** if the LLM's `reasoning` contains the canary, the response is REJECTED, not surfaced.

**Residual risk:** MEDIUM. The defense is partially model-level (boundary markers — depends on LLM obedience) and partially mechanical (canary — depends on the LLM not leaking the canary in its reasoning). A sophisticated attacker can craft injection that fools the LLM into producing reasoning that doesn't include the canary but does include other instructions.

**Defenses proposed but not yet implemented:**
- Strip known injection patterns from the LLM's `reasoning` field before returning to the orchestrator. This is a tradeoff: stripping changes the verdict and the user wants to see it. We log instead of strip. The user reviews the audit log.
- Run the LLM's `reasoning` through a SECOND, smaller, more constrained LLM that checks for instruction-following. This is "LLM-as-judge of the judge" — a future enhancement.

### AV-7: Denial of service via resource exhaustion (LOW)

**Description:** An attacker calls dark-research-mcp many times, exhausting the API rate limit, the disk, or the LLM budget.

**Preconditions:** access to dark-research-mcp.

**Capability required:** low.

**Existing defenses:**
- **L8 (rate limiting):** 1000 tool calls per session default. Configurable via `DARK_MAX_CALLS_PER_SESSION` env or future flag.
- LLM calls have a 60-second timeout.
- SQLite is opened with `WAL` mode and bounded cache sizes.

**Residual risk:** LOW. The rate limit caps the damage.

## Defense Architecture (mapping to Lushbinary 2026)

| Layer | Implementation | Status | Notes |
|---|---|---|---|
| L1 Input validation | `internal/safety/defense.go::InputValidator` | **Implemented (v0.4.1)** | Length cap, canary check, marker logging. Universal (light + dark). |
| L2 Output filtering | `internal/safety/defense.go::OutputSanitizer` | **Implemented (v0.4.1)** | Canary detection in tool results. Refuses to surface if canary leaks. |
| L3 Privilege separation | Orchestrator's responsibility | **Out of scope** | dark-research-mcp has no host system access; the orchestrator does. |
| L4 Sandboxing | Deployment-time concern | **Documented, not enforced** | Operators SHOULD run the orchestrator + dark-research-mcp in a container with no network or restricted network. |
| L5 Boundary markers | `dark.toml` + `BoundaryMarkers` constant | **Implemented (v0.4.1)** | In constitution footer. Tells the LLM that tool result text fields are [DATA]. |
| L6 Instruction hierarchy | Binary enforcement | **Implemented (v0.4.1)** | `defenseWrap` refuses to call the inner handler if input validation fails. |
| L7 Canary tokens | `internal/safety/defense.go::CanaryToken` | **Implemented (v0.4.1)** | 128-bit per-session. Embedded in system prompt by `BuildSystemPrompt`. Detected in input and output. |
| L8 Rate limiting | `internal/safety/defense.go::RateLimiter` | **Implemented (v0.4.1)** | 1000 calls per session default. |
| L9 Anomaly detection | `internal/safety/defense.go::AnomalyDetector` | **Implemented (v0.4.1)** | Heuristic: refusal bursts, canary leaks, tool runaways. Hook-based for future ML. |
| L10 Human-in-the-loop | Orchestrator's responsibility | **Out of scope** | For high-risk actions, the orchestrator should pause for human approval. dark-research-mcp itself cannot enforce this. |

## Deployment Requirements

To deploy dark-research-mcp safely, the operator MUST:

1. **Use the dark constitution only for research.** Not for production user-facing systems.
2. **Run in a sandboxed environment.** Docker, gVisor, or Firecracker. No direct host filesystem access from the orchestrator.
3. **Restrict network egress.** The orchestrator + dark-research-mcp should not have unrestricted internet access. Allow only the LLM provider's API endpoint.
4. **Audit the ssd_evaluations table.** Every tool call is logged. Review the log for `refused_attempts > 0` and `canary_leak_attempts > 0`.
5. **Set a conservative rate limit.** The default is 1000 calls per session. Lower it for untrusted users; raise it only for vetted workloads.
6. **Use a vetted orchestrator.** Pi, opencode, Claude Code with permission gates. Not a custom LLM loop without the same defenses.
7. **Use a vetted model provider.** MiniMax-M3, Anthropic, OpenAI. Not a self-hosted model unless the operator has independently evaluated its safety properties.
8. **Have a revocation path.** If the canary leaks, restart the process to mint a new one. The old canary is no longer valid.

## Open Risks (Known Unknowns)

1. **No end-to-end security test against a real LLM.** The 5/5 functional probes (Pi + dark SYSTEM.md + dark-research-mcp) tested functionality, not security. The 8 security probes (S1-S8) proposed earlier have NOT been run. Without these, we don't know if the defense actually catches indirect prompt injection in practice.

2. **No ML-based anomaly detection.** The current anomaly detector is heuristic. Sophisticated multi-step attacks that don't trigger the heuristic thresholds are not detected.

3. **No signed mods.** A malicious mod installed by the user is rendered into the system prompt with the same authority as a vetted mod. The user must vet mods manually.

4. **No operator UI for the defense layer.** The defense logs to stderr; there's no dashboard, no alerts, no way to visualize the canary events or anomaly bursts. Operators have to read the SQLite database and the stderr log.

5. **No public responsible disclosure process yet.** The SECURITY.md (next document) defines the policy, but no one has tested it.

## Decision Log

| Decision | Date | Rationale |
|---|---|---|
| Add L7 canary defense | 2026-07-11 | Direct defense against constitution extraction. Pre-empts the most obvious weaponization path. |
| Defense applies in light AND dark | 2026-07-11 | Light mode is the default for non-research use; an attacker who can convince a user to run the light binary in a research context should still be defended. |
| L2 logs but does not strip | 2026-07-11 | Stripping changes the verdict. The audit log is the operator's review path. A future enhancement may add a "strict" mode that strips. |
| L8 default cap = 1000 calls | 2026-07-11 | Conservative. Advanced users raise via env. Production deployments should be lower (100-200). |
| Mod auto_load = false in examples | v0.4.0-rc.2 | Mods must be explicitly activated. Reduces accidental weaponization. |
| dark.toml scope.does is permissive | v0.4.0-rc.1 | The constitution's research posture is broad. The defense layer (added v0.4.1) is what makes this safe. The combination is intentional. |

## Changelog

- **v0.4.1**: initial threat model document. Coincides with the addition of the defense layer (`internal/safety/`).
- (future) v0.5.0: review and update based on end-to-end security tests.
