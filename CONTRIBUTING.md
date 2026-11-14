# Contributing

sltty is a small, personal project. Issues and pull requests are welcome.

    make check     # gofmt, go vet, tests: what CI runs
    make cover     # total coverage (make cover-html for the report)
    make run ARGS=-demo   # try the UI with built-in data, no Slack needed

## Ground rules

- Go version: whatever `go.mod` says.
- Keep `make check` green; new behavior comes with a test. The UI is tested
  through the model (`internal/tui/*_test.go`), without a terminal.
- **Never log or print secrets** (token, cookie). `internal/diag` is the only
  logger and only records method names and statuses.
- Everything that comes from Slack is untrusted text. It is sanitized at
  the boundary (`internal/chat/sanitize.go`); keep new code reading Slack data
  behind `Slack.call` so it gets the same treatment.
- Do not commit session files, recordings of a real workspace or screenshots
  with real names. Demo GIFs are recorded against `-demo` only (`make demo`, which films a
  kitty window so pictures show; see `scripts/record-demo.py`). Hands off the
  keyboard while it records.
- No code is copied from other clients (slk, msga); ideas are fine.

## Layout

See [docs/architecture.md](docs/architecture.md).
