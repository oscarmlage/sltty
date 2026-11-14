package tui

import (
	"strings"
	"testing"

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
	if !strings.Contains(s, "Links") || !strings.Contains(s, "a 1") {
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
