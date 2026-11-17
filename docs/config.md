# Configuration and features

Everything here is optional. `sltty config` prints what the config file takes.

## Config file

`~/.config/sltty/config.json` (or `-config`), all optional:

```json
{
  "theme": "nord",
  "layout": "slack",
  "clock": "12h",
  "keys": { "react": ["r", "*"], "normal_mode": ["esc", "ctrl+c", "j j"] },
  "themes": { "mine": { "base": "dracula", "primary": "#ff79c6" } }
}
```

Every keybinding is editable under `keys`. Its object keys are action IDs:
in the example, `react` accepts `r` or `*`, while `normal_mode` makes `j j`
another way to leave INSERT. Each array replaces that action's defaults. Run
`sltty keys` to print the complete starter mapping. Unknown actions, two
actions on one key, and bad colors are errors at start-up.

## Accounts and tabs

Slack and Telegram run side by side in one sltty, each account in a tab.
Every service with a saved session opens (`sltty login` for Slack,
`sltty login telegram` for Telegram); `"accounts": ["telegram", "slack"]`
in the config chooses which and in what order. With a single account there
is no tab bar.

The tab bar on top shows each account and its unread count (muted chats
aside): the active tab filled with its theme's main color, the others in
their own sidebar color. `1`…`9` open a tab (`alt+1`…`alt+9` also while writing), `g t` / `g T` go to the
next / previous one, and a click on a tab opens it. Accounts in the
background keep working: Telegram's live updates and every unread count
stay current.

Each account has its own theme and layout: pick one with `T` (or `;`)
while in its tab and it is kept for that account. Without one saved, an
account takes the config's `theme`, and Telegram the `telegram` theme when
the config sets none. The other settings (clock, images, sizes, reading,
history cache) are one set for all the accounts. Searches, the switcher, drafts, sections
and the history cache belong to each account: `ctrl+f` searches the
account in front. `sections` in the config apply to Slack; Telegram keeps
its own folders (Archived starts folded).

## Command line, settings, help

`:` opens a command line in the status bar, slk style (`COMMAND` pill; tab
completes, ↑/↓ walk the history, `esc`/`ctrl+c`/`jk` leave): `:help`,
`:version`, `:find [name]` (one match jumps there), `:search [words]`,
`:settings`, `:config`, `:theme [name]`, `:layout [slack|panels]`,
`:clock [12h|24h]`, `:images [on|off]`, `:status` (or `ctrl+s`: active, away, Do Not Disturb), `:away`, `:active`, `:read`, `:read_on_enter [on|off]`, `:fold [section]`,
`:log`, `:save` (`:w`), `:savequit` (`:wq`) and `:quit` (`:q`).

`;` (or `:settings`) is the settings panel: theme, layout, clock (15:04 or
3:04 PM), images, custom emoji, marking messages read on entry, composer height and sidebar width (`h`/`l` change them, live), and the
way to the keybindings and to the config reference; it shows the config
lines that keep what you changed. `?` lists keys in compact category tabs;
`h/l` changes tabs and `/` searches all of them. `sltty config` prints what
the config file takes.

The status bar ends with `● Active`, `● Away` or `◐ Snoozed` (`:status`,
`:away`, `:active`) and `● Connected`, which turns `Disconnected` when a
backend call fails.

## Sidebar

Threads (threads you are in, with unread replies) and Activity (mentions,
replies, reactions to your messages) sit on top; `j/k` walks into them like
any chat, `enter` opens a thread on the right or jumps to the message.
Private channels show 🔒 (`private_icon` changes it, e.g. `◆`). Custom
sections come from the config, above Channels and Direct messages:

```json
"sections": [
  { "name": "Team",  "channels": ["tools-*", "@ana"] },
  { "name": "Noise", "channels": ["deploys", "random"], "collapsed": true },
  { "name": "Channels", "collapsed": true }
]
```

Patterns are globs, `@name` for DMs; a chat goes to the first section that
matches, in the order of its patterns. Your Slack sections come along too,
with their emoji. In the sidebar `j/k` stop on section headers as well;
`space` or `enter` on one (or `space` / `z a` from inside, or a click on the
header) folds it, the way slk does: folded, it only shows the open chat, and
the header counts the unread messages it hides. An entry named `Channels` or `Direct messages` with no
patterns only sets whether that built-in section starts folded.

## Messages

`history_cache_ttl` is an opt-in local cache of the last 100 messages opened
in each channel. It is in minutes (`0` disables it); expired entries are deleted.
The Settings panel can toggle it and choose its lifetime. Cached messages live
under the user cache directory with private permissions, and Slack refreshes
them after they are shown.

Messages read as in Slack: from their blocks when they have them (lists,
quotes, warm code chips, the small print under an app's message and
non-interactive action labels such as `[ Resolve ]`; a button that only opens a
URL, such as a bot's `[ Authenticate ]`, is a link to it), mentions by name,
links by their label (a click on one opens it, as `o` does), and bot
messages under their profile's display name rather than a technical handle.

## Images and custom emoji

Attachments show as pictures (side by side while they fit the width),
people's avatars next to every message (apps' and bots' too: their icon, or
a webhook's emoji) and the workspace's `:emoji:` (in
messages, reactions, the reaction picker, Activity and section headers) as
small images, the way slk does it: kitty graphics with unicode placeholders where the
terminal has them (kitty, Ghostty, WezTerm), half blocks (`▀`, two colors a
cell) elsewhere. Custom emoji need kitty; elsewhere they stay `:name:`. The
standard ones (`:tada:`, `:+1::skin-tone-3:`) are always plain emoji. `I`
turns pictures off and on. Pictures take the terminal's cell shape (it is
asked, CSI 16 t), so they leave no blank around them.

A click on a picture (or `v` on its message) shows it full screen, at its
original resolution (the thumbnail when that cannot be read, HEIC for one):
`h`/`l` (or `j`/`k`, the wheel) go through the message's pictures, `esc`, a
click or `v` go back.

```json
"images": "auto",        // "kitty", "halfblock" or "off"
"custom_emoji": true,
"image_width": 40,       // cells
"image_height": 14
```

In tmux, `auto` uses half blocks; `"images": "kitty"` works with
`set -g allow-passthrough on`. Pictures are fetched when a conversation shows
them (Slack's thumbnails; the original only full screen), shrunk to what the cells can show and kept in memory only (64 MB at
most), never on disk; kitty gets them directly through the terminal (no
temporary files) and they are freed from it on quit. The demo draws its
pictures in memory; with a real workspace, screenshots can hold sensitive
data, so check with Security before turning them on there.
