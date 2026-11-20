package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/chat"
)

func TestMessageLinks(t *testing.T) {
	msg := &chat.Message{
		Text:        "see [the docs](https://a.test/docs), https://b.test/x. and [again](https://a.test/docs) javascript:alert(1)",
		Context:     []string{"more: http://c.test/y"},
		Attachments: []chat.Attachment{{Title: "Card", TitleURL: "https://d.test/card", Text: "[inner](https://e.test/z)"}},
	}
	got := messageLinks(msg)
	want := []link{{"the docs", "https://a.test/docs"}, {"https://b.test/x", "https://b.test/x"}, {"http://c.test/y", "http://c.test/y"}, {"Card", "https://d.test/card"}, {"inner", "https://e.test/z"}}
	if len(got) != len(want) {
		t.Fatalf("links = %+v", got)
	}
	for i := range want {
		if got[i].url != want[i].url || got[i].label != want[i].label && want[i].label != want[i].url {
			t.Errorf("link %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLinksSheetOpensByHint(t *testing.T) {
	var opened []string
	old := openURL
	openURL = func(u string) error { opened = append(opened, u); return nil }
	defer func() { openURL = old }()

	m := newTest(t)
	m.goTo(t, "tools-team")
	m.focus = paneChat
	// the Sentry message has links; walk up to a message that does
	for range 8 {
		if len(messageLinks(m.target())) > 0 {
			break
		}
		press(m, "k")
	}
	links := messageLinks(m.target())
	if len(links) < 2 {
		t.Skip("demo data has no message with two links")
	}
	press(m, "o")
	if !m.help || m.sheet == nil || m.sheet.links == nil {
		t.Fatal("o did not open the links sheet")
	}
	s := screen(m)
	if !strings.Contains(s, "Links") || !strings.Contains(s, "a 1") || !strings.Contains(s, "yank") {
		t.Fatalf("sheet not drawn with hints:\n%s", s)
	}
	press(m, "2") // 2 is the second link, like s
	if m.help {
		t.Fatal("sheet still open after picking")
	}
	if len(opened) != 1 || opened[0] != links[1].url {
		t.Fatalf("opened %v, want %s", opened, links[1].url)
	}
}

func TestNoLinksSaysSo(t *testing.T) {
	m := newTest(t)
	m.openLinks(&chat.Message{Text: "nothing here"})
	if m.help || !strings.Contains(m.status, "no links") {
		t.Fatalf("help %v status %q", m.help, m.status)
	}
}

func TestOpenURLRefusesNonWeb(t *testing.T) {
	if err := openURL("file:///etc/passwd"); err == nil {
		t.Fatal("opened a file: URL")
	}
}

func TestButtonLinkInBrackets(t *testing.T) {
	msg := &chat.Message{Text: "Please authenticate.\n[ [Authenticate](https://okta.test/authorize?a=1&b=2) ]"}
	got := messageLinks(msg)
	if len(got) != 1 || got[0].label != "Authenticate" || got[0].url != "https://okta.test/authorize?a=1&b=2" {
		t.Fatalf("a link button inside brackets is one link by its label: %+v", got)
	}
}

func TestLinkAt(t *testing.T) {
	row := "\x1b[1m[ \x1b]8;;https://okta.test/a?b=1\x1b\\Authenticate\x1b]8;;\x1b\\ ]\x1b[0m 🙂 \x1b]8;;javascript:x\aRun\x1b]8;;\a"
	for col, want := range map[int]string{0: "", 2: "https://okta.test/a?b=1", 13: "https://okta.test/a?b=1", 14: "", 17: "", 20: "", 99: ""} {
		if got := linkAt(row, col); got != want {
			t.Errorf("column %d: %q, want %q", col, got, want)
		}
	}
}

func TestClickOpensTheLinkUnderIt(t *testing.T) {
	var opened []string
	old := openURL
	openURL = func(u string) error { opened = append(opened, u); return nil }
	defer func() { openURL = old }()

	m := newTest(t)
	m.goTo(t, "tools-team")
	m.render()
	origin := m.mainX()
	if boxed() {
		origin++
	}
	for y, row := range m.layout.chatText {
		if m.layout.chatMsg[y] < 0 {
			continue
		}
		for x := range ansi.StringWidth(row) {
			if u := linkAt(row, x); u != "" {
				run(m, tea.MouseClickMsg{X: origin + x, Y: y + m.layout.top, Button: tea.MouseLeft})
				run(m, tea.MouseReleaseMsg{X: origin + x, Y: y + m.layout.top, Button: tea.MouseLeft})
				if len(opened) != 1 || opened[0] != u {
					t.Fatalf("clicking %q opened %v", u, opened)
				}
				return
			}
		}
	}
	t.Skip("demo data draws no link in tools-team")
}

func TestLinksYanksTheSelectedURL(t *testing.T) {
	m := newTest(t)
	m.openLinks(&chat.Message{Text: "https://example.com/a https://example.com/b"})
	press(m, "down", "y")
	if !m.help || m.status != "copied link" {
		t.Fatalf("link yank: help %v status %q", m.help, m.status)
	}
}

func TestLinksKeyTwiceOpensTheOnlyLink(t *testing.T) {
	var opened []string
	old := openURL
	openURL = func(u string) error { opened = append(opened, u); return nil }
	defer func() { openURL = old }()

	m := newTest(t)
	m.openLinks(&chat.Message{Text: "see https://example.com/one"})
	if !m.help || len(m.sheet.links) != 1 {
		t.Fatal("the links sheet did not open with one link")
	}
	press(m, "o")
	if m.help || len(opened) != 1 || opened[0] != "https://example.com/one" {
		t.Fatalf("o o: help %v, opened %v", m.help, opened)
	}

	// With two links, o again does not pick one.
	opened = nil
	m.openLinks(&chat.Message{Text: "https://example.com/a https://example.com/b"})
	press(m, "o")
	if len(opened) != 0 {
		t.Fatalf("o with two links opened %v", opened)
	}
}
