package tui

import (
	"strings"
	"testing"

	"github.com/oscarmlage/sltty/internal/chat"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTabsNeverReachTheTerminal(t *testing.T) {
	m := newTest(t)
	c := m.current()
	m.history[c.ID] = []chat.Message{{TS: "1", User: "ana", Text: "col1\tcol2\tcol3\t" + strings.Repeat("x", 200)}}
	for i, l := range strings.Split(m.render(), "\n") {
		if strings.Contains(l, "\t") {
			t.Fatalf("row %d has a tab: the terminal would widen it", i)
		}
		if w := ansi.StringWidth(l); w != m.width {
			t.Fatalf("row %d is %d wide, want %d", i, w, m.width)
		}
	}
}

func TestGraphemeWidthForKitty(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	cmd := graphemeWidthCmd(env(map[string]string{"TERM": "xterm-kitty"}))
	if cmd == nil {
		t.Fatal("kitty keeps wcwidth: emoji rows overflow")
	}
	if msg, ok := cmd().(tea.ModeReportMsg); !ok || msg.Mode != ansi.ModeUnicodeCore || msg.Value != ansi.ModeSet {
		t.Fatalf("mode report: %#v", cmd())
	}
	if graphemeWidthCmd(env(map[string]string{"TERM": "xterm-256color"})) != nil {
		t.Fatal("forced grapheme widths on an unknown terminal")
	}
}
