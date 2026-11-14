# Security policy

## Reporting a vulnerability

Please do **not** open a public issue for security problems. Use GitHub's
private vulnerability reporting ("Security" tab → "Report a vulnerability") on
this repository. You will get an answer within a few days.

## What matters here

sltty handles a Slack session (the `d` cookie and an `xoxc` token), which is
as good as a password. Reports about any of these are especially welcome:

- the session leaking (logs, temp files, requests to hosts other than
  Slack's, plain-text HTTP);
- untrusted Slack content (messages, names, titles, files) reaching the
  terminal in a way that drives it (escape sequences) or crashes the client;
- the login flows reading more than they say they read.

The threat model and what the code does about each point is in
[docs/slack-session.md](docs/slack-session.md).

## Supported versions

Only the latest commit on `main`.
