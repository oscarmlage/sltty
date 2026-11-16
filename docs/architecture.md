# Architecture

```text
cmd/sltty            flag parsing and sub-commands (login, logout, keys, config)
internal/chat        the Backend interface, the in-memory Fake, the Slack and Telegram backends
internal/tui         the Bubble Tea model, views, pickers, keymap, themes, images
internal/config      the JSON config file and its reference
internal/uistate     small UI preferences (state.json), separate from config
internal/img         kitty graphics and half-block image encoding
internal/emoji       Slack emoji names (generated) and lookups
internal/diag        the secret-safe log
scripts              record-demo.py: records docs/assets/demo/*.tape as GIFs
docs/assets/demo     the tapes and GIFs used in the README
```

## chat

`chat.Backend` is all the TUI knows about a chat service. `Fake` implements it
with made-up data (demo mode); `Slack` implements it with Slack's web API and
`Telegram` with MTProto (gotd/td). What only some services do is an optional
interface the TUI looks for: `Live` (pushed events: Telegram), `Typer`
(typing notifications) and `Threadless` (no Threads view).

| File | Contents |
| --- | --- |
| `chat.go` | `Backend` and the data types |
| `slack.go` | `Slack`, credentials, the HTTP client and `call` (every API request) |
| `slack_messages.go` | wire types, conversion to `Message`, history, send, edit, react, search |
| `slack_users.go` | people and bots, names, avatars, presence |
| `slack_conversations.go` | the sidebar: conversations, native sections, unread counts |
| `slack_feeds.go` | Threads and Activity |
| `slack_files.go` | pictures and custom emoji (the only place that downloads files) |
| `telegram.go` | `Telegram`, app credentials, session paths, the client, the peer cache (access hashes) |
| `telegram_api.go` | the `Backend` methods: dialogs, history, replies, send, edit, react, search, activity, files |
| `telegram_convert.go` | Telegram messages to `Message`: entities to markup, media, reply quotes, reactions |
| `telegram_live.go` | the updates stream turned into `chat.Event`s (the `chat.Live` interface) |
| `telegram_limit.go` | pacing under Telegram's rate limits, FLOOD_WAIT |
| `telegram_login.go` | `sltty login telegram` (QR code or phone, 2FA) and `logout telegram` |
| `telegram_app.go` | the built-in api_id/api_hash |
| `sanitize.go` | control-character stripping for everything Slack sends |
| `session_store.go` | where the session lives, load/save/logout |
| `login_browser.go` | `login --browser` (Chrome + DevTools Protocol) |
| `session_desktop_*.go` | `login --desktop` (macOS only) |

## tui

| File | Contents |
| --- | --- |
| `app.go` | the accounts: one `model` per tab, the tab bar, routing each account's messages, shared settings |
| `model.go` | the `model` struct (one account), `Update` and message handling |
| `live.go` | a `chat.Live` backend's events: new and edited messages, reads, typing |
| `history_cache.go` | the opt-in on-disk history cache |
| `model_nav.go` | unread bookkeeping, conversations, threads, jumps, pane widths |
| `run.go` | `Run`, terminal setup, UI-state persistence |
| `keys.go`, `keymap.go` | key handling and the configurable key table |
| `picker*.go` | switcher, search, reactions, theme picker; `picker_view.go` draws them |
| `view.go`, `views.go`, `sidebar.go`, `messages.go`, `sheet.go`, `help.go` | rendering |
| `mrkdwn.go` | Slack's markup to styled terminal text |
| `images.go`, `viewer.go`, `gallery.go` | pictures |
| `theme.go`, `styles.go` | themes and layouts |
| `commands.go`, `settings.go` | the `:` command line and the settings panel |
| `mouse.go` | clicks, drags, selection |
