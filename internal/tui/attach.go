package tui

// Attachments: a file goes with the next message of a composer. It gets
// there by dropping it on the terminal (which pastes its path), by pasting
// a picture from the clipboard (paste_image, ctrl+v while writing), or with
// :attach [path]. enter sends it with the text as its caption; :detach
// drops it.

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/diag"
)

const (
	// maxUpload is the largest file sltty sends; both services take more,
	// but it is read whole into memory.
	maxUpload = 50 << 20
	// uploadTimeout bounds sending a file, beyond the usual call timeout.
	uploadTimeout = 5 * time.Minute
)

// clipboardMsg is what the clipboard had for the composer key: a picture,
// or a file copied in the file manager (its path).
type clipboardMsg struct {
	key  string
	path string
	data []byte
	err  error
}

// attachKey is the composer a file attaches to: the thread's or the
// conversation's.
func (m *model) attachKey(in *textarea.Model) string {
	if in == &m.threadIn && m.thread != nil {
		return threadKey(m.thread.convID, m.thread.root)
	}
	if c := m.current(); c != nil {
		return c.ID
	}
	return ""
}

func (m *model) canUpload() bool {
	_, ok := m.backend.(chat.Uploader)
	if !ok {
		m.err = m.serviceName() + " cannot send files from sltty"
	}
	return ok
}

// attachFile reads path for the composer being written in (the open
// conversation's when none is).
func (m *model) attachFile(path string) error {
	if !m.canUpload() {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%s is not a file", path)
	}
	if st.Size() > maxUpload {
		return fmt.Errorf("%s is %s: at most %s", filepath.Base(path), humanBytes(int(st.Size())), humanBytes(maxUpload))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m.attachData(m.attachKey(m.activeInput()), filepath.Base(path), b)
	return nil
}

func (m *model) attachData(key, name string, data []byte) {
	if key == "" {
		return
	}
	m.attach[key] = &chat.Upload{Name: name, Data: data}
	m.status = "📎 " + name + " attached · " + m.km.first(actSend) + " sends it · :detach drops it"
}

func (m *model) detach() {
	key := m.attachKey(m.activeInput())
	if m.attach[key] == nil {
		m.status = "nothing attached"
		return
	}
	delete(m.attach, key)
	m.status = "attachment dropped"
}

// pastedPath is the file a paste names, when it is one: what terminals
// paste when a file is dropped on them (quoted, with escaped spaces, or as
// a file:// URL).
func pastedPath(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, "\n\r") {
		return "", false
	}
	if len(s) > 1 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	} else if u, ok := strings.CutPrefix(s, "file://"); ok {
		p, err := url.PathUnescape(u)
		if err != nil {
			return "", false
		}
		s = p
	} else if strings.Contains(s, `\`) {
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] == '\\' && i+1 < len(s) {
				i++
			}
			b.WriteByte(s[i])
		}
		s = b.String()
	}
	if rest, ok := strings.CutPrefix(s, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			s = filepath.Join(home, rest)
		}
	}
	if !filepath.IsAbs(s) {
		return "", false
	}
	if st, err := os.Stat(s); err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	return s, true
}

// unframePaste removes the left gutter added when copying a rendered terminal
// message. A heading is sometimes outside the gutter, so a strong majority
// rather than every line must have it. This avoids treating a lone ▌ in normal
// prose or code as UI chrome.
func unframePaste(content string) string {
	lines := strings.Split(content, "\n")
	var contentLines, guttered int
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" {
			continue
		}
		contentLines++
		if strings.HasPrefix(line, "▌") {
			guttered++
		}
	}
	if guttered < 2 || guttered*2 < contentLines {
		return content
	}
	return cleanCopiedText(content)
}

// cleanCopiedText removes terminal-only rendering from a known sltty message
// selection. Unlike unframePaste it is deliberately unconditional: a drag can
// span normal messages and attachment cards, so gutters are not a majority.
func cleanCopiedText(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		cr := strings.HasSuffix(line, "\r")
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			line = ""
		} else if rest, ok := strings.CutPrefix(line, "▌"); ok {
			line = strings.TrimLeft(rest, " \t")
		}
		lines[i] = stripTerminalImagePlaceholders(line)
		if cr {
			lines[i] += "\r"
		}
	}
	return strings.Join(lines, "\n")
}

func isPrivateUse(r rune) bool {
	return r >= 0xE000 && r <= 0xF8FF || r >= 0xF0000 && r <= 0xFFFFD || r >= 0x100000 && r <= 0x10FFFD
}

// stripTerminalImagePlaceholders removes Kitty's private-use image cells and
// their combining Unicode markers, which leak into a terminal text selection.
func stripTerminalImagePlaceholders(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		start := i
		hasPrivateUse := false
		for i < len(s) {
			r, n := utf8.DecodeRuneInString(s[i:])
			if !isPrivateUse(r) && !unicode.IsMark(r) {
				break
			}
			hasPrivateUse = hasPrivateUse || isPrivateUse(r)
			i += n
		}
		if i == start {
			_, n := utf8.DecodeRuneInString(s[i:])
			out.WriteString(s[i : i+n])
			i += n
		} else if !hasPrivateUse {
			out.WriteString(s[start:i])
		}
	}
	return out.String()
}

// pasted takes a paste that names a file as an attachment; ok is false for
// any other paste, which is text.
func (m *model) pasted(content string) (tea.Cmd, bool) {
	diag.Printf("tui: paste of %d bytes", len(content))
	// An empty paste: the terminal had no text to paste (a picture alone in
	// the clipboard, with cmd+v). The picture is attached.
	if strings.TrimSpace(content) == "" && m.picker == nil && m.cmdline == nil && m.view == sideConv && m.current() != nil {
		if _, can := m.backend.(chat.Uploader); can {
			var cmd tea.Cmd
			if m.mode != insert {
				if m.focus == paneSide {
					m.focus = paneChat
				}
				cmd = m.enterInsert()
			}
			return tea.Batch(cmd, m.pasteImage()), true
		}
	}
	path, ok := pastedPath(content)
	if !ok || m.picker != nil || m.cmdline != nil || m.view != sideConv {
		return nil, false
	}
	if _, can := m.backend.(chat.Uploader); !can {
		return nil, false
	}
	if m.mode != insert && m.focus == paneSide {
		m.focus = paneChat
	}
	var cmd tea.Cmd
	if m.mode != insert {
		cmd = m.enterInsert()
	}
	if err := m.attachFile(path); err != nil {
		m.err = err.Error()
	}
	return cmd, true
}

// pasteImage reads a picture from the clipboard, off the UI loop.
func (m *model) pasteImage() tea.Cmd {
	if !m.canUpload() {
		return nil
	}
	key := m.attachKey(m.activeInput())
	m.status = "reading the clipboard…"
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		path, b, err := clipboardFile(ctx)
		return clipboardMsg{key, path, b, err}
	}
}

// gotClipboard attaches what the clipboard had. With neither a picture nor
// a file in it, paste_image is the usual paste: its text.
func (m *model) gotClipboard(msg clipboardMsg) tea.Cmd {
	m.status = ""
	switch {
	case msg.err != nil:
		if m.mode == insert {
			return textarea.Paste
		}
		m.err = msg.err.Error()
	case msg.path != "":
		if err := m.attachFile(msg.path); err != nil {
			m.err = err.Error()
		}
	case len(msg.data) > maxUpload:
		m.err = "the clipboard's picture is too big to send"
	default:
		m.attachData(msg.key, "pasted "+m.now().Format("2006-01-02 15.04.05")+".png", msg.data)
	}
	return nil
}

var errNoClipboardImage = errors.New("no picture in the clipboard")

// macClipboard is a file copied in the Finder ("file:" and its path) or
// else the clipboard's picture as PNG. It asks what the clipboard holds
// first: any text would read as a file's path, and any picture (TIFF from
// some apps) converts to PNG.
const macClipboard = `if (clipboard info for «class furl») is not {} then
	return "file:" & POSIX path of (the clipboard as «class furl»)
end if
if (clipboard info for «class PNGf») is not {} or (clipboard info for «class TIFF») is not {} then
	return the clipboard as «class PNGf»
end if
error "no picture"`

// clipboardFile is a file copied in the file manager (its path), or the
// clipboard's picture as PNG: osascript on macOS, wl-paste or xclip
// elsewhere. Tests replace it.
var clipboardFile = func(ctx context.Context) (string, []byte, error) {
	if runtime.GOOS == "darwin" {
		out, err := exec.CommandContext(ctx, "osascript", "-e", macClipboard).Output()
		if err != nil {
			return "", nil, errNoClipboardImage
		}
		if p, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "file:"); ok {
			if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
				return "", nil, errNoClipboardImage // a folder, or gone
			}
			return p, nil, nil
		}
		b, err := parseAppleData(string(out))
		return "", b, err
	}
	for _, c := range [][]string{{"wl-paste", "--no-newline", "--type", "image/png"}, {"xclip", "-selection", "clipboard", "-t", "image/png", "-o"}} {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		if out, err := exec.CommandContext(ctx, c[0], c[1:]...).Output(); err == nil && len(out) > 0 {
			return "", out, nil
		}
		return "", nil, errNoClipboardImage
	}
	return "", nil, errors.New("reading pictures from the clipboard needs wl-paste or xclip")
}

// parseAppleData reads AppleScript's «data PNGf89504E…» form.
func parseAppleData(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	h, ok := strings.CutPrefix(s, "«data PNGf")
	if !ok || !strings.HasSuffix(h, "»") {
		return nil, errNoClipboardImage
	}
	b, err := hex.DecodeString(strings.TrimSuffix(h, "»"))
	if err != nil || len(b) == 0 {
		return nil, errNoClipboardImage
	}
	return b, nil
}

// upload sends f with text, in a call allowed to take longer than others.
func (m *model) upload(id, root, text string, f chat.Upload) tea.Cmd {
	up := m.backend.(chat.Uploader)
	m.status = "sending " + f.Name + " (" + humanBytes(len(f.Data)) + ")…"
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
		defer cancel()
		msg, err := up.Upload(ctx, id, root, text, f)
		return sentMsg{id, root, msg, err}
	}
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}
