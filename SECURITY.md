# Security Policy — dark-research-mcp

## Scope

This policy covers:
- The dark-research-mcp source code
- The dark-research-mcp binary releases on GitHub
- The constitutions in `internal/constitution/constitutions/`
- The example mods in `mods-examples/`

This policy does NOT cover:
- The orchestrator (Pi, opencode, Claude Code, etc.) that calls dark-research-mcp
- The model provider (Anthropic, OpenAI, MiniMax, etc.) whose LLM is the judge
- The user's host system

For the full threat model, see [`docs/security/threat-model.md`](docs/security/threat-model.md).

## Reporting a Vulnerability

If you have found a vulnerability in dark-research-mcp, please report it privately to the maintainers. **Do not** open a public GitHub issue for security-relevant findings.

**Contact:** Open a private security advisory at <https://github.com/Opita-Code/dark-research-mcp/security/advisories/new>. The maintainers are notified via GitHub.

**What to include:**
- A description of the vulnerability
- The attack vector (e.g. "via tool argument", "via constitution extraction")
- A minimal reproduction (a prompt, a tool call sequence)
- The impact (e.g. "allows a non-researcher to extract the dark constitution")
- The dark-research-mcp version affected

**What to expect:**
- Acknowledgment within 7 days
- A patch or mitigation within 30 days for critical findings; 90 days for others
- Coordinated disclosure: we will work with you on a disclosure timeline
- Credit in the fix's release notes (unless you prefer anonymity)

## Out-of-Scope Findings

The following are NOT considered vulnerabilities in dark-research-mcp and should not be reported as such:

- **Bypasses of the underlying LLM's safety training.** The LLM (e.g. Claude, GPT-5, MiniMax-M3) is the orchestrator's model, not ours. dark-research-mcp does not implement the model. If the model is fooled by a prompt injection, that is a model-level finding, not a dark-research-mcp finding.
- **Bypasses of the orchestrator's defenses.** If Pi or opencode is compromised, that is the orchestrator's problem, not ours. The defense layer in dark-research-mcp defends against an orchestrator that may be compromised; it does not defend the orchestrator itself.
- **"The dark constitution is dual-use."** This is a known and intentional property of the project. See the threat model for the dual-use reasoning and the defenses that mitigate it. We do not consider the existence of the dark constitution a vulnerability.
- **"The example mod teaches jailbreak techniques."** The example mod `red-team-jailbreak-arsenal` is a research artifact. Its content is documentation of known attack patterns, not new attacks. The mod is opt-in (auto_load = false). See the threat model for the disclosure policy.

## Disclosure Norms

We follow the AI red-team disclosure norms published by the OWASP GenAI Security Project and the AI red-team community. The key principles:

1. **Coordinated disclosure.** Report privately first. Work with the maintainers on a fix timeline.
2. **Defense-first publication.** When disclosing an attack, also disclose the defense. dark-research-mcp is published with both the attack surfaces (the dark constitution, the arsenal mod) and the defenses (the L1-L9 layers).
3. **No weaponization.** Do not use vulnerabilities found in dark-research-mcp to weaponize the project. The whole point of the project is research; the maintainers will not work with researchers who use findings offensively.
4. **Credit.** Reporters who follow this policy are credited in the fix's release notes (unless anonymity is requested).

## What is "Responsible Use" of dark-research-mcp?

dark-research-mcp is a **research instrument**. It is designed to enable:

- AI safety research (studying how LLMs respond to adversarial prompts)
- Red-team and penetration testing (authorized)
- OSINT and forensic analysis
- Adversarial ML research (studying how LLMs can be made to comply with legitimate research tasks that commercial LLMs refuse)

It is NOT designed for:

- Generating phishing content at scale
- Jailbreaking commercial LLMs to bypass their safety training for harm
- Automated content moderation evasion
- Any use that violates the EU AI Act, US state AI laws (e.g. California SB-1001), or the AISI voluntary commitments
- Targeting minors
- Operational synthesis of weapons of mass destruction

The `scope.does_not` list in the dark constitution is the project's product-scope limit. The maintainers do not enforce these limits via the LLM (the LLM can be fooled). The defense layer in the binary (`internal/safety/`) is what makes the project safer to operate. Operators are responsible for using the project within their jurisdiction's laws and within the project's scope.

## Security Updates

Security fixes are released as patch versions (e.g. v0.4.1, v0.4.2). Subscribe to GitHub releases at <https://github.com/Opita-Code/dark-research-mcp/releases> to be notified.

The threat model document ([`docs/security/threat-model.md`](docs/security/threat-model.md)) is updated with every defense change. If a release adds a new defense, the threat model reflects it.

## Hall of Fame

This section credits researchers who have reported vulnerabilities in dark-research-mcp following this policy.

(none yet — first release of this policy is v0.4.1)

## Contact

- **GitHub security advisories:** <https://github.com/Opita-Code/dark-research-mcp/security/advisories/new>
- **Maintainers:** Opita-Code organization
- **Public issues** (non-security): <https://github.com/Opita-Code/dark-research-mcp/issues>
