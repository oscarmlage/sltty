package chat

import (
	"html"
	"regexp"
	"strings"
)

// Everything the chat service sends is untrusted text that ends up on the user's
// terminal. A raw ESC (or any other C0/C1 control) in a message, a name or a
// file title could drive the terminal: write to the clipboard (OSC 52), set
// the window title, forge what is on screen... and the TUI itself reserves
// NUL as an internal marker. JSON carries such characters as \uXXXX escapes,
// so they are neutralised in the raw response, before it is decoded.
var controlEscape = regexp.MustCompile(`(?i)\\u(00[01][0-9a-f]|007f|008[0-9a-f]|009[0-9a-f])`)

// StripControl replaces JSON-escaped control characters (all but \t and \n)
// with U+FFFD.
func StripControl(body []byte) []byte {
	return controlEscape.ReplaceAllFunc(body, func(m []byte) []byte {
		switch string(m[2:]) {
		case "0009", "000a", "000A":
			return m
		}
		return []byte(`\ufffd`)
	})
}

// Sanitize does the same on decoded text: controls other than \t and \n
// become U+FFFD. The demo data and anything not read through Slack's JSON
// should pass through it.
func Sanitize(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r != '\t' && r != '\n' && (r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)) {
			out[i] = '\uFFFD'
		}
	}
	return string(out)
}

// unescape decodes HTML entities (&lt;, &#27;...) and then sanitizes: an
// entity must not smuggle back a control character.
func unescape(s string) string { return Sanitize(html.UnescapeString(s)) }

// safeLink keeps web links only: a message must not get the terminal to
// open file:, javascript: or other schemes.
func safeLink(u string) string {
	if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
		return u
	}
	return ""
}
