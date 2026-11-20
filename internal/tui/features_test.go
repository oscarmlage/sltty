package tui

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
)

func TestDefaultKeymapIsValid(t *testing.T) {
	if _, err := newKeymap(nil); err != nil {
		t.Fatal(err)
	}
	var cfg struct{ Keys map[string][]string }
	if err := json.Unmarshal([]byte(DefaultsJSON()), &cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := newKeymap(cfg.Keys); err != nil {
		t.Fatalf("`sltty keys` output does not load back: %v", err)
	}
}

func TestKeysOutputIncludesConfigurableEmojiAndWho(t *testing.T) {
	var cfg struct{ Keys map[string][]string }
	if err := json.Unmarshal([]byte(DefaultsJSON()), &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Keys["emoji"]; !slices.Equal(got, []string{"ctrl+e"}) {
		t.Fatalf("emoji keys: %v", got)
	}
	if _, ok := cfg.Keys["who"]; !ok {
		t.Fatal("who is missing from sltty keys")
	}
}

func TestKeymapOverridesAndErrors(t *testing.T) {
	km, err := newKeymap(map[string][]string{"react": {"*"}, "normal_mode": {"ctrl+g", "j j"}})
	if err != nil {
		t.Fatal(err)
	}
	if !km.is(scNormal, "*", actReact) || km.is(scNormal, "r", actReact) {
		t.Fatal("react not rebound to *")
	}
	if !km.is(scInsert, "j j", actNormalMode) || km.is(scInsert, "esc", actNormalMode) {
		t.Fatal("normal_mode not rebound")
	}
	for name, o := range map[string]map[string][]string{
		"unknown action": {"fly": {"f"}},
		"conflict":       {"react": {"j"}},
		"three keys":     {"top": {"g g g"}},
		"prefix clash":   {"bottom": {"g"}},
	} {
		if _, err := newKeymap(o); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestLeaveInsertWithoutEsc(t *testing.T) {
	m := newTest(t)
	press(m, "i")
	typeText(m, "ojk")
	if m.mode != normal || m.chatIn.Value() != "o" {
		t.Fatalf("jk: mode %v text %q", m.mode, m.chatIn.Value())
	}
	press(m, "i", "ctrl+c")
	if m.mode != normal {
		t.Fatal("ctrl+c did not leave INSERT")
	}
	typeText(m, "j") // still running: ctrl+c in INSERT is not quit
}

func TestEditWithE(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "ana")
	press(m, "l", "k", "k") // "you: sure, send it over"
	press(m, "e")
	if m.mode != insert || m.editing == nil || m.chatIn.Value() != "sure, send it over" {
		t.Fatalf("e: mode %v editing %v text %q", m.mode, m.editing != nil, m.chatIn.Value())
	}
	if !strings.Contains(screen(m), "editing") {
		t.Fatal("no editing title")
	}
	m.chatIn.SetValue("sure, I will take it now")
	press(m, "enter")
	got := m.history["D1"][1]
	if got.Text != "sure, I will take it now" || !got.Edited || m.mode != normal {
		t.Fatalf("after save: %+v mode %v", got, m.mode)
	}
	if !strings.Contains(screen(m), "(edited)") {
		t.Fatal("no (edited) mark")
	}
	press(m, "k", "e")
	if m.editing != nil {
		t.Fatal("edited someone else's message")
	}
}

func TestEditLastWithUpKeepsDraftOnCancel(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "ana")
	press(m, "l", "i", "up")
	if m.editing == nil || m.chatIn.Value() != "sure, send it over" {
		t.Fatalf("up: editing %v text %q", m.editing != nil, m.chatIn.Value())
	}
	press(m, "esc") // cancels the edit, stays in INSERT
	if m.editing != nil || m.mode != insert || m.chatIn.Value() != "" {
		t.Fatalf("esc: editing %v mode %v text %q", m.editing != nil, m.mode, m.chatIn.Value())
	}
	typeText(m, "draft")
	press(m, "up") // not empty: the cursor moves, no edit
	if m.editing != nil {
		t.Fatal("up edited with text in the box")
	}
}

func TestReactToggles(t *testing.T) {
	m := newTest(t)
	press(m, "l", "r")
	if m.picker == nil || m.picker.kind != pickReact {
		t.Fatal("r did not open the reaction picker")
	}
	typeText(m, "roc")
	press(m, "enter")
	msg := m.history["C1"][m.selected()]
	if len(msg.Reactions) == 0 || msg.Reactions[len(msg.Reactions)-1].Emoji != "🚀" || msg.Reactions[len(msg.Reactions)-1].Count != 1 || !msg.Reactions[len(msg.Reactions)-1].Mine {
		t.Fatalf("reactions %+v", msg.Reactions)
	}
	press(m, "r")
	typeText(m, "roc")
	press(m, "enter")
	if n := len(m.history["C1"][m.selected()].Reactions); n != len(msg.Reactions)-1 {
		t.Fatalf("second pick did not remove it: %+v", m.history["C1"][m.selected()].Reactions)
	}
}

func TestPickerListModeWithJK(t *testing.T) {
	m := newTest(t)
	press(m, "ctrl+k", "esc")
	if m.picker == nil || m.picker.typing {
		t.Fatal("esc did not switch the picker to its list")
	}
	press(m, "j", "j", "k", "j", "j")
	want := m.convs[m.picker.items[3].conv].Name
	press(m, "enter")
	if m.picker != nil || m.current().Name != want {
		t.Fatalf("picked %q, want %q", m.current().Name, want)
	}
	press(m, "ctrl+k", "esc", "esc")
	if m.picker != nil {
		t.Fatal("esc in the list did not close the picker")
	}
}

// search runs a query through the debounce by hand: the test helper drops
// the slow tick.
func search(m *model, q string) {
	press(m, "ctrl+f")
	m.picker.input.SetValue(q) // typing it would wait on the debounce at every key
	m.refreshPicker()
	run(m, searchTick{strings.ToLower(q)})
}

func TestSearchJumpsIntoThread(t *testing.T) {
	m := newTest(t)
	search(m, "out of memory")
	if m.picker == nil || len(m.picker.items) != 1 {
		t.Fatalf("hits: %+v", m.picker)
	}
	press(m, "enter")
	if m.thread == nil || m.focus != paneChat && m.focus != paneThread {
		t.Fatal("no thread opened")
	}
	if got := m.thread.msgs[m.thread.sel].Text; !strings.Contains(got, "out of memory") {
		t.Fatalf("selected reply %q", got)
	}
}

func TestSearchJumpsToAnotherChat(t *testing.T) {
	m := newTest(t)
	search(m, "timeout")
	press(m, "enter")
	if m.current().Name != "ana" || !strings.Contains(m.selectedMsg().Text, "timeout") {
		t.Fatalf("at %q, selected %q", m.current().Name, m.selectedMsg().Text)
	}
}

func TestHelpShowsConfiguredKeys(t *testing.T) {
	km, err := newKeymap(map[string][]string{"react": {"*"}})
	if err != nil {
		t.Fatal(err)
	}
	m := newTest(t)
	m.km = km
	press(m, "?")
	s := screen(m)
	if !m.help || !strings.Contains(s, "Keybindings") {
		t.Fatalf("help not shown:\n%s", s)
	}
	for _, r := range m.sheet.rows {
		if r[1] == "add or remove a reaction" && r[0] != "*" {
			t.Fatalf("help shows the default key: %q", r[0])
		}
	}
	press(m, "q")
	if m.help {
		t.Fatal("q did not close the help")
	}
}

func TestCtrlPIsTheSwitcherWithFilters(t *testing.T) {
	m := newTest(t)
	press(m, "ctrl+p")
	if m.picker == nil || m.picker.kind != pickSwitch {
		t.Fatal("ctrl+p did not open the switcher")
	}
	s := screen(m)
	if !strings.Contains(s, "Switch conversation") || !strings.Contains(s, "╭") {
		t.Fatalf("no centered box:\n%s", s)
	}
	typeText(m, "@")
	for _, it := range m.picker.items {
		if m.convs[it.conv].Kind != chat.DM {
			t.Fatalf("@ let %q through", m.convs[it.conv].Name)
		}
	}
	m.picker.input.SetValue("#t")
	m.refreshPicker()
	if len(m.picker.items) == 0 {
		t.Fatal("#t found nothing")
	}
	for _, it := range m.picker.items {
		if it.conv < 0 || m.convs[it.conv].Kind != chat.Channel {
			t.Fatalf("# let %+v through", it)
		}
	}
}

func TestSearchModifiers(t *testing.T) {
	m := newTest(t)
	search(m, "restarted from:@carol")
	if len(m.picker.items) != 1 || m.picker.items[0].hit.Msg.User != "carol" {
		t.Fatalf("from: hits %+v", m.picker.items)
	}
	if !strings.Contains(m.picker.items[0].label, matchStyle.Render("restarted")) {
		t.Fatal("the word is not highlighted")
	}
	press(m, "esc", "esc")
	search(m, "runner in:#general")
	for _, it := range m.picker.items {
		if it.hit.ConvID != "C1" {
			t.Fatalf("in: let %s through", it.hit.ConvID)
		}
	}
	if q := chat.ParseQuery("OOM in:#deploys from:@Bob x"); q.In != "deploys" || q.From != "bob" || strings.Join(q.Words, " ") != "OOM x" {
		t.Fatalf("parse: %+v", q)
	}
}

func TestPickerClick(t *testing.T) {
	m := newTest(t)
	press(m, "ctrl+k")
	m.render()
	want := m.picker.items[2]
	run(m, tea.MouseClickMsg{X: m.layout.pickX + 4, Y: m.layout.pickTop + 2, Button: tea.MouseLeft})
	if m.picker != nil || m.current().ID != m.convs[want.conv].ID {
		t.Fatalf("click picked %q, want %q", m.current().Name, m.convs[want.conv].Name)
	}
	press(m, "ctrl+k")
	m.render()
	run(m, tea.MouseClickMsg{X: 0, Y: 5, Button: tea.MouseLeft})
	if m.picker != nil {
		t.Fatal("a click outside did not close it")
	}
}

func TestSlashFindsWhatHasTheFocus(t *testing.T) {
	m := newTest(t)
	press(m, "/")
	if m.picker == nil || m.picker.kind != pickSwitch {
		t.Fatal("/ in the sidebar is not the switcher")
	}
	press(m, "esc", "esc", "l", "/")
	if m.picker == nil || m.picker.kind != pickSearch {
		t.Fatal("/ in the chat is not the search")
	}
	press(m, "esc", "esc", "h", "ctrl+f")
	if m.picker == nil || m.picker.kind != pickSearch {
		t.Fatal("ctrl+f in the sidebar is not the search")
	}
}

func TestSearchScopeAndNN(t *testing.T) {
	m := newTest(t)
	press(m, "l", "/") // from the chat: only #general
	p := m.picker
	if p.scope != "C1" || !strings.Contains(screen(m), "in:#general") {
		t.Fatalf("/ in the chat: scope %q", p.scope)
	}
	p.input.SetValue("runner")
	m.refreshPicker()
	if q := m.searchQuery(); q != "in:#general runner" {
		t.Fatalf("query %q", q)
	}
	press(m, "tab")
	if p.scope != "" || m.searchQuery() != "runner" {
		t.Fatalf("tab: scope %q query %q", p.scope, m.searchQuery())
	}
	run(m, searchTick{m.searchQuery()})
	all := len(p.items)
	convs := map[string]bool{}
	for _, it := range p.items {
		convs[it.hit.ConvID] = true
	}
	if all < 2 || len(convs) < 2 {
		t.Fatalf("everywhere: %d hits in %d chats", all, len(convs))
	}
	press(m, "enter")
	if m.found == nil || len(m.found.hits) != all {
		t.Fatal("picking did not keep the results for n/N")
	}
	if !strings.Contains(m.render(), matchStyle.Render("runner")) {
		t.Fatal("the word is not marked in the messages")
	}
	seen := map[string]bool{}
	for range all {
		h := m.found.hits[m.found.i]
		seen[h.ConvID] = true
		at := m.selectedMsg()
		if m.thread != nil && m.focus == paneChat {
			at = &m.thread.msgs[m.thread.sel]
		}
		if h.RootTS == "" && (m.current().ID != h.ConvID || at.TS != h.Msg.TS) {
			t.Fatalf("result %d: at %s %q, want %s %q", m.found.i, m.current().ID, at.Text, h.ConvID, h.Msg.Text)
		}
		press(m, "n")
	}
	if m.found.i != 0 || len(seen) != len(convs) {
		t.Fatalf("n did not walk every chat and wrap: i %d, %d/%d chats", m.found.i, len(seen), len(convs))
	}
	press(m, "N")
	if m.found.i != all-1 {
		t.Fatalf("N from the first: %d", m.found.i)
	}
	m.focus = paneChat
	press(m, "esc")
	if m.found != nil || m.focus != paneChat {
		t.Fatal("esc did not just clear the search first")
	}
	press(m, "n")
	if !strings.Contains(screen(m), "no search to repeat") {
		t.Fatal("n without a search said nothing")
	}

	press(m, "/")
	press(m, "backspace") // on the empty filter: drops the chip
	if m.picker.scope != "" {
		t.Fatal("backspace kept the scope")
	}
}
