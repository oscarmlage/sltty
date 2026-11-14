# Keys and mouse

Every binding lives in one table and can be changed in the config file (see [config.md](config.md)).
`sltty keys` prints them all as a config file to start from; `?` shows them as configured.

Highlights: `j/k` move everywhere (chats, messages, thread, the lists of
`ctrl+k` / `ctrl+f` / `r` / `T`), `h/l` panes, `enter` opens a thread, `R`
shows who reacted (tabs by emoji), `r` reacts (the usual few; typing finds any of Slack's
emoji and the workspace's own, as in slk), `v` opens the message's picture
full screen, `e` edits your message (`↑` in an empty composer
edits your last one), `u` toggles read / unread (on a message: unread from
there on; on a thread or an activity entry: just that one; what `u` marks
unread stays so until you leave the chat and enter it again: `j/k` in the
sidebar only previews a chat, `enter`, `l` or a click enters it), `ctrl+f` search,
`/` find (chats from the sidebar, messages elsewhere), `i` INSERT.

`ctrl+k` or `ctrl+p` switches conversation and `ctrl+f` searches messages;
`/` is the switcher when the sidebar has the focus and the search elsewhere,
both in a box in the middle of the screen (slk's finder look). In
the switcher `#` keeps the channels and `@` the people; the search takes
Slack's `in:#channel` / `in:@person` and `from:@person`, highlights the words
and shows the time on the right. A click picks, the wheel scrolls, a click
outside closes.

Leaving INSERT without `esc` (iPad): `ctrl+c`, `ctrl+[` (it is esc to the
terminal) or typing `jk`. New line: `shift+enter` (terminals with the kitty
keyboard protocol: Ghostty, kitty, WezTerm, iTerm2 with CSI u), `alt+enter`
or `ctrl+j` (everywhere).

`ctrl+c` in NORMAL steps back (list, thread, conversation) and only from
the sidebar asks to quit; `q` asks too.

Mouse: drag the dividers between panes, drag the composer's top edge to make
it taller (`+`/`-` from the keyboard), drag across message or thread text to
copy it to the clipboard (OSC 52 terminal support required), click to select,
double-click a message to open its thread.

Lists are telescope-style: they open in the filter; `normal_mode` (esc,
ctrl+c, jk) moves to the list, where `j/k`, `gg/G`, `enter`, `i` (back to
the filter) and `esc` (close) work.

## Default keybindings

The most-used defaults are below. `?` shows the complete live list (including
list-filter bindings), while `sltty keys` prints every configurable action and
its defaults as JSON.

| Area | Keys | Action ID / effect |
| --- | --- | --- |
| Move | `h` / `l`, `j` / `k` | `focus_left` / `focus_right`, `down` / `up` |
| Move | `ctrl+k` or `ctrl+p` | `switcher` |
| Find | `ctrl+f`, `/`, `n` / `N` | `search`, `filter`, next / previous match |
| Messages | `enter`, `r`, `R`, `e`, `u`, `v` | open, react, reactions, edit, read/unread, view picture |
| Compose | `i` or `a`, `enter`, `shift+enter` | insert, send, new line |
| Compose | `esc`, `ctrl+c`, `j k` | `normal_mode` (leave INSERT) |
| Layout | `T`, `L`, `I`, `;`, `ctrl+s`, `+` / `-` | theme, layout, images, settings, status, composer size |
| General | `?`, `:`, `q` | keybindings, command line, back / quit |
