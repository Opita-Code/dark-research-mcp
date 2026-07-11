# Contributing to dark-research-mcp

Thanks for considering a contribution. This project is part of the **Opita Code**
ecosystem — practical software for real research and engineering, not for pretty
demos. Your PRs will be judged on whether they make the tool more useful for
people doing actual OSINT, vibe-coding, and red-team work.

## Quickstart

```bash
git clone https://github.com/Opita-Code/dark-research-mcp.git
cd dark-research-mcp
go test ./...
```

If `go test ./...` passes locally, your PR is on the right track. CI runs the
same on Go 1.22 and 1.23 with `-race`.

## Ground rules

1. **One commit per logical change.** Squash your WIP commits locally before pushing.
2. **Run `go test ./...` before every push.** CI is the ground truth — if it fails,
   your PR will be bounced.
3. **Don't introduce `// XXX` or `// TODO` without an issue number.**
4. **No silent breaking changes.** If your PR changes a public tool's
   response shape, document it in `CHANGELOG.md` (if we have one — otherwise
   the commit message) and flag it in the PR description.
5. **No new dependencies without discussion.** Each new `go.mod` line is a
   long-term commitment. Open an issue first if you want to add a runtime
   dep. Test-only deps are fine.
6. **Match the voice.** Commit messages in English, code in English, comments
   in English. The README is bilingual by intent (Spanish for users, English
   for code), but source code stays English.

## Code style

- **Go**: `gofmt`, `go vet`, and the lints in `.github/workflows/go-test.yml`.
  No `golint`, no `golangci-lint` (yet) — keep it boring.
- **No init() functions** unless they're registering a research backend or an
  MCP tool. Package-level state should be opt-in.
- **No globals in tests.** Pass dependencies explicitly or use `t.TempDir()`.
- **Errors**: wrap with `fmt.Errorf("package: context: %w", err)` — never
  swallow with `_` unless the comment explains why.
- **JSON tags**: every exported struct field that crosses the JSON boundary
  must have an explicit `json:"snake_case"` tag. MCP tool args follow the
  same rule via `jsonschema`.

## Adding a new OSINT backend

1. Implement `research.Backend` interface (see `internal/research/backends.go`).
2. Add your backend to `DefaultRegistry()` with a sensible `Weight` (lower
   numbers = tried first).
3. Add a unit test in `internal/research/backends_test.go` that mocks the
   HTTP response with `httptest.NewServer`.
4. If your backend requires an API key, document it in `README.md` config table.
5. Open a PR. Title format: `feat(research): add <backend> backend for <intent>`.

## Adding a new MCP tool

1. Decide which family: OSINT, memory, vibe-flow CRUD, dark-ssd, or standalone.
2. Add the function to the appropriate file in `internal/tools/`.
3. Register it in `internal/tools/tools.go`'s `All()`.
4. If it touches `dark.db`, add a mem method first and write a mem test.
5. If it calls the LLM, follow the LLM-as-judge pattern in
   `internal/tools/ssd.go` (system + user prompt, JSON-only response,
   persist to `sdd_evaluations`).
6. Update `README.md` and `ARCHITECTURE.md` tool counts (they hardcode 45).

## Adding a database migration

1. **Never edit a past migration.** Append to `internal/mem/migrate.go`'s
   `AllMigrations` slice.
2. The new migration's `Up` SQL must be idempotent (`CREATE ... IF NOT EXISTS`,
   `CREATE INDEX ... IF NOT EXISTS`, etc).
3. Add `Down` SQL for the symmetric operation if it's safe (DROP TABLE / INDEX).
4. Open the existing `dark.db` once locally — `Open()` will apply your migration
   and record it in `schema_migrations`. Verify the new tables/columns work.
5. Add a test in `internal/mem/migrate_test.go`.

## Adding a dark-ssd judge

1. Add a tool in `internal/tools/ssd.go` following the pattern of the existing
   5 (`brand_match`, `compliance_check`, `drift_judge`, `grounding_check`,
   `list_evaluations`).
2. The tool must:
   - Fetch context from `dark.db` (brand guide, compliance rule, spec, source)
   - Build a structured JSON-only system prompt
   - Call `c.CompleteJSON(ctx, system, user, &verdict)`
   - Persist the verdict to `sdd_evaluations` with `prompt_version`
   - Return the structured verdict + `persisted: true` to the agent
3. Update `internal/mem/types.go`'s `EvalType` doc comment with the new value.
4. Smoke-test against the real LLM before opening the PR.

## Commit message format

We use the Conventional Commits style loosely:

```
<type>(<scope>): <short summary>

<optional longer description>

<optional footer>
```

Common types: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `chore`.
Scopes: `research`, `mem`, `llm`, `tools`, `vault`, `ci`, `docs`.

Examples from history:
- `feat: 45 MCP tools, versioned schema, LLM cache, vault abstraction`
- `fix(ssd): handle 529 overloaded errors without caching the failure`
- `docs: open-source README (Opita-Code voice) + MIT LICENSE`

## Pull request process

1. Fork the repo and create a feature branch (`feat/<short-name>`).
2. Push commits to your fork.
3. Open a PR against `main`. The PR title is the merge commit title — make it
   descriptive.
4. Fill in the PR template (auto-loaded if present). Include:
   - What changed and why
   - How you tested (test commands, smoke results)
   - Backwards-compatibility notes (if any)
   - Screenshots for UX-visible changes (rare here, but possible for the
     README banner)
5. Wait for CI to pass. Address review comments promptly.
6. Squash-merge when approved.

## Issue reporting

- **Bugs**: include `go version`, OS, the exact command you ran, and the
  full stderr output. A minimal reproducer is worth ten paragraphs.
- **Feature requests**: open a Discussion first if you're not sure it fits
  the project scope. We may point you to a fork or to a downstream project.

## Code of conduct

By participating, you agree to abide by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
Be kind. We have all seen enough bad-faith GitHub threads.

## License

By contributing, you agree that your contributions will be licensed under the
MIT License. See [LICENSE](LICENSE).

---

*"No construimos software para que se vea bonito en una presentación. Lo construimos para que trabaje contigo todos los días."* — Opita Code