# Contributing to daybook

Thanks for looking. daybook is small on purpose — one binary, one SQLite file,
a handful of packages with strict boundaries. Read
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) first; it's short and it explains
where things go and why.

## Good first contributions

- **A new agent source.** Cursor, opencode, Aider, Gemini CLI, Copilot CLI —
  each is one package under `internal/source/<name>` implementing
  `source.Source`, plus a fixture in `testdata/`. Nothing else changes.
- **A provider adapter** that `type = "openai"` + `base_url` doesn't already
  cover.
- **Scrub patterns** for secrets we miss — add the case to
  `internal/scrub/testdata/golden.txt` first, then the regex.
- **Prompt quality** — better session/digest prompts, with a before/after on
  the fixture sessions.

Look for issues tagged `good first issue` and `help wanted`.

## Workflow

1. Open an issue before a large change so we can agree on the shape.
2. Fork, branch from `main`, keep commits small and in
   [Conventional Commits](https://www.conventionalcommits.org/) form
   (`feat(source): add cursor parser`).
3. `make lint test` must pass. CI runs the same thing on macOS.
4. Every behaviour change ships with a test. Parsers and scrub use golden
   files; everything that talks HTTP uses `httptest`.
5. One PR, one concern. A refactor and a feature are two PRs.

## Ground rules that won't change in review

- **No cgo** in the Go binary. The menubar app under `macos/` is the only
  place native code lives.
- **Nothing unscrubbed reaches a `cloud = true` provider.** There is no flag
  for this and there won't be one.
- **Dependencies are a cost.** If the stdlib does it, use the stdlib. New
  modules need a sentence in the PR explaining why.
- **Package dependencies point downward** as in the architecture table.
  `web` reads the DB; it never calls the summariser.
- **Loopback only.** The web UI binds `127.0.0.1`. Don't add a flag to change
  that without adding auth.

## Fixtures and privacy

Never commit a real transcript. Run `daybook fixture <session-id>` to produce
a scrubbed, truncated copy under `testdata/`, then read it end-to-end before
committing. Reviewers will read it too.

## Running locally

```sh
brew install ollama && ollama pull qwen2.5:3b
make build
./daybook backfill --limit 5     # ingest + summarise five sessions
./daybook serve                  # http://127.0.0.1:7331
```

## Releasing (maintainers)

Tag `vX.Y.Z` on `main`; goreleaser builds darwin/arm64 + amd64, publishes the
GitHub release and bumps the Homebrew tap.
