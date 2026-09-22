# daybook

A local macOS service that reads every AI-coding session you run — Claude Code,
Codex, and whatever comes next — and turns it into daily and weekly digests of
what you actually did: the systems, the numbers, the tech, the facts.

Yours, on your disk, summarised by any LLM you choose. Local Ollama by default;
cloud providers are one config block away, and nothing unscrubbed ever leaves
the machine.

> Status: pre-alpha. See [docs/PRD.html](docs/PRD.html) and
> [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Why

If you work through an agent all day, its transcript *is* your work log — but
it sits as gigabytes of JSONL under `~/.claude/projects` that nobody reads back.
daybook watches those files, summarises each session as it closes, and rolls
sessions into a **Today** page, a **Week** page, and a running **ledger** of
tech terms, numbers and hosts you touched — the raw material for a resume, a
weekly update, or a build-in-public post.

## Install

```sh
brew install kaushal07wick/tap/daybook   # soon
daybook install                          # writes + starts the launchd agent
daybook backfill                         # ingest + summarise history (resumable)
open http://127.0.0.1:7331
```

With no config, daybook probes `localhost:11434` and uses Ollama if present.

## Configure

`~/.config/daybook/config.toml`:

```toml
default_provider = "local"

[providers.local]
type     = "openai"                      # openai-compatible chat completions
base_url = "http://127.0.0.1:11434/v1"
model    = "qwen2.5:7b"
cloud    = false

[providers.claude]
type        = "anthropic"
model       = "claude-sonnet-5"
api_key_env = "ANTHROPIC_API_KEY"
cloud       = true                       # => scrubbed before sending, always

[privacy]
redact = ["acme-corp", "10.10.0.206"]    # your own denylist, on top of built-ins
```

## Commands

| command | does |
|---|---|
| `daybook install` / `uninstall` | manage the launchd agent |
| `daybook serve` | run the watcher + summariser + web UI in the foreground |
| `daybook backfill [--no-llm]` | ingest everything on disk; summarise oldest-first |
| `daybook digest --day 2026-09-21 [--force]` | (re)generate a digest |
| `daybook status [--json]` | queue depth, last ingest, provider health |
| `daybook export --week 2026-W38 --format md\|json` | export a digest or the ledger |

## Privacy

Providers marked `cloud = true` only ever see text that has passed through
`scrub()`: IPs, emails, key-shaped strings, `password=` patterns, private-key
blocks, and your `redact` list are replaced with stable placeholders. There is
no flag to turn this off. Local providers receive raw text.

## Development

```sh
make lint test build
```

Go 1.23+, no cgo. Sources live in `internal/source/<name>`; adding an agent is
one package implementing `source.Source`.

## License

MIT
