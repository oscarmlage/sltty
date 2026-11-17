package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/chat"
)

// named is a Fake under another workspace name.
type named2 struct {
	*chat.Fake
	name string
}

func (n named2) Workspace() string { return n.name }

// drive feeds msg to the app and then what the accounts' commands bring
// back for them.
func drive(a *app, msg tea.Msg) {
	queue := []tea.Msg{msg}
	for steps := 0; len(queue) > 0 && steps < 500; steps++ {
		msg, queue = queue[0], queue[1:]
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				if c != nil {
					queue = append(queue, quick(c))
				}
			}
			continue
		}
		if msg == nil {
			continue
		}
		_, cmd := a.Update(msg)
		if cmd == nil {
			continue
		}
		switch out := quick(cmd).(type) {
		case tea.BatchMsg:
			for _, c := range out {
				if c != nil {
					queue = append(queue, quick(c))
				}
			}
		case acctMsg:
			switch out.msg.(type) {
			case convsMsg, historyMsg, repliesMsg, sentMsg, changedMsg, markedMsg, presenceMsg, searchMsg, threadsMsg, activityMsg, tabMsg, cachedHistoryMsg:
				queue = append(queue, out)
			}
		}
	}
}

func newTestApp(t *testing.T) *app {
	t.Helper()
	opts := Options{ConfigPath: "/tmp/x/config.json"}
	a := newApp([]*model{
		newModel(named2{chat.NewFake(), "Flywire"}, defaultKeymap(), opts),
		newModel(named2{chat.NewFake(), "Telegram"}, defaultKeymap(), opts),
	})
	drive(a, tea.WindowSizeMsg{Width: 120, Height: 32})
	drive(a, a.Init()())
	for i, m := range a.tabs {
		if len(m.convs) == 0 {
			t.Fatalf("account %d: no conversations", i)
		}
	}
	return a
}

func appScreen(a *app) []string { return strings.Split(ansi.Strip(a.View().Content), "\n") }

func TestTabsShowTheAccounts(t *testing.T) {
	a := newTestApp(t)
	rows := appScreen(a)
	if len(rows) != 32 {
		t.Fatalf("%d rows, want the terminal's 32", len(rows))
	}
	if !strings.Contains(rows[0], "1 Flywire") || !strings.Contains(rows[0], "2 Telegram") {
		t.Fatalf("tab bar: %q", rows[0])
	}
	for i, l := range strings.Split(a.View().Content, "\n") {
		if w := ansi.StringWidth(l); w != 120 {
			t.Fatalf("row %d is %d wide", i, w)
		}
	}
	if a.tabs[0].height != 31 || a.tabs[1].height != 31 {
		t.Fatal("the accounts do not leave the tab bar its row")
	}
}

func TestSwitchingTabs(t *testing.T) {
	a := newTestApp(t)
	drive(a, tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	if a.active != 1 {
		t.Fatal("alt+2 did not open the second account")
	}
	drive(a, tea.KeyPressMsg{Code: 'g', Text: "g"})
	drive(a, tea.KeyPressMsg{Code: 't', Text: "t"})
	if a.active != 0 {
		t.Fatal("g t did not cycle back to the first account")
	}
	drive(a, tea.KeyPressMsg{Code: 'g', Text: "g"})
	drive(a, tea.KeyPressMsg{Code: 'T', Text: "T"})
	if a.active != 1 {
		t.Fatal("g T did not go to the previous account")
	}
	drive(a, tea.KeyPressMsg{Code: '9', Mod: tea.ModAlt})
	if a.active != 1 {
		t.Fatal("a tab that does not exist was opened")
	}
	drive(a, tea.KeyPressMsg{Code: '1', Text: "1"})
	if a.active != 0 {
		t.Fatal("1 did not open the first account")
	}
	drive(a, tea.KeyPressMsg{Code: 'i', Text: "i"}) // INSERT: 2 is text
	drive(a, tea.KeyPressMsg{Code: '2', Text: "2"})
	if a.active != 0 || !strings.Contains(a.tabs[0].chatIn.Value(), "2") {
		t.Fatal("2 while writing switched tabs instead of typing")
	}
	drive(a, tea.KeyPressMsg{Code: tea.KeyEscape})
	drive(a, tea.KeyPressMsg{Code: '2', Text: "2"})
	if a.active != 1 {
		t.Fatal("2 in NORMAL did not open the second account")
	}
	// A click on the bar.
	span := a.tabSpans()[0]
	drive(a, tea.MouseClickMsg{X: span[0] + 1, Y: 0, Button: tea.MouseLeft})
	if a.active != 0 {
		t.Fatal("a click on the first tab did not open it")
	}
}

func TestKeysGoToTheActiveAccountOnly(t *testing.T) {
	a := newTestApp(t)
	before := a.tabs[1].cursor
	drive(a, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if a.tabs[1].cursor != before {
		t.Fatal("a key reached the hidden account")
	}
	if a.tabs[0].cursor == before && a.tabs[0].onSection == "" && a.tabs[0].view == sideConv {
		t.Fatal("the active account did not move")
	}
}

func TestSettingsAreSharedButThemesAreNot(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(func() { a.tabs[0].setTheme(defaultTheme, "") })
	a.tabs[1].setTheme("telegram", "")
	a.tabs[0].setTheme("nord", "")
	a.tabs[0].setSide(40)
	a.tabs[0].setInputRows(4)
	drive(a, tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	if m := a.tabs[1]; m.side != 40 || m.inputRows != 4 {
		t.Fatalf("settings not carried: side %d rows %d", m.side, m.inputRows)
	}
	if a.tabs[1].themeName != "telegram" || theme.SidebarBg != "#17212b" {
		t.Fatalf("the second account lost its theme: %s", a.tabs[1].themeName)
	}
	drive(a, tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt})
	if a.tabs[0].themeName != "nord" || theme.Primary != "#88c0d0" {
		t.Fatal("back on the first account, its theme is not applied")
	}
}

func TestActiveTabStandsOut(t *testing.T) {
	a := newTestApp(t)
	bar := strings.Split(a.View().Content, "\n")[0]
	primary := lipglossBg(a.tabs[0].accountTheme().Primary)
	if !strings.Contains(bar, primary) {
		t.Fatalf("the active tab is not filled with its theme's primary color: %q", bar)
	}
	drive(a, tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	bar2 := strings.Split(a.View().Content, "\n")[0]
	if bar == bar2 {
		t.Fatal("the bar does not change with the active tab")
	}
}

// lipglossBg is the SGR lipgloss writes for a background color.
func lipglossBg(hex string) string {
	r, g, b := rgb(hex)
	return fmt.Sprintf("48;2;%d;%d;%d", r, g, b)
}

func TestMouseIsShiftedUnderTheBar(t *testing.T) {
	a := newTestApp(t)
	m := a.tabs[0]
	a.View()
	// The first conversation row of the sidebar, in the account's own rows.
	y := -1
	for r, row := range m.layout.side {
		if row >= 0 && row != m.cursor {
			y = r
			break
		}
	}
	if y < 0 {
		t.Fatal("no other conversation in the sidebar")
	}
	want := m.layout.side[y]
	drive(a, tea.MouseClickMsg{X: 3, Y: y + 1, Button: tea.MouseLeft})
	drive(a, tea.MouseReleaseMsg{X: 3, Y: y + 1, Button: tea.MouseLeft})
	if m.cursor != want {
		t.Fatalf("the click opened %d, want %d", m.cursor, want)
	}
}

func TestMessagesReachTheirOwnAccount(t *testing.T) {
	a := newTestApp(t)
	id := a.tabs[1].convs[0].ID
	drive(a, acctMsg{1, historyMsg{id: id, msgs: []chat.Message{{TS: "1", User: "x", Text: "only here"}}}})
	if got := a.tabs[1].history[id]; len(got) != 1 || got[0].Text != "only here" {
		t.Fatal("the message did not reach account 2")
	}
	if got := a.tabs[0].history[id]; len(got) == 1 && got[0].Text == "only here" {
		t.Fatal("account 1 got account 2's message")
	}
	if !owned(historyMsg{}) || owned(tea.QuitMsg{}) {
		t.Fatal("owned misjudges messages")
	}
	if _, ok := tag(0, tea.QuitMsg{}).(tea.QuitMsg); !ok {
		t.Fatal("quit was not passed through")
	}
}

func TestImageIDsAreProgramWide(t *testing.T) {
	a, b := newImageID(), newImageID()
	if a == b || a < kittyFirstImage {
		t.Fatalf("ids %d %d", a, b)
	}
}

func TestSingleAccountHasNoBar(t *testing.T) {
	a := newApp([]*model{newModel(chat.NewFake(), defaultKeymap(), Options{})})
	drive(a, tea.WindowSizeMsg{Width: 100, Height: 20})
	drive(a, a.Init()())
	if a.bar() != 0 || a.tabs[0].height != 20 {
		t.Fatal("a single account must use the whole screen")
	}
}
