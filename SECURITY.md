# Security

daybook reads your AI-agent transcripts, which routinely contain credentials,
hostnames and customer data. The threat model is:

- **Local providers receive raw text.** That is by design; the data never
  leaves the machine.
- **Cloud providers receive scrubbed text only.** `internal/scrub` is the
  boundary. A pattern it misses is a security bug.
- **The web UI is loopback-only and unauthenticated.** Anything on your Mac
  can read it. Do not expose port 7331.
- **The database is plaintext SQLite** under `~/Library/Application Support`.
  Use FileVault.

## Reporting

Email the maintainer (address on the GitHub profile) rather than opening a
public issue for anything involving data leaving the machine. Expect a reply
within 72 hours. Scrub-pattern gaps can be reported as normal issues if the
example is synthetic.
