# Slack session and security model

> **Experimental and unofficial.** sltty talks to Slack with the same browser
> session the Slack web client uses (the `d` cookie and an `xoxc` token). That
> is not a documented public API: it may violate Slack's terms, may stop
> working when Slack changes its web client, and your workspace's admins may
> not allow it. Use it on workspaces where that is acceptable, at your own
> risk. It is not a corporate deployment tool. The TUI only knows
> `chat.Backend`, so an approved OAuth app could replace this backend later.

Without a saved session sltty runs in demo mode (made-up data, no network).
`sltty -demo` forces demo mode even when a session exists.

## Logging in

```text
sltty login             # same as --browser
sltty login --browser
sltty login --desktop   # macOS only
sltty logout
```

### `--browser` (default)

1. Starts Chrome/Chromium/Brave/Edge with a **new temporary profile** and a
   DevTools port bound to `127.0.0.1` on a random free port.
2. You sign in as usual (password, SSO, MFA happen in the browser; sltty never
   sees them).
3. sltty reads, over that local DevTools port, the `d` cookie of `slack.com` and the
   `xoxc` token from the page's local storage (falling back to Slack's
   authenticated boot page), validates them with `auth.test`, and kills the
   browser and deletes the profile.

The DevTools port accepts connections from local processes while the login
window is open (up to five minutes). Do the login on a machine you trust.

### `--desktop` (macOS)

Imports the active Slack Desktop session without opening a browser. It:

- copies Slack's `Cookies` database and `Local Storage/leveldb` to a private
  (`0700`) temporary directory and removes it afterwards;
- runs `/usr/bin/sqlite3` on that copy to read the `d` cookie;
- asks the macOS Keychain for `Slack Safe Storage` (macOS shows its usual
  prompt) to decrypt the cookie;
- reads the workspace tokens from `localConfig_v2`.

It reads nothing else and sends nothing anywhere except Slack's API. Because
it reads another application's credentials, only use it for your own account,
and read the code (`internal/chat/session_desktop_darwin.go`) before you run it.

## Where the session lives

`slack-session.json` in the platform config directory (on macOS
`~/Library/Application Support/sltty/`, on Linux `~/.config/sltty/`; the path is printed
after login), directory `0700`, file `0600` (tightened at load if it was looser).
It is **not encrypted**. Treat it as a password: do not commit it, copy it or paste it
in issues. `sltty logout` deletes it. Signing out of Slack (or revoking the
session in Slack's account settings) invalidates it.

Other files: `state.json` (theme, layout, widths: no secrets) and the log at
the user cache directory (`sltty.log`, truncated past 1 MiB at launch). The
log only records API method names and result statuses, never tokens, cookies,
message text or URLs.

## What sltty does about untrusted content

Everything that comes from Slack is attacker-controlled (any member of a
channel can write to you):

- **Terminal escape sequences.** Control characters (C0, C1, DEL; not `\n` or
  `\t`) are stripped from every API response, and again after HTML entities
  are decoded (`&#27;`), so a message cannot write to your clipboard (OSC 52),
  retitle the window or forge the screen.
- **Internal markers.** The renderer's own NUL marker is removed from text,
  so crafted text cannot crash it.
- **Links** are only turned into terminal hyperlinks for `http(s)` and
  `mailto:` targets.
- **Network.** The session is only ever sent to Slack's API host (taken from
  `auth.test`, must be `https` under `slack.com`) and to `*.slack.com` file
  URLs over `https`; redirects must stay on `https` and are limited to five.
  Pictures are limited to 16 MiB, API responses to 8 MiB, and are kept in
  memory only.
- **Images** may show sensitive data from your workspace; `I` (or `"images":
  "off"`) turns them off.

## Known limits

- The session file is not stored in the OS keychain.
- `--desktop` and `--browser` temporary directories are removed on exit, but a
  hard kill (`kill -9`, power loss) can leave them in the system temp
  directory until the next cleanup.
- Anyone who can run code as your user can read the session file.

Found something? See [SECURITY.md](../SECURITY.md).
