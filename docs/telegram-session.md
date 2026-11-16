# Telegram session and security model

sltty signs in to Telegram **as you** (a user account, not a bot) over
MTProto, with [gotd/td](https://github.com/gotd/td). That is Telegram's
public client API: third-party clients are allowed, but your use must follow
the [API terms of service](https://core.telegram.org/api/terms).

With a Telegram session saved, sltty opens it in its own tab next to Slack
(see [accounts](config.md#accounts-and-tabs)). `sltty -demo` ignores every
session.

## Logging in

```text
sltty login telegram            # QR code: scan it from your phone
sltty login telegram --phone    # phone number + the code Telegram sends
sltty logout telegram
```

**QR code (default).** sltty draws a QR code in the terminal; on your phone
open Telegram → *Settings → Devices → Link Desktop Device* and scan it, as
you would for Telegram Desktop. The code refreshes by itself when it
expires.

**Phone.** Your number in international format (`+34…`), then the code
Telegram sends to your other Telegram apps (or by SMS).

Either way, if the account has a two-step verification password it is asked
for (read without echo, never stored: only an SRP proof is sent).

### The application credentials

Every Telegram client identifies itself with an *api_id/api_hash*: they
name the program, not the user, and users never see them. sltty's are built
in (`internal/chat/telegram_app.go`, or `make build SLTTY_TELEGRAM_API_ID=…
SLTTY_TELEGRAM_API_HASH=…`). Only a build without them asks for a pair from
<https://my.telegram.org> (saved in `telegram-app.json`); `SLTTY_TELEGRAM_API_ID`
and `SLTTY_TELEGRAM_API_HASH` (or tgtty's `TGTTY_API_ID` and `TGTTY_API_HASH`)
in the environment override them.

`sltty logout telegram` ends the session on Telegram's side (it disappears from
*Settings → Devices*) and deletes the local file. Ending the session from
another device has the same effect on Telegram's side.

## Where the session lives

In the platform config directory (on macOS
`~/Library/Application Support/sltty/`, on Linux `~/.config/sltty/`),
directory `0700`, files `0600` (tightened at start if looser):

- `telegram-session.json`: the MTProto authorization key. It is **not
  encrypted** and is as good as a logged-in device: do not commit it, copy it
  or paste it in issues.
- `telegram-app.json`: the api_id/api_hash, only for builds without them.

A session left by tgtty (sltty's Telegram-only fork) in its own directory is
copied over the first time. Do not run tgtty and sltty with the same session
at the same time: one authorization key used by two programs at once can
make Telegram end it. `sltty login telegram` makes an independent one.

## Live updates

While sltty runs it receives Telegram's update stream (new, edited and
deleted messages, reactions, reads on other devices, presence) through
gotd's updates manager, which fills gaps with `updates.getDifference`.
Nothing is polled. Each history is loaded once the sidebar cursor rests on
it, and calls are paced under Telegram's limits (FLOOD_WAIT is waited out).

## Untrusted content

Everything Telegram sends (messages, names, titles, file names) is passed
through `chat.Sanitize`, which replaces control characters other than tab
and newline, so a message cannot drive the terminal. Formatted links keep
`http(s)` URLs only.

## What is not supported (yet)

Secret chats (end-to-end encrypted, device-bound), calls, sending files and
stickers, voice notes playback, editing folders. Messages are sent as plain
text.
