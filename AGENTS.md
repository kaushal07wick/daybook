# AGENTS.md — onboarding for the next agent (or human)

Read this first. It is the map; `docs/ARCHITECTURE.md` is the territory.
Keep it current: when you finish a milestone or make a ruling, edit the
**State** and **Rulings** sections here. Stale onboarding is worse than none.

## What this is

daybook is a single Go binary that watches AI-coding-agent transcripts
(Claude Code `~/.claude/projects/*/*.jsonl`, Codex `~/.codex/sessions/**/*.jsonl`),
stores them in SQLite, summarises each closed session with an LLM into a
strict JSON record, rolls sessions into daily/weekly digests, and serves them
on `127.0.0.1:7331`. Output is meant to be resume- and tweet-grade: concrete,
quantified, past tense. Local Ollama by default; cloud providers only ever see
`scrub.Text` output.

Owner: Kaushal Choudhary (`kaushal07wick`). Public repo, MIT. Quality bar:
"someone could open-source this and invite collaborators" — lint clean, tests
per layer, Conventional Commits, no real transcript data in the repo, ever.

## State (update me)

Branch `feat/m0-m3` (off `main`). Plan: `docs/superpowers/plans/2026-09-22-daybook-m0-m3.md`
(15 tasks). Execution ledger with every ruling:
`.superpowers/sdd/2026-09-22-daybook-m0-m3/progress.md` (git-ignored, local only —
if it is gone, `git log` is the record).

| task | what | status |
|---|---|---|
| 1–2 | `internal/store` schema, sessions/events/offsets | done, reviewed |
| 3–5 | `internal/source` + `claude` + `codex` parsers | done, reviewed |
| 6 | ingest walk, `backfill --no-llm`, `status` (M1) | done, reviewed — 117 files → 113 sessions / 68k events in 7 s |
| 7–9 | `scrub`, `provider` (openai, anthropic), `config` | done, reviewed (scrub had 2 fix rounds) |
| 10 | store summaries / digests / terms / FTS | done, reviewed |
| 11 | `internal/summarize` + `backfill` wiring (M2) | committed `d5c422f`; fix round in progress: native `ollama` provider + prompt tune |
| 12 | day/week digests + `digest` cmd | not started |
| 13 | fsnotify watcher + scheduler + `serve` | not started |
| 14 | web UI (`internal/web`, html/template, embedded) | not started |
| 15 | launchd `install`/`uninstall` + README pass (M3) | not started |

After M3: M4 = ledger export (`daybook export`), M5 = Swift menubar app
under `macos/` + goreleaser + brew tap + `v0.1.0`.

## Run it

```sh
make lint test build                 # the gate; golangci-lint 2.13 is installed
DAYBOOK_HOME=/tmp/dbtest ./daybook backfill --no-llm   # ingest only, never touches ~/Library
DAYBOOK_HOME=/tmp/dbtest ./daybook backfill --limit 5  # summarise 5 sessions with the default provider
DAYBOOK_HOME=/tmp/dbtest ./daybook status
sqlite3 /tmp/dbtest/daybook.db 'select json_extract(json,"$.headline") from summaries'
```

- `DAYBOOK_HOME` moves the DB. `DAYBOOK_CONFIG` moves the config file
  (`config.Path()` does **not** honour `DAYBOOK_HOME` — a test run once wrote
  a stale default to `~/.config/daybook/config.toml`; check for it).
- Ollama runs as a brew service; `qwen2.5:3b` is pulled. `ollama ps` shows
  what's loaded. Bench on this M1 Pro: Ollama/Metal ≈ 47 tok/s, MLX ≈ 63 tok/s
  for the same 4-bit 3B model. MLX is an opt-in `type = "openai"` provider
  block, not the default (no brew-service story for adopters).
- Never test against the user's real `~/Library/Application Support/daybook`
  until `install` is meant to be real.

## How the pieces fit (30-second version)

```
source.Watch / IngestAll → source.<kind>.Parse → store.AppendEvents (offset + events, one tx)
store.CloseIdle (30 min idle) → summarize.Drain → Render → [scrub if Cloud()] → Chunk → provider.Complete
→ SessionSummary JSON → store.PutSummary + UpsertTerms (+ fts)
schedule (23:55 day, Sun 23:58 week) → summarize.Day/Week → store.PutDigest
web.Handler reads store only. cmd/daybook wires it all.
```

Dependency direction is strictly downward: `web` never imports `summarize`;
`provider` never imports `store`. Adding an agent source = one package under
`internal/source/<name>` implementing `source.Source` + a synthetic fixture.

## Rulings that changed the plan (don't re-litigate, do respect)

- **Parsers keep per-path state.** `Parse` is called with only the bytes
  appended since last time, so tool-use-id → name maps live on the `*Source`
  keyed by path. Register with `source.Register(&Source{})`.
- **`MaxToolText` is 2048 runes**, not bytes (never split UTF-8).
- **`store.Unsummarized(limit, provider, model)`** — three args. Failed
  summaries (`json IS NULL`) are retried only when provider/model changed;
  unconditional retry would loop forever on a model that can't emit JSON.
- **`Search` swallows only FTS5 syntax errors** (sqlite code 1 + "fts5" /
  "unterminated string"); BUSY/CORRUPT/IOERR propagate.
- **Scrub is the security boundary.** Golden test is the contract. IPs use a
  maximal-munch run `[0-9A-Fa-f:.]+` + `net.ParseIP`; IPv4 and IPv6 share one
  counter. `-p <secret>` over-redaction is accepted (safe direction). Never
  weaken a pattern to make a test pass.
- **Cloud gating happens in exactly one place**: top of `summarize.Session`.
- **Ollama context is 4096 by default and `/v1` silently truncates.** Hence
  `EstimateTokens` = chars/3 (measured on qwen2.5) and the native `ollama`
  provider type that sets `options.num_ctx` (default 16384) — without it a
  300 KB session became 47–49 chunks and took 12–17 min.
- **`--limit`** on `backfill` caps *summarisation*, not ingest.
- **Lint passes honestly.** `_ =` only where discarding is deliberate; use
  `...Context(...)` DB/HTTP calls; never disable a linter.
- **UI is a journal, not a dashboard**: one column, serif headings, mono for
  numbers/hosts, no KPI tiles, no card grids. Light/dark via
  `prefers-color-scheme`. Palette lives in `docs/PRD.html`.

## Gotchas you will hit

- `store.Open(":memory:")` needs `SetMaxOpenConns(1)` and the
  `file::memory:?_pragma=…` DSN or each connection gets its own empty DB.
- `golangci-lint` v2 config format (`version: "2"` in `.golangci.yml`).
- `modernc.org/sqlite` pulls a large transitive closure into `go.sum`; that
  is expected, not scope creep. Allowed direct deps: sqlite, fsnotify, toml,
  x/sync — anything else needs a PR justification.
- Two implementers editing the same tree at once bit us once (`git stash`
  mid-flight). One implementer at a time.
- `source.Files` glob is `projects/*/*.jsonl` on purpose — nested subagent
  transcripts are skipped.

## How work gets done here

Superpowers-style: brainstorm → spec (`docs/ARCHITECTURE.md`, `docs/PRD.html`)
→ plan with TDD tasks → one implementer subagent per task → task review
(spec + quality) → fix rounds → final whole-branch review → merge. Briefs are
extracted per task into `.superpowers/sdd/<plan>/task-N-brief.md`; the
implementer writes `task-N-report.md`; every decision goes in `progress.md`
as `Ruling: … — why — cost if wrong`. If you're a single agent working alone,
still: read the task's brief, TDD it, `make lint test`, commit with the
trailer, and update **State** above.

Commit trailer in use: `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.

## Open questions

- Default local model: `qwen2.5:3b` chosen for RAM (16 GB M1 Pro); a
  10-session bake-off vs `qwen2.5:7b` / `llama3.2:3b` on JSON validity and
  headline specificity is still owed.
- Session "closed" = 30 min idle; Claude Code resumes sessions days later —
  a resumed session re-opens and gets re-summarised only if provider changes.
  Decide whether resumed segments should be separate digest entries.
- Ledger term normalisation ("TensorRT"/"TRT") — raw in v1.
