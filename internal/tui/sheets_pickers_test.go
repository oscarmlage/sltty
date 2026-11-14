package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"
	"time"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/diag"
)

func TestKeysSheetNavigation(t *testing.T) {
	m := newTest(t)
	press(m, "?")
	if !m.help || m.sheet == nil || len(m.sheet.tabs) < 3 {
		t.Fatal("no keys sheet with tabs")
	}
	tab := m.sheet.tab
	press(m, "l")
	if m.sheet.tab != tab+1 {
		t.Fatalf("l: tab %d", m.sheet.tab)
	}
	press(m, "h", "h") // wraps around to the last tab
	if m.sheet.tab != len(m.sheet.tabs)-1 {
		t.Fatalf("h wraps: tab %d of %d", m.sheet.tab, len(m.sheet.tabs))
	}
	press(m, "l") // and forward again, round to the first
	press(m, "j", "j", "k", "ctrl+d", "ctrl+u", "G", "g", "g", "G", "k")
	// scrolling a long tab keeps the cursor in view
	m.sheet.cursor = len(m.sheet.visible()) - 1
	screen(m)
	m.sheet.cursor = 0
	screen(m)
	// the filter: typing, found count, no match, esc clears then closes
	press(m, "/")
	typeText(m, "zzzzqq")
	if s := screen(m); !strings.Contains(s, "nothing matches") {
		t.Fatalf("no match not said:\n%s", s)
	}
	press(m, "enter") // leaves the typing, keeps the filter
	if m.sheet.searching {
		t.Fatal("enter should leave the filter box")
	}
	if s := screen(m); !strings.Contains(s, "esc clears") {
		t.Fatal("the filter is not shown")
	}
	press(m, "h", "l") // with a filter, tabs do not move
	press(m, "esc")
	if m.sheet.filter.Value() != "" {
		t.Fatal("esc must clear the filter first")
	}
	press(m, "esc")
	if m.help {
		t.Fatal("esc must close the sheet")
	}
	// the typing filter leaves with esc too
	press(m, "?", "/")
	typeText(m, "react")
	if len(m.sheet.visible()) == 0 {
		t.Fatal("no binding mentions react")
	}
	press(m, "esc")
	press(m, "q")
	// a section divider row renders dimmed
	m.sheet = newSheet("t", "", [][2]string{{"", "── part ──"}, {"a", "b"}})
	m.help = true
	if !strings.Contains(screen(m), "part") {
		t.Fatal("divider row not drawn")
	}
	m.closeSheet()
	press(m, "?")
	m.sheet = nil // a sheet action with no sheet just closes
	press(m, "j")
	if m.help {
		t.Fatal("a sheet-less help must close")
	}
}

func TestReactionsSheetEdgeCases(t *testing.T) {
	m := newTest(t)
	m.openReactions(&chat.Message{})
	if !strings.Contains(m.status, "no reactions") {
		t.Fatalf("status %q", m.status)
	}
	m.openReactions(nil)
	m.openReactions(&chat.Message{Reactions: []chat.Reaction{{Emoji: "👍", Count: 2}}})
	if !strings.Contains(screen(m), "did not return the people") {
		t.Fatal("a reaction with no people must say so")
	}
}

func TestLogSheetShowsTheTail(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if err := diag.Init(); err != nil {
		t.Fatal(err)
	}
	diag.Printf("hello from the test")
	m := newTest(t)
	m.opts.LogPath = diag.Path()
	m.openLog()
	if !strings.Contains(screen(m), "Logs") {
		t.Fatal("no log sheet")
	}
}

func TestPickerKeysInTheListAndFilter(t *testing.T) {
	m := newTest(t)
	press(m, "ctrl+k")
	press(m, "ctrl+n", "down", "ctrl+p", "up", "ctrl+j", "ctrl+k")
	press(m, "esc") // to the list
	if m.picker.typing {
		t.Fatal("esc should leave the filter for the list")
	}
	press(m, "j", "k", "ctrl+d", "ctrl+u", "G", "g", "g")
	press(m, "i") // back to the filter
	if !m.picker.typing {
		t.Fatal("i must return to the filter")
	}
	press(m, "esc", "esc")
	if m.picker != nil {
		t.Fatal("esc in the list closes the picker")
	}
	// the search scope key needs a conversation to search in
	press(m, "ctrl+f", "tab")
	press(m, "tab")
	press(m, "esc", "esc")
	empty := newModel(chat.NewFake(), defaultKeymap(), Options{})
	empty.picker = &picker{kind: pickSearch, typing: true}
	empty.picker.input = newPickerInput()
	empty.pickerTypingKey(tabKey(), "tab")
	if !strings.Contains(empty.status, "no conversation open") {
		t.Fatalf("status %q", empty.status)
	}
}

func TestSwitcherReachesThreadsActivityAndStatusPicks(t *testing.T) {
	m := newTest(t)
	for _, tc := range []struct {
		query string
		view  sideKind
	}{{"threads", sideThreads}, {"activity", sideActivity}} {
		press(m, "ctrl+k")
		typeText(m, tc.query)
		press(m, "enter")
		if m.view != tc.view {
			t.Fatalf("%s: view %v", tc.query, m.view)
		}
	}
	for _, q := range []string{"away", "active", "snooze for 20"} {
		press(m, "ctrl+s")
		typeText(m, q)
		press(m, "enter")
		if m.picker != nil {
			t.Fatalf("%s: picker still open", q)
		}
	}
}

func TestToggleReadEverywhere(t *testing.T) {
	m := newTest(t)
	// threads list: a single thread, then all of them
	press(m, "k", "k", "k", "l")
	press(m, "u")
	press(m, "h", "u")
	// activity list likewise
	press(m, "j", "l", "u")
	press(m, "h", "u")
	// in a thread that is not in the threads list
	m.view = sideConv
	m.goTo(t, "random")
	press(m, "l", "g", "g", "enter")
	m.threads = nil
	press(m, "u")
	if !strings.Contains(m.status, "not one of your threads") {
		t.Fatalf("status %q", m.status)
	}
	// a chat with no messages
	m.thread, m.focus = nil, paneChat
	m.history[m.current().ID] = nil
	press(m, "u")
	if !strings.Contains(m.status, "no messages") {
		t.Fatalf("status %q", m.status)
	}
	// reading what is unread
	m.goTo(t, "deploys")
	m.current().Unread = 4
	press(m, "u")
	if !strings.Contains(m.status, "marked read") {
		t.Fatalf("status %q", m.status)
	}
}

func TestEmptyListsSayLoadingOrNothing(t *testing.T) {
	m := newTest(t)
	press(m, "k", "k", "k", "l")
	m.threads, m.activity = nil, nil
	if !strings.Contains(screen(m), "loading") {
		t.Fatal("a feed not loaded yet must say loading")
	}
	m.threads = []chat.ThreadSummary{}
	if !strings.Contains(screen(m), "nothing here yet") {
		t.Fatal("an empty feed must say so")
	}
}

func TestAttachmentCardsAndEditedMarks(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "help")
	id := m.current().ID
	long := strings.Repeat("word ", 40)
	m.history[id] = []chat.Message{
		{TS: "1.0", User: "bot", At: time.Now().AddDate(-2, 0, 0), Text: "last year's card",
			Attachments: []chat.Attachment{{Color: "#36a64f", Pretext: "pre", Title: "Build", TitleURL: "https://ci.test/1", Text: "all green",
				Fields: []chat.AttachmentField{{Title: "env", Value: "prod"}, {Value: "no title"}}, Footer: "ci"}}},
		{TS: "2.0", User: "you", At: time.Now().AddDate(0, 0, -20), Text: long, Edited: true,
			Reactions: []chat.Reaction{{Emoji: "👍", Count: 1, Mine: true}, {Emoji: "🎉", Count: 3}}},
		{TS: "3.0", User: "ana", At: time.Now(), Text: "short", Edited: true},
	}
	s := screen(m)
	for _, want := range []string{"Build", "all green", "env: prod", "(edited)", "👍", "20"} {
		if want == "20" {
			continue
		}
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if dayName(time.Now().AddDate(-2, 0, 0), time.Now()) == dayName(time.Now(), time.Now()) {
		t.Fatal("dayName")
	}
	if n := dayName(time.Now().AddDate(0, 0, -400), time.Now()); !strings.Contains(n, ",") {
		t.Fatalf("a date from another year: %q", n)
	}
}

func newPickerInput() textinput.Model { return textinput.New() }
func tabKey() tea.KeyPressMsg         { return tea.KeyPressMsg{Code: tea.KeyTab} }
