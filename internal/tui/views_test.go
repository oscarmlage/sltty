package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/img"
)

func TestPaneHeaderFlattensMultilineTopics(t *testing.T) {
	newTest(t) // initializes the shared styles
	got := ansiStrip(paneHeader("#alerts", "Bots:\n/pd help for pagerduty", 80, false))
	if strings.Contains(got, "\n") || !strings.Contains(got, "Bots: /pd help for pagerduty") {
		t.Fatalf("multiline topic escaped its header: %q", got)
	}
}

func TestPrivateChannelsHaveALock(t *testing.T) {
	m := newTest(t)
	s := screen(m)
	if !strings.Contains(s, "🔒tools-oncall") || strings.Contains(s, "# tools-oncall") {
		t.Fatalf("private channel without a lock:\n%s", s)
	}
	m.goTo(t, "leads")
	if !strings.Contains(screen(m), "🔒 leads") {
		t.Fatal("no lock in the header")
	}
}

func TestCustomSections(t *testing.T) {
	m := newTest(t)
	m.opts.Sections = []Section{
		{Name: "Team", Channels: []string{"tools-*", "@ana"}},
		{Name: "Noise", Channels: []string{"deploys", "random"}},
	}
	s := screen(m)
	team, noise, chans, dms := strings.Index(s, "▾ Team"), strings.Index(s, "▾ Noise"), strings.Index(s, "▾ Channels"), strings.Index(s, "▾ Direct messages")
	if team < 0 || !(team < noise && noise < chans && chans < dms) {
		t.Fatalf("section order: team %d noise %d channels %d dms %d\n%s", team, noise, chans, dms, s)
	}
	var names []string
	for _, it := range m.sideOrder() {
		if it.kind == sideConv {
			names = append(names, m.convs[it.conv].Name)
		}
	}
	want := "tools-team tools-oncall ana deploys random general leads help bob carol you"
	if got := strings.Join(names, " "); got != want {
		t.Fatalf("sidebar order\n got %s\nwant %s", got, want)
	}
	if err := validateSections([]Section{{Name: "x", Channels: []string{"[bad"}}}); err == nil {
		t.Fatal("bad pattern accepted")
	}
}

func TestUnreadOnlySidebarKeepsSwitcherComplete(t *testing.T) {
	m := newTest(t)
	press(m, "U")
	if !m.unreadOnly {
		t.Fatal("U did not enable unread-only sidebar")
	}
	for _, it := range m.sideOrder() {
		if it.kind == sideConv && m.convs[it.conv].Unread == 0 && it.conv != m.openConv() {
			t.Fatalf("read chat %q in unread-only sidebar", m.convs[it.conv].Name)
		}
	}
	press(m, "ctrl+p")
	if m.picker == nil {
		t.Fatal("switcher did not open")
	}
	if !slices.ContainsFunc(m.picker.items, func(it pickItem) bool { return it.conv >= 0 && m.convs[it.conv].Name == "general" }) {
		t.Fatal("switcher hid a read chat")
	}
}

func TestUnreadOnlyKeepsTheOpenChatWhenRead(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "tools-team")
	press(m, "U", "u")
	if c := m.current(); c.Name != "tools-team" || c.Unread != 0 {
		t.Fatalf("u: on %q with %d unread", c.Name, c.Unread)
	}
	if !slices.Contains(m.sideOrder(), sideItem{kind: sideConv, conv: m.cursor}) {
		t.Fatal("the chat just read left the sidebar")
	}
	before := m.cursor
	press(m, "j")
	if m.view != sideConv || m.cursor == before || m.current().Unread == 0 {
		t.Fatalf("j from the read chat went to %q (view %v)", m.current().Name, m.view)
	}
	if slices.Contains(m.sideOrder(), sideItem{kind: sideConv, conv: before}) {
		t.Fatal("a read chat left behind should drop out of the unread sidebar")
	}
}

func TestUnreadOnlyKeepsTheChatAfterEnteringAndLeaving(t *testing.T) {
	for _, keys := range [][]string{{"l", "h"}, {"enter", "esc"}, {"right", "left"}, {"tab", "tab"}} {
		m := newTest(t)
		m.goTo(t, "tools-team")
		press(m, "U")
		press(m, keys...) // entering reads it
		if c := m.current(); c.Name != "tools-team" || c.Unread != 0 || m.focus != paneSide {
			t.Fatalf("%v: on %q, %d unread, focus %v", keys, c.Name, c.Unread, m.focus)
		}
		if !slices.Contains(m.sideOrder(), m.sideCurrent()) {
			t.Fatalf("%v: the chat just read left the sidebar", keys)
		}
		press(m, "j")
		if m.current().Name == "tools-team" || m.current().Unread == 0 {
			t.Fatalf("%v: j went to %q", keys, m.current().Name)
		}
	}
}

func TestSidebarWalksThreadsAndActivity(t *testing.T) {
	m := newTest(t)
	press(m, "k") // from #general up: its section's header, Activity, Threads
	if m.onSection != builtinChannels || m.view != sideConv || m.current().Name != "general" {
		t.Fatalf("not on the header: %q, view %v", m.onSection, m.view)
	}
	press(m, "k")
	if m.view != sideActivity || m.onSection != "" {
		t.Fatalf("view %v", m.view)
	}
	press(m, "k")
	if m.view != sideThreads || !strings.Contains(screen(m), "threads you are in") {
		t.Fatalf("view %v", m.view)
	}
	press(m, "j", "j", "j")
	if m.view != sideConv || m.current().Name != "general" {
		t.Fatalf("back down: view %v at %q", m.view, m.current().Name)
	}
}

func TestThreadsViewOpenAndReply(t *testing.T) {
	m := newTest(t)
	before := m.threadsUnread()
	if before == 0 {
		t.Fatal("the demo should have unread threads")
	}
	press(m, "k", "k", "k", "l") // the header, Activity, Threads; focus the list
	if len(m.threads) == 0 {
		t.Fatal("no threads")
	}
	first := m.threads[0]
	press(m, "enter")
	if m.thread == nil || m.thread.convID != first.ConvID || m.thread.root != first.Root.TS {
		t.Fatalf("enter opened %+v, want %s/%s", m.thread, first.ConvID, first.Root.TS)
	}
	if m.threadsUnread() >= before && m.threadUnread(first) > 0 {
		t.Fatal("opening the thread did not mark it read")
	}
	press(m, "i")
	typeText(m, "visto")
	press(m, "enter", "esc")
	last := m.thread.msgs[len(m.thread.msgs)-1]
	if last.Text != "visto" {
		t.Fatalf("reply went elsewhere: last %q", last.Text)
	}
	if !strings.Contains(screen(m), "Threads › thread") {
		t.Fatal("status line does not say where the thread is")
	}
}

func TestActivityJumpsAndReads(t *testing.T) {
	m := newTest(t)
	if m.activityUnread() == 0 {
		t.Fatal("the demo should have unread activity")
	}
	press(m, "k", "k", "l") // the header, Activity
	i := -1
	for j, a := range m.activity {
		if a.Kind == chat.ActMention && a.ConvID == "C1" {
			i = j
		}
	}
	if i < 0 {
		t.Fatalf("no mention in #general: %+v", m.activity)
	}
	m.listSel[sideActivity] = i
	press(m, "enter")
	if m.view != sideConv || m.current().ID != "C1" || !strings.Contains(m.selectedMsg().Text, "@you") {
		t.Fatalf("jumped to %v %q", m.view, m.selectedMsg().Text)
	}
	if m.activityUnread() != 0 {
		t.Fatalf("leaving Activity left %d unread", m.activityUnread())
	}
}

func TestActivityReplyOpensItsThread(t *testing.T) {
	m := newTest(t)
	press(m, "k", "k", "l")
	for j, a := range m.activity {
		if a.Kind == chat.ActReply {
			m.listSel[sideActivity] = j
			break
		}
	}
	a := m.activity[m.listSel[sideActivity]]
	press(m, "enter")
	if m.thread == nil || m.thread.root != a.RootTS || m.thread.msgs[m.thread.sel].TS != a.Msg.TS {
		t.Fatalf("reply %q not selected in its thread", a.Msg.Text)
	}
}

func TestQStepsBackLikeCtrlC(t *testing.T) {
	m := newTest(t)
	press(m, "l", "enter", "q")
	if m.thread != nil || m.quitting {
		t.Fatal("q did not close the thread first")
	}
	press(m, "q")
	if m.focus != paneSide || m.quitting {
		t.Fatal("q did not go back to the sidebar")
	}
	press(m, "q")
	if !m.quitting {
		t.Fatal("q in the sidebar did not ask")
	}
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}); !isQuit(cmd) {
		t.Fatal("q on the confirmation did not quit")
	}
}

func TestDoubleClickInThreadsList(t *testing.T) {
	m := newTest(t)
	press(m, "k", "k")
	m.render()
	for y, bi := range m.layout.chatMsg {
		if bi == 0 {
			for range 2 {
				run(m, tea.MouseClickMsg{X: m.mainX() + 3, Y: y + m.layout.top, Button: tea.MouseLeft})
			}
			break
		}
	}
	if m.thread == nil {
		t.Fatal("double click on a thread did not open it")
	}
}

func TestFoldSections(t *testing.T) {
	m := newTest(t)
	m.opts.Sections = []Section{
		{Name: "Noise", Channels: []string{"deploys", "random"}, Collapsed: true},
		{Name: "Direct messages", Collapsed: true},
	}
	s := screen(m)
	if !strings.Contains(s, "▸ Noise") || !strings.Contains(s, "▸ Direct messages") || !strings.Contains(s, "▾ Channels") {
		t.Fatalf("fold arrows:\n%s", s)
	}
	for _, it := range m.sideOrder() {
		if c := m.convs[it.conv]; it.kind == sideConv && m.folded("Noise") && (c.Name == "deploys" || c.Name == "random") && c.Unread == 0 {
			t.Fatalf("%s shows in a folded section", c.Name)
		}
	}
	m.goTo(t, "random") // the open one shows even folded
	if !strings.Contains(screen(m), "random") {
		t.Fatal("the open conversation is hidden")
	}
	press(m, "h", "space")
	if m.folded("Noise") || !strings.Contains(screen(m), "▾ Noise") {
		t.Fatal("space did not unfold")
	}
	press(m, "z", "a")
	if !m.folded("Noise") {
		t.Fatal("z a did not fold")
	}
	m.render()
	for y, r := range m.layout.side {
		if r <= rowSection {
			run(m, tea.MouseClickMsg{X: 2, Y: y + m.layout.top, Button: tea.MouseLeft})
			break
		}
	}
	if m.folded("Noise") {
		t.Fatal("click on the header did not unfold")
	}
	if err := validateSections([]Section{{Name: "a"}, {Name: "a"}}); err == nil {
		t.Fatal("duplicate section accepted")
	}
}

// sectioned is the demo with Slack's own sections, emoji and all.
type sectioned struct{ chat.Backend }

func (s sectioned) Conversations(ctx context.Context) ([]chat.Conversation, error) {
	cs, err := s.Backend.Conversations(ctx)
	for i := range cs {
		switch cs[i].Name {
		case "general", "random", "deploys":
			cs[i].Section, cs[i].SectionEmoji = "SALSA", "tomato"
		case "tools-team":
			cs[i].Section, cs[i].SectionEmoji = "LOGS", "sltty"
		}
	}
	return cs, err
}

func TestFoldFromTheHeader(t *testing.T) {
	m := newModel(sectioned{chat.NewFake()}, defaultKeymap(), Options{CustomEmoji: true})
	run(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	run(m, m.Init()())
	if s := screen(m); !strings.Contains(s, "▾ 🍅 SALSA") || strings.Contains(s, "tomato") {
		t.Fatalf("the section's emoji is not a character:\n%s", s)
	}
	m.goTo(t, "general")
	press(m, "k")
	if m.onSection != "SALSA" || m.current().Name != "general" {
		t.Fatalf("k from the first channel: on %q, showing %q", m.onSection, m.current().Name)
	}
	press(m, "space")
	if !m.folded("SALSA") || sideRow(m, "random") != "" || sideRow(m, "deploys") != "" {
		t.Fatalf("space on the header did not fold:\n%s", screen(m))
	}
	if sideRow(m, "general") == "" {
		t.Fatal("the open conversation hid under its folded header")
	}
	if r := strings.TrimSpace(sideRow(m, "SALSA")); !strings.Contains(r, "▸") || !strings.Contains(r, " 12 ") {
		t.Fatalf("folded header %q: no arrow or no unread count", r)
	}
	press(m, "j", "j")
	if m.onSection != "LOGS" {
		t.Fatalf("j past the open one: on %q", m.onSection)
	}
	press(m, "k", "k", "enter")
	if m.folded("SALSA") || sideRow(m, "random") == "" || m.focus != paneSide {
		t.Fatal("enter on the header did not unfold")
	}
	press(m, "j", "space") // from inside: the cursor goes up to the header
	if !m.folded("SALSA") || m.onSection != "SALSA" {
		t.Fatalf("space inside the section: folded %v, on %q", m.folded("SALSA"), m.onSection)
	}
	press(m, "l")
	press(m, "J") // next chat from the pane: no header stops
	if m.onSection != "" || m.current().Name == "general" {
		t.Fatalf("J went to %q / header %q", m.current().Name, m.onSection)
	}

	m.imgs.mode = img.Kitty // a custom section emoji is a picture
	for range 3 {
		if cmd := m.imageCmds(); cmd != nil {
			run(m, cmd())
		}
	}
	p := m.imgs.pics["emoji:sltty"]
	if p == nil || p.id == 0 || !strings.Contains(sideRow(m, "LOGS"), kittyColor(p.id)) && !strings.Contains(m.render(), kittyColor(p.id)) {
		t.Fatalf("no picture for the LOGS emoji: %+v", p)
	}
}

func TestEmojiAndMentionsRead(t *testing.T) {
	m := newTest(t)
	c := m.current()
	m.history[c.ID] = append(m.history[c.ID], chat.Message{TS: "99", User: "ana", Text: "ok :+1::skin-tone-3: :tada: :sltty:",
		At: m.now(), Reactions: []chat.Reaction{{Emoji: ":white_check_mark:", Count: 2}, {Emoji: ":party-parrot:", Count: 1}}})
	s := ansiStrip(screen(m))
	for _, want := range []string{"ok 👍🏼 🎉 :sltty:", "✅ 2", ":party-parrot: 1"} {
		if !strings.Contains(s, want) {
			t.Fatalf("no %q in:\n%s", want, s)
		}
	}
}

func TestToggleRead(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "deploys") // sidebar navigation only previews it
	if m.current().Unread == 0 {
		t.Fatal("sidebar navigation unexpectedly read the channel")
	}
	press(m, "enter") // explicitly enter the previewed conversation
	if m.current().Unread != 0 {
		t.Fatal("enter did not read")
	}
	press(m, "h", "u") // sidebar: unread from the last message
	if m.current().Unread != 1 || !strings.Contains(screen(m), "marked unread") {
		t.Fatalf("u in the sidebar: %d unread", m.current().Unread)
	}
	press(m, "u")
	if m.current().Unread != 0 {
		t.Fatal("second u did not read")
	}
	press(m, "l", "k", "k", "u") // chat: unread from the selected message on
	if m.current().Unread != 3 {
		t.Fatalf("u on the 3rd last message: %d unread", m.current().Unread)
	}
	press(m, "h", "k", "j", "enter") // explicitly reopen reads it in the backend too
	if m.current().Unread != 0 {
		t.Fatalf("reopened: %d", m.current().Unread)
	}
	cs, _ := m.backend.Conversations(t.Context())
	for _, c := range cs {
		if c.Name == "deploys" && c.Unread != 0 {
			t.Fatalf("backend still has %d unread", c.Unread)
		}
	}

	press(m, "h", "gg"[:1], "g", "l") // Threads list
	s := m.threads[0]
	before := m.threadUnread(s) > 0
	press(m, "u")
	if (m.threadUnread(s) > 0) == before {
		t.Fatal("u did not toggle the thread")
	}
	press(m, "u")
	if (m.threadUnread(s) > 0) != before {
		t.Fatal("u did not toggle the thread back")
	}

	press(m, "h", "j", "l") // Activity: mark one unread, it survives leaving
	press(m, "u", "u")
	a := m.activity[0]
	if !m.activityIsUnread(a) {
		t.Fatal("activity not unread after u u")
	}
	press(m, "h", "j")
	if !m.activityIsUnread(a) {
		t.Fatal("leaving Activity read what u marked unread")
	}
}

// slackLike marks the way Slack does: no unread count comes back.
type slackLike struct{ *chat.Fake }

func (slackLike) Mark(context.Context, string, string) error { return nil }

// sideRow is the sidebar line of a conversation.
func sideRow(m *model, name string) string {
	for _, l := range strings.Split(screen(m), "\n") {
		if side := []rune(l)[:min(m.side, len([]rune(l)))]; strings.Contains(string(side), name) {
			return string(side)
		}
	}
	return ""
}

func TestMarkUnreadWithoutABackendCount(t *testing.T) {
	m := newModel(slackLike{chat.NewFake()}, defaultKeymap(), Options{})
	run(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	run(m, m.Init()())
	m.goTo(t, "deploys")
	press(m, "l") // l enters it as enter does
	if m.current().Unread != 0 {
		t.Fatalf("l did not read: %d unread", m.current().Unread)
	}
	press(m, "k", "k", "u")
	if m.current().Unread != 3 {
		t.Fatalf("u on the 3rd last message: %d unread", m.current().Unread)
	}
	if s := screen(m); !strings.Contains(s, "─ New") {
		t.Fatalf("no New line after u:\n%s", s)
	}
	if r := sideRow(m, "deploys"); !strings.HasSuffix(strings.TrimSpace(r), "3") {
		t.Fatalf("no unread count in the sidebar: %q", r)
	}
	press(m, "enter", "esc") // its thread and back: still unread
	if m.current().Unread != 3 {
		t.Fatalf("back from the thread: %d unread", m.current().Unread)
	}
	press(m, "h", "l") // out and in again: read
	if m.current().Unread != 0 || !strings.Contains(screen(m), "─ New") {
		t.Fatalf("re-entering: %d unread, or the New line went", m.current().Unread)
	}
}

func TestNewLine(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "deploys") // 12 unread: the line sits above the 12th last
	h := m.history["C4"]
	if m.newFrom["C4"] != h[len(h)-12].TS {
		t.Fatalf("new from %q, want %q", m.newFrom["C4"], h[len(h)-12].TS)
	}
	m.goTo(t, "random")
	if _, ok := m.newFrom["C4"]; ok {
		t.Fatal("leaving did not clear the New line")
	}
	m.goTo(t, "general")
	if strings.Contains(screen(m), " New") {
		t.Fatal("a New line without unread messages")
	}
	press(m, "l", "k", "u") // marked unread from here: the line shows here
	if s := screen(m); !strings.Contains(s, "─ New") {
		t.Fatalf("no New line after u:\n%s", s)
	}
	press(m, "u")
	if strings.Contains(screen(m), "─ New") {
		t.Fatal("marking read kept the New line")
	}
}

func TestLoadingModalUntilTheConversationsCome(t *testing.T) {
	m := newModel(chat.NewFake(), defaultKeymap(), Options{})
	run(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	cmd := m.Init()
	s := screen(m)
	if !strings.Contains(s, "Loading conversations…") || !strings.Contains(s, "sltty (demo)") {
		t.Fatalf("no loading modal before the conversations:\n%s", s)
	}
	if !m.busy() {
		t.Fatal("the spinner would not turn while loading")
	}
	run(m, cmd())
	if s := screen(m); strings.Contains(s, "Loading conversations") {
		t.Fatalf("loading modal still up with the conversations in:\n%s", s)
	}
	if m.busy() {
		t.Fatal("still busy after loading")
	}
}

func TestUnreadVIPsGoFirst(t *testing.T) {
	m := newTest(t)
	for i := range m.convs {
		if m.convs[i].Name == "ana" || m.convs[i].Name == "bob" {
			m.convs[i].VIP = true // ana has an unread message, bob none
		}
	}
	first := func() section { return m.sections()[0] }
	if s := first(); s.name != vipSection || len(s.convs) != 1 || m.convs[s.convs[0]].Name != "ana" {
		t.Fatalf("first section: %q %v", s.name, s.convs)
	}
	if s := screen(m); strings.Index(s, "VIP") > strings.Index(s, "Channels") {
		t.Fatalf("VIP is not above the rest:\n%s", s)
	}
	// Read inside, it stays until left; then it goes back to its section.
	m.goTo(t, "ana")
	press(m, "l")
	if c := m.current(); c.Name != "ana" || c.Unread != 0 || first().name != vipSection {
		t.Fatalf("read inside: %q unread %d, first %q", c.Name, c.Unread, first().name)
	}
	press(m, "h", "j", "j") // the next section's header, then its first chat
	if m.current().Name == "ana" || first().name == vipSection {
		t.Fatalf("left: on %q, first %q", m.current().Name, first().name)
	}
	// With the config's own sections too.
	m.setUnread(m.convs[slices.IndexFunc(m.convs, func(c chat.Conversation) bool { return c.Name == "bob" })].ID, 2)
	m.opts.Sections = []Section{{Name: "Team", Channels: []string{"@bob", "tools-*"}}}
	if s := first(); s.name != vipSection || m.convs[s.convs[0]].Name != "bob" {
		t.Fatalf("custom sections: first %q %v", s.name, s.convs)
	}
}
