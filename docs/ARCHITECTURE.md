# daybook — Architecture

One process, one static binary, one SQLite file. Every box in the diagram is a
Go package with a single job and a small interface; the only shared state is
the database.

```
~/.claude/projects/**/*.jsonl ─┐
~/.codex/sessions/**/*.jsonl  ─┤ fsnotify (kqueue)
                               ▼
                        internal/source ──── Source interface: Claude, Codex, …
                               │  []Event
                               ▼
                        internal/store ───── SQLite · WAL · FTS5 · migrations
                          │           ▲
           closed sessions│           │ summaries · digests · terms
                          ▼           │
                        internal/summarize ─ queue · map-reduce · JSON schema
                          │
                          │ scrub() iff provider.Cloud
                          ▼
                        internal/provider ── OpenAI-compatible · Anthropic
                          ▲
                        internal/schedule ── 23:55 daily · Sun 23:58 weekly
                          │
     :7331 ◄─────────── internal/web ─────── html/template · embed · FTS search
                          ▲
                        cmd/daybook ──────── install serve backfill digest status export
```

## Packages

| package | owns | depends on |
|---|---|---|
| `cmd/daybook` | CLI (stdlib `flag`, subcommand switch), launchd plist install, wiring | everything below |
| `internal/config` | `config.toml` load/validate, first-run Ollama probe, defaults | — |
| `internal/source` | `Source` interface, fsnotify watcher, offset-tailing, `Event` type | `store` (offsets) |
| `internal/source/claude` | Claude Code JSONL parser | `source` |
| `internal/source/codex` | Codex JSONL parser | `source` |
| `internal/store` | schema, migrations, all SQL, FTS5 | `modernc.org/sqlite` |
| `internal/scrub` | deterministic redaction with stable placeholders | — |
| `internal/provider` | `Provider` interface + two adapters, token estimation | `net/http` |
| `internal/summarize` | session summariser, chunking, map-reduce, digest builders, ledger upsert | `store`, `provider`, `scrub` |
| `internal/schedule` | timer loop for daily/weekly digests and session-close sweeps | `summarize`, `store` |
| `internal/web` | HTTP handlers, templates, static assets (embedded) | `store` |

Dependency direction is strictly downward in this table. `web` never imports
`summarize`; it reads what `summarize` wrote. `provider` never imports `store`.

## Core interfaces

```go
// internal/source
type Event struct {
    SessionID string
    Kind      string    // "claude" | "codex"
    Project   string    // derived from cwd, e.g. "work/fleet-manager"
    CWD       string
    TS        time.Time
    Role      Role      // User | Assistant | Tool
    ToolName  string    // when Role == Tool
    Text      string    // ≤ 2 KB for tool results
}

type Source interface {
    Kind() string
    Globs() []string                                  // watched paths
    Parse(path string, r io.Reader) ([]Event, error)  // parses from an offset; ignores unknown lines
}
```

```go
// internal/provider
type Provider interface {
    Name() string
    Cloud() bool                    // gates scrub()
    MaxInputTokens() int            // drives chunking
    Complete(ctx context.Context, req Request) (Response, error)
}

type Request struct {
    System string
    User   string
    JSON   bool   // ask for a JSON object; adapters map to response_format / tool-use
}
```

```go
// internal/summarize — the one shape every LLM call must return
type SessionSummary struct {
    Headline string   `json:"headline"`
    Project  string   `json:"project"`
    Work     []string `json:"work"`     // what was done, past tense, concrete
    Tech     []string `json:"tech"`     // libraries, tools, protocols, models
    Numbers  []string `json:"numbers"`  // "72 cams @ 45% GPU", "17-19 fps", "620 MB"
    Facts    []string `json:"facts"`    // gotchas, decisions, "X because Y"
    Hosts    []string `json:"hosts"`    // servers / boxes touched (placeholders if scrubbed)
    Outcome  string   `json:"outcome"`  // shipped | partial | blocked | investigation
}
```

## Data flow

### Ingest
1. `source.Watcher` registers every `Globs()` directory with fsnotify (recursively; new subdirs are added on `Create`).
2. On `Write`/`Create` for a matching file, it looks up `sources.byte_offset`, seeks, hands the remainder to `Parse`, and appends events in one transaction along with the new offset. Partial trailing lines are not consumed (offset stays before them).
3. `sessions` rows are upserted from the first event; `ended_at` is bumped on every batch.
4. A sweep every 5 min marks a session `closed` when `now - ended_at > close_after` (default 30 min) and enqueues it for summarisation.

Backfill is the same code path driven by a directory walk instead of fsnotify.

### Summarise
1. Queue worker pulls closed sessions with no summary (oldest first), concurrency = `provider.concurrency` (1 local, 4 cloud).
2. Events are rendered to a compact transcript (`U:`/`A:`/`T(name):` lines). If it exceeds `MaxInputTokens()*0.8` it's split into chunks at message boundaries; each chunk is summarised, then the chunk summaries are reduced with the same schema.
3. If `provider.Cloud()`, the transcript passes through `scrub.Text()` first. Scrubbed text is never persisted.
4. Response is parsed strictly into `SessionSummary`. Invalid JSON → one retry with the error appended. Second failure → stored with `json = null, raw = <text>` so the session is never silently dropped and can be retried after a provider change.
5. `terms` and `term_links` are upserted from `Tech/Numbers/Hosts/Project` — no LLM involved.

### Digest
- `schedule` fires at 23:55 local: load the day's summaries → one LLM call → `digests(period='day')`.
- Sunday 23:58: load the 7 day digests → one call → `digests(period='week')`.
- Both are idempotent on `(period, key, provider, model)`; `--force` regenerates.
- If no provider is reachable the job is retried every 15 min until it succeeds; a missed day is generated the next time the process is up (`schedule` checks for gaps on start).

### Serve
- `net/http` mux, `127.0.0.1:7331` only, no auth (loopback). Pages: `/` (today), `/day/{date}`, `/week/{iso-week}`, `/ledger`, `/sessions`, `/sessions/{id}`, `/search?q=`, `/settings`.
- Templates and a single CSS/JS file are `embed`ded. No build step.
- `/api/status` returns the same JSON as `daybook status --json`; the menubar app polls it.

## Storage

```sql
sources    (id INTEGER PK, kind TEXT, path TEXT UNIQUE, byte_offset INTEGER, mtime INTEGER, last_seen INTEGER);
sessions   (id TEXT PK, kind TEXT, project TEXT, cwd TEXT, started_at INTEGER, ended_at INTEGER, closed INTEGER, n_events INTEGER);
events     (id INTEGER PK, session_id TEXT, ts INTEGER, role TEXT, tool_name TEXT, text TEXT);
summaries  (session_id TEXT PK, provider TEXT, model TEXT, json TEXT, raw TEXT, created_at INTEGER);
digests    (period TEXT, key TEXT, provider TEXT, model TEXT, json TEXT, md TEXT, created_at INTEGER, PRIMARY KEY(period,key));
terms      (term TEXT, kind TEXT, first_seen INTEGER, last_seen INTEGER, n INTEGER, PRIMARY KEY(term,kind));
term_links (term TEXT, kind TEXT, session_id TEXT, PRIMARY KEY(term,kind,session_id));
fts        VIRTUAL TABLE USING fts5(kind, key, body);   -- rows for summaries and digests
```

- WAL mode, `synchronous=NORMAL`, single writer goroutine funnels all writes (SQLite is happiest that way; reads are concurrent).
- Migrations are numbered `.sql` files embedded in `store`, applied in a transaction with `user_version` tracking.
- Location: `~/Library/Application Support/daybook/daybook.db`. Override with `DAYBOOK_HOME`.

## Privacy

`scrub.Text(s string, extra []string) string` replaces, in order:

1. PEM private-key blocks → `<private-key>`
2. `password=`, `passwd:`, `-p <x>`, `token:`, `Authorization: Bearer <x>` values → `<secret>`
3. Key-shaped tokens (`sk-…`, `AKIA…`, `ghp_…`, `xox[abp]-…`, `gho_…`, 32+ char hex/base64 runs) → `<key>`
4. Emails → `<email-N>`
5. IPv4 / IPv6 → `<ip-N>`
6. User `redact` list, longest first, case-insensitive → `<redacted-N>`

`N` is stable within one call (same IP → same placeholder) so the model can still say "restarted the service on `<ip-2>` twice". The function is pure and has a golden-file test. There is deliberately no config option to skip it for cloud providers.

## Process model

- `daybook serve` is the only long-running entry point. `install` writes a `LaunchAgent` plist that runs `serve` with `KeepAlive`, `RunAtLoad`, `Nice 10`, logs to `~/Library/Logs/daybook/`.
- Goroutines: watcher, N summariser workers, scheduler, HTTP server, one DB writer. All wired with a root `context` and `errgroup`; SIGTERM drains the summariser (current call finishes, queue persists in DB).
- `backfill` runs in the foreground and refuses to start if `serve` holds the DB lock — it's the same code, so the user just runs `daybook backfill` once before `install`, or `serve` handles it lazily on first start.

## Menubar (P1)

A ~150-line Swift `NSStatusItem` app under `macos/`, built with `swiftc` in CI, that polls `/api/status` every 30 s, shows a count badge, and offers *Open daybook* / *Summarise now* / *Pause*. It shells out to the `daybook` binary for actions. Keeping it separate keeps the Go build cgo-free.

## Testing

| layer | how |
|---|---|
| parsers | fixture JSONL (scrubbed real transcripts) → golden `[]Event` |
| scrub | golden input/output file; property: idempotent |
| provider | `httptest.Server` faking both APIs, including invalid-JSON and 429 paths |
| summarize | fake provider returning canned JSON; asserts chunking boundaries and retry/raw-fallback |
| store | in-memory SQLite, migration up from empty and from each prior version |
| web | `httptest` against a seeded DB, asserts rendered pages contain expected strings |
| e2e | `daybook backfill --no-llm` against `testdata/` then `status --json` |

CI: `go vet`, `golangci-lint`, `go test -race` on macOS runner.

## Extending

- **New agent source**: add `internal/source/<name>` implementing `Source`, register it in `source.All()`. Nothing else changes.
- **New LLM API shape**: add an adapter in `internal/provider`; config `type = "<name>"`. Most vendors are already covered by `type = "openai"` + `base_url`.
- **New digest period** (month, quarter): a third builder in `summarize` and a schedule entry; the `digests` table already keys on `period`.

## Rejected alternatives

- **Vector DB / RAG** — the product is a digest, not a chatbot. FTS5 covers "find that session".
- **Python** — faster to prototype, worse to distribute; adopters must not need a venv.
- **Rust** — no benefit for an I/O-bound daemon; slower iteration.
- **Native SwiftUI app** — the value is in the pipeline, not the window chrome; a web page embedded in Go ships in a fraction of the time and works for the Linux port later.
- **Cron library / job scheduler** — two timers.
- **SDKs for LLM vendors** — two hand-written HTTP clients are smaller than one SDK's dependency tree.
