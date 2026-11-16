package tui

// Links: `o` lists the links of the selected message and opens one by its
// hint key, Vimium style, so no click is ever needed.

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/chat"
)

// hintKeys label the links: the first nine also answer to 1-9. Letters that
// navigate the list (j, k, h, l, g, q, o, r, t, x) are left out.
const hintKeys = "asdfewcmpuiyvbnz"

type link struct{ label, url string }

// openedMsg is the answer of the system opener.
type openedMsg struct{ err error }

// linkToken finds [label](url) and bare http(s) URLs, in reading order.
var linkToken = regexp.MustCompile(`\[([^\[\]]+)\]\((https?://[^)\s]+)\)|https?://[^\s<>()\[\]]+`)

// messageLinks are the distinct http(s) links of a message: in its text,
// the small print and its attachment cards.
func messageLinks(msg *chat.Message) []link {
	var out []link
	seen := map[string]bool{}
	add := func(label, url string) {
		url = strings.TrimRight(url, ".,;:!?'\"")
		if url == "" || seen[url] || !isWebURL(url) {
			return
		}
		seen[url] = true
		if label == "" {
			label = url
		}
		out = append(out, link{label, url})
	}
	scan := func(s string) {
		for _, m := range linkToken.FindAllStringSubmatch(s, -1) {
			if m[2] != "" {
				add(m[1], m[2])
			} else {
				add("", m[0])
			}
		}
	}
	scan(msg.Text)
	for _, c := range msg.Context {
		scan(c)
	}
	for _, a := range msg.Attachments {
		add(a.Title, a.TitleURL)
		scan(a.Pretext)
		scan(a.Text)
		for _, f := range a.Fields {
			scan(f.Value)
		}
	}
	return out
}

func isWebURL(u string) bool {
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
}

// openLinks is `o`: the links of msg in a sheet, each with its hint key.
func (m *model) openLinks(msg *chat.Message) {
	if msg == nil {
		return
	}
	links := messageLinks(msg)
	if len(links) == 0 {
		m.status = "no links in this message"
		return
	}
	links = links[:min(len(links), len(hintKeys))]
	rows := make([][2]string, len(links))
	urls := make([]string, len(links))
	for i, l := range links {
		hint := string(hintKeys[i])
		if i < 9 {
			hint += " " + fmt.Sprint(i+1)
		}
		text := l.url
		if l.label != l.url {
			text = l.label + "  " + dimStyle.Render(l.url)
		}
		rows[i] = [2]string{hint, text}
		urls[i] = l.url
	}
	m.sheet = newSheet("Links", "press a key to open it · enter opens the selected one", rows)
	m.sheet.links = urls
	m.help = true
}

// linkKey opens the link a hint key (or enter, on the cursor) stands for.
func (m *model) linkKey(k string, a action) (tea.Cmd, bool) {
	s := m.sheet
	i := -1
	switch {
	case len(k) == 1 && k >= "1" && k <= "9":
		i = int(k[0] - '1')
	case len(k) == 1 && strings.Contains(hintKeys, k):
		i = strings.Index(hintKeys, k)
	case a == actOpen:
		if rows := s.visible(); s.cursor < len(rows) && rows[s.cursor][0] != "" {
			i = strings.Index(hintKeys, rows[s.cursor][0][:1])
		}
	}
	if i < 0 || i >= len(s.links) {
		return nil, false
	}
	url := s.links[i]
	m.closeSheet()
	m.status = "opening " + url
	return func() tea.Msg {
		if err := openURL(url); err != nil {
			return openedMsg{fmt.Errorf("opening %s: %w", url, err)}
		}
		return openedMsg{}
	}, true
}

// openURL hands a link to the system's opener. It is a variable so tests do
// not launch a browser. Only http(s) links ever get here.
var openURL = func(u string) error {
	if !isWebURL(u) {
		return fmt.Errorf("not a web link")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// linkAt is the URL of the hyperlink drawn at column col of a rendered row,
// read from the OSC-8 marks the renderer put around each link's label.
func linkAt(row string, col int) string {
	cur, x := "", 0
	for i := 0; i < len(row); {
		if row[i] == '\x1b' && i+1 < len(row) {
			switch row[i+1] {
			case ']': // OSC, ended by ST (ESC \) or BEL
				end, n := len(row), 0
				if j := strings.Index(row[i:], "\x1b\\"); j >= 0 {
					end, n = i+j, 2
				}
				if j := strings.IndexByte(row[i:], '\a'); j >= 0 && i+j < end {
					end, n = i+j, 1
				}
				if body := row[i+2 : end]; strings.HasPrefix(body, "8;") {
					_, cur, _ = strings.Cut(body[2:], ";")
				}
				i = end + n
				continue
			case '[': // CSI, ended by a byte in @..~
				j := i + 2
				for j < len(row) && (row[j] < 0x40 || row[j] > 0x7e) {
					j++
				}
				i = j + 1
				continue
			}
		}
		g, w := ansi.FirstGraphemeCluster(row[i:], ansi.GraphemeWidth)
		if g == "" {
			break
		}
		if col >= x && col < x+max(w, 1) {
			if isWebURL(cur) {
				return cur
			}
			return ""
		}
		x += w
		i += len(g)
	}
	return ""
}

// clickLink opens the link under a click in a message pane, if any.
func (m *model) clickLink(p pane, x, y int) tea.Cmd {
	rows := m.layout.chatText
	if p == paneThread {
		rows = m.layout.threadText
	}
	if y < 0 || y >= len(rows) || m.messageAt(p, y) < 0 {
		return nil
	}
	origin := m.paneX(p)
	if boxed() {
		origin++
	}
	u := linkAt(rows[y], x-origin)
	if u == "" {
		return nil
	}
	m.status = "opening " + u
	return func() tea.Msg {
		if err := openURL(u); err != nil {
			return openedMsg{fmt.Errorf("opening %s: %w", u, err)}
		}
		return openedMsg{}
	}
}
