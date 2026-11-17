# Keys and mouse

Every binding lives in one table and can be changed in the config file (see [config.md](config.md)).
`sltty keys` prints them all as a config file to start from; `?` shows them as configured.

Highlights: `j/k` move everywhere (chats, messages, thread, the lists of
`ctrl+k` / `ctrl+f` / `r` / `T`), `h/l` panes, `enter` opens a thread (on Telegram it
replies to the message, quoting it in the chat; `t` opens its thread), `R`
shows who reacted (tabs by emoji), `r` reacts (the usual few; typing finds any of Slack's
emoji and the workspace's own, as in slk), `o` lists the message's links and opens one with a hint key (`a s d f…`
or `1`-`9`, no mouse needed; it uses `open` / `xdg-open`), `v` opens the message's picture
full screen, `e` edits your message (`↑` in an empty composer
edits your last one), `u` toggles read / unread (on a message: unread from
there on; on a thread or an activity entry: just that one; `U` toggles the
sidebar between unread chats and all chats (the switcher still includes all
chats); what `u` marks unread stays so until you leave the chat and enter it again: `j/k` in the
sidebar only previews a chat, `enter`, `l` or a click enters it). On Telegram
`u` marks the whole chat unread on the server, as Telegram's "Mark as unread"
does, and it shows so on every device until read; on Slack it is local to
sltty (Slack's API cannot move the read cursor back), `ctrl+f` search,
`/` find (chats from the sidebar, messages elsewhere), `i` INSERT.

Files: drop one on the terminal (Ghostty, kitty, iTerm2 and WezTerm paste
its path) or `ctrl+v` while writing (a picture in the clipboard, a
screenshot, or a file copied in the Finder; with neither it pastes the
text as always), or `:attach [path]`. The composer shows `📎 name`, `enter`
sends it with what you wrote as its caption (on Telegram a picture goes as
a photo, anything else as a file; `enter` on a message to reply works
too), `:detach` drops it. Up to 50 MB; reading pictures from the
clipboard on Linux takes `wl-paste` or `xclip`.

`ctrl+k` or `ctrl+p` switches conversation and `ctrl+f` searches messages;
`/` is the switcher when the sidebar has the focus and the search elsewhere,
both in a box in the middle of the screen (slk's finder look). In
the switcher `#` keeps the channels and `@` the people; the search takes
Slack's `in:#channel` / `in:@person` and `from:@person`, highlights the words
and shows the time on the right. A click picks, the wheel scrolls, a click
outside closes.

Accounts (with Slack and Telegram signed in; `?` lists them under
*Accounts*): `1`…`9` open that tab in NORMAL mode and `alt+1`…`alt+9` from
anywhere, even while writing (actions `account_1`…`account_9`),
`g t` / `g T` the next / previous one (`next_account`, `previous_account`);
a click on a tab opens it. Bindings with `alt+` or `ctrl+` work while
writing or with a list open; plain keys only in NORMAL mode with nothing
open over the panes (there `1`…`9` still pick links in `o`'s list).

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

This is the complete reference for the defaults printed by `sltty keys`. The
**Action ID** is the name to use under `keys` in the config file. Bindings are
scoped: a key in **NORMAL** can mean something different while typing in
**INSERT** or in a list's **FILTER**.

| Area | Mode | Action ID | Default keys | Effect |
| --- | --- | --- | --- | --- |
| Move around | NORMAL | `focus_left` | `h`, `left`, `shift+tab` | pane on the left |
| Move around | NORMAL | `focus_right` | `l`, `right` | pane on the right |
| Move around | NORMAL | `toggle_sidebar_chat` | `tab` | sidebar ⇄ chat |
| Move around | NORMAL | `down` | `j`, `down` | down |
| Move around | NORMAL | `up` | `k`, `up` | up |
| Move around | NORMAL | `page_down` | `ctrl+d` | five down |
| Move around | NORMAL | `page_up` | `ctrl+u` | five up |
| Move around | NORMAL | `top` | `g g`, `home` | first |
| Move around | NORMAL | `bottom` | `G`, `end` | last |
| Move around | NORMAL | `next_chat` | `J` | next chat, from any pane |
| Move around | NORMAL | `prev_chat` | `K` | previous chat |
| Move around | NORMAL | `switcher` | `ctrl+k`, `ctrl+p` | jump to a conversation |
| Move around | NORMAL | `search` | `ctrl+f` | search messages |
| Move around | NORMAL | `filter` | `/` | find chats in the sidebar, messages elsewhere |
| Move around | NORMAL | `next_match` | `n` | next search result (older) |
| Move around | NORMAL | `prev_match` | `N` | previous search result |
| Move around | NORMAL | `toggle_section` | `space`, `z a` | fold / unfold the sidebar section |
| Move around | NORMAL | `toggle_unread_sidebar` | `U` | sidebar: unread chats / all chats |
| Messages | NORMAL | `open` | `enter` | open the chat / the thread (Telegram: reply to the message) |
| Messages | NORMAL | `thread` | `t` | open the thread (Telegram: its replies, a post's comments) |
| Messages | NORMAL | `reply` | — | reply in the thread |
| Messages | NORMAL | `react` | `r` | add or remove a reaction |
| Messages | NORMAL | `reactions` | `R` | show who reacted |
| Messages | NORMAL | `edit` | `e` | edit your message |
| Messages | NORMAL | `toggle_read` | `u` | mark read / unread from here |
| Messages | NORMAL | `links` | `o` | list the message's links |
| Messages | NORMAL | `view_picture` | `v` | view the message's picture full size |
| Messages | NORMAL | `insert` | `i`, `a` | write (INSERT mode) |
| Messages | NORMAL | `back` | `esc`, `x` | back, close |
| INSERT mode | INSERT | `send` | `enter` | send / save the edit |
| INSERT mode | INSERT | `newline` | `shift+enter`, `alt+enter`, `ctrl+j` | new line |
| INSERT mode | INSERT | `normal_mode` | `esc`, `ctrl+c`, `j k` | back to NORMAL |
| INSERT mode | INSERT | `edit_last` | `up` | edit your last message |
| INSERT mode | INSERT | `paste_image` | `ctrl+v` | attach the clipboard's picture or copied file (else paste its text) |
| Lists | FILTER | `picker_down` | `ctrl+n`, `ctrl+j`, `down` | next while filtering |
| Lists | FILTER | `picker_up` | `ctrl+p`, `ctrl+k`, `up` | previous while filtering |
| Lists | FILTER | `picker_select` | `enter` | pick |
| Lists | FILTER | `search_scope` | `tab` | search here ⇄ everywhere |
| Layout | NORMAL | `divider_left` | `<` | move the divider left |
| Layout | NORMAL | `divider_right` | `>` | move the divider right |
| Layout | NORMAL | `divider_reset` | `=` | reset the widths |
| Layout | NORMAL | `theme` | `T` | pick a theme |
| Layout | NORMAL | `layout` | `L` | slack / panels layout |
| Layout | NORMAL | `images_toggle` | `I` | images and custom emoji on / off |
| Layout | NORMAL | `help` | `?` | this help |
| Layout | NORMAL | `settings` | `;` | settings: theme, layout, clock, images, sizes |
| Layout | NORMAL | `status` | `ctrl+s` | set active, away, or Do Not Disturb |
| Layout | NORMAL | `command` | `:` | command line (`:help`, `:find`, `:version`…) |
| Layout | NORMAL | `composer_taller` | `+` | taller composer |
| Layout | NORMAL | `composer_shorter` | `-` | shorter composer |
| Layout | NORMAL | `back_or_quit` | `q`, `ctrl+c` | back step by step, then quit |
| Layout | NORMAL | `quit` | — | quit from anywhere (asks first) |
| Layout | NORMAL | `redraw` | `ctrl+l` | repaint the whole screen |
| Accounts | NORMAL | `next_account` | `g t` | next account tab |
| Accounts | NORMAL | `previous_account` | `g T` | previous account tab |
| Accounts | NORMAL | `account_1` … `account_9` | `1` … `9`, `alt+1` … `alt+9` | account tab 1 … 9 (`alt+` also while writing) |

`—` means the action is configurable and therefore appears in `sltty keys`,
but has no default key.
