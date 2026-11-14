# Architecture

```text
cmd/sltty            flag parsing and sub-commands (login, logout, keys, config)
internal/chat        the Backend interface, the in-memory Fake and the Slack backend
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
with made-up data (demo mode); `Slack` implements it with Slack's web API.

| File | Contents |
| --- | --- |
| `chat.go` | `Backend` and the data types |
| `slack.go` | `Slack`, credentials, the HTTP client and `call` (every API request) |
| `slack_messages.go` | wire types, conversion to `Message`, history, send, edit, react, search |
| `slack_users.go` | people and bots, names, avatars, presence |
| `slack_conversations.go` | the sidebar: conversations, native sections, unread counts |
| `slack_feeds.go` | Threads and Activity |
| `slack_files.go` | pictures and custom emoji (the only place that downloads files) |
| `sanitize.go` | control-character stripping for everything Slack sends |
| `session_store.go` | where the session lives, load/save/logout |
| `login_browser.go` | `login --browser` (Chrome + DevTools Protocol) |
| `session_desktop_*.go` | `login --desktop` (macOS only) |

## tui

| File | Contents |
| --- | --- |
| `model.go` | the `model` struct, `Update` and message handling |
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
