package tui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
)

// liveFake is the demo backend with an events channel. The tests deliver
// events with run(m, eventMsg(...)); the channel itself stays silent.
type liveFake struct {
	*chat.Fake
	ch chan chat.Event
}

func (l liveFake) Events() <-chan chat.Event { return l.ch }

// newLiveTest is the demo with Telegram-like sections (most recent first).
func newLiveTest(t *testing.T) *model {
	t.Helper()
	m := newModel(liveFake{chat.NewFake(), make(chan chat.Event)}, defaultKeymap(), Options{ConfigPath: "/tmp/x/config.json"})
	run(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	run(m, m.Init()())
	if m.events == nil {
		t.Fatal("a Live backend's events are not listened to")
	}
	for i := range m.convs {
		m.convs[i].Section = "Chats"
	}
	return m
}

func ev(e chat.Event) eventMsg { return eventMsg(e) }

func TestLiveMessageInOpenConversation(t *testing.T) {
	m := newLiveTest(t)
	m.goTo(t, "general")
	press(m, "l") // into the messages: what arrives is read
	n := len(m.history["C1"])
	run(m, ev(chat.Event{Kind: chat.EvMessage, ConvID: "C1", Msg: chat.Message{TS: "900", User: "ana", Text: "live hello", At: time.Now()}}))
	if len(m.history["C1"]) != n+1 || m.history["C1"][n].Text != "live hello" {
		t.Fatalf("not appended: %d", len(m.history["C1"]))
	}
	if c, _ := m.convByID("C1"); c.Unread != 0 {
		t.Fatalf("read where the user is: unread %d", c.Unread)
	}
	if !strings.Contains(screen(m), "live hello") {
		t.Fatal("not on screen")
	}
	// The same message again (this client sent it) is not repeated.
	run(m, ev(chat.Event{Kind: chat.EvMessage, ConvID: "C1", Msg: chat.Message{TS: "900", User: "ana", Text: "live hello (2)"}}))
	if len(m.history["C1"]) != n+1 || m.history["C1"][n].Text != "live hello (2)" {
		t.Fatal("a known message must be replaced")
	}
}

func TestLiveMessageElsewhereCountsAndBumps(t *testing.T) {
	m := newLiveTest(t)
	m.goTo(t, "general")
	before, _ := m.convByID("D2")
	run(m, ev(chat.Event{Kind: chat.EvMessage, ConvID: "D2", Msg: chat.Message{TS: "901", User: "bob", Text: "psst"}}))
	c, _ := m.convByID("D2")
	if c.Unread != before.Unread+1 {
		t.Fatalf("unread %d, want %d", c.Unread, before.Unread+1)
	}
	if m.convs[0].ID != "D2" {
		t.Fatalf("not moved to the top: %s", m.convs[0].Name)
	}
	if m.current().ID != "C1" {
		t.Fatalf("the cursor moved off general to %s", m.current().Name)
	}
	if !strings.Contains(m.status, "bob") {
		t.Fatalf("status %q", m.status)
	}
	// Read on another device.
	run(m, ev(chat.Event{Kind: chat.EvRead, ConvID: "D2", Unread: 0}))
	if c, _ := m.convByID("D2"); c.Unread != 0 {
		t.Fatal("read elsewhere still unread")
	}
}

func TestLiveNewConversation(t *testing.T) {
	m := newLiveTest(t)
	m.goTo(t, "random")
	conv := chat.Conversation{ID: "D9", Name: "zoe", Kind: chat.DM, Section: "Chats"}
	run(m, ev(chat.Event{Kind: chat.EvMessage, ConvID: "D9", Conv: &conv, Msg: chat.Message{TS: "1", User: "zoe", Text: "hi"}}))
	if c, ok := m.convByID("D9"); !ok || c.Unread != 1 {
		t.Fatalf("new conversation: %+v %v", c, ok)
	}
	if m.current().Name != "random" {
		t.Fatalf("the cursor moved to %s", m.current().Name)
	}
}

func TestLiveEditReactDelete(t *testing.T) {
	m := newLiveTest(t)
	m.goTo(t, "general")
	h := m.history["C1"]
	ts := h[len(h)-1].TS
	edited := h[len(h)-1]
	edited.Text = "edited elsewhere"
	run(m, ev(chat.Event{Kind: chat.EvEdited, ConvID: "C1", Msg: edited}))
	if m.history["C1"][len(h)-1].Text != "edited elsewhere" {
		t.Fatal("edit not applied")
	}
	run(m, ev(chat.Event{Kind: chat.EvReactions, ConvID: "C1", Msg: chat.Message{TS: ts, Reactions: []chat.Reaction{{Emoji: "🔥", Count: 3}}}}))
	if r := m.history["C1"][len(h)-1].Reactions; len(r) != 1 || r[0].Emoji != "🔥" {
		t.Fatalf("reactions: %+v", r)
	}
	run(m, ev(chat.Event{Kind: chat.EvDeleted, ConvID: "C1", TS: []string{ts}}))
	if len(m.history["C1"]) != len(h)-1 {
		t.Fatal("delete not applied")
	}
	run(m, ev(chat.Event{Kind: chat.EvPresence, ConvID: "D2", Online: true}))
	if c, _ := m.convByID("D2"); !c.Online {
		t.Fatal("presence not applied")
	}
}

func TestLiveThreadReply(t *testing.T) {
	m := newLiveTest(t)
	m.goTo(t, "tools-team")
	var root chat.Message
	for _, x := range m.history["C2"] {
		if x.Replies > 0 {
			root = x
		}
	}
	if root.TS == "" {
		t.Fatal("the demo has no thread in tools-team")
	}
	run(m, m.openThreadAt("C2", root.TS)())
	n := len(m.thread.msgs)
	run(m, ev(chat.Event{Kind: chat.EvMessage, ConvID: "C2", RootTS: root.TS, Msg: chat.Message{TS: "950", User: "ana", Text: "in the thread"}}))
	if len(m.thread.msgs) != n+1 {
		t.Fatalf("thread: %d messages, want %d", len(m.thread.msgs), n+1)
	}
	for _, x := range m.history["C2"] {
		if x.TS == root.TS && x.Replies != root.Replies+1 {
			t.Fatalf("root replies %d", x.Replies)
		}
	}
}

func TestLiveBackendDoesNotPoll(t *testing.T) {
	m := newLiveTest(t)
	m.goTo(t, "general")
	_, cmd := m.Update(sentMsg{id: "C1", msg: chat.Message{TS: "777", User: "you", Text: "x"}})
	if cmd != nil {
		if _, ok := quick(cmd).(replyPollMsg); ok {
			t.Fatal("a live backend must not poll after sending")
		}
	}
}

func TestLiveTypingShowsAndExpires(t *testing.T) {
	m := newLiveTest(t)
	m.goTo(t, "general")
	now := time.Now()
	m.now = func() time.Time { return now }
	run(m, ev(chat.Event{Kind: chat.EvTyping, ConvID: "C1", User: "ana", Action: "typing"}))
	if s := screen(m); !strings.Contains(s, "ana is typing…") {
		t.Fatalf("no typing in the header:\n%s", s)
	}
	run(m, ev(chat.Event{Kind: chat.EvTyping, ConvID: "D2", User: "bob", Action: "typing"}))
	if !strings.Contains(screen(m), "✎") {
		t.Fatal("no typing mark in the sidebar")
	}
	run(m, ev(chat.Event{Kind: chat.EvTyping, ConvID: "C1", User: "carol", Action: "typing"}))
	if m.typingText("C1") != "ana and carol are typing…" {
		t.Fatalf("two: %q", m.typingText("C1"))
	}
	// Their message ends it; time ends the rest.
	run(m, ev(chat.Event{Kind: chat.EvMessage, ConvID: "C1", Msg: chat.Message{TS: "990", User: "ana", Text: "done"}}))
	if m.typingText("C1") != "carol is typing…" {
		t.Fatalf("after ana's message: %q", m.typingText("C1"))
	}
	now = now.Add(typingFor + time.Second)
	run(m, typingTick{})
	if m.typingText("C1") != "" || len(m.typing) != 0 {
		t.Fatal("typing did not expire")
	}
}

// typerFake records Typing calls.
type typerFake struct {
	liveFake
	calls *[]string
}

func (f typerFake) Typing(_ context.Context, conv, root string) error {
	*f.calls = append(*f.calls, conv+"/"+root)
	return nil
}

func TestTypingIsSentAtMostEveryFewSeconds(t *testing.T) {
	var calls []string
	m := newModel(typerFake{liveFake{chat.NewFake(), make(chan chat.Event)}, &calls}, defaultKeymap(), Options{ConfigPath: "/tmp/x/config.json"})
	run(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	run(m, m.Init()())
	m.goTo(t, "general")
	now := time.Now()
	m.now = func() time.Time { return now }
	press(m, "i")
	typeText(m, "hello")
	waitFor(t, func() bool { return len(calls) == 1 })
	now = now.Add(6 * time.Second)
	typeText(m, "!")
	waitFor(t, func() bool { return len(calls) == 2 })
	if calls[0] != "C1/" {
		t.Fatalf("calls %v", calls)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for range 50 {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func TestPinnedStaysArchivedFoldsAndComesBack(t *testing.T) {
	m := newLiveTest(t)
	for i := range m.convs {
		switch m.convs[i].ID {
		case "C1":
			m.convs[i].Section, m.convs[i].Pinned = "Pinned", true
		case "D3":
			m.convs[i].Section, m.convs[i].Folded = "Archived", true
		}
	}
	// Pinned first, archived last, as the backend sends them.
	slices.SortStableFunc(m.convs, func(a, b chat.Conversation) int {
		rank := map[string]int{"Pinned": 0, "Chats": 1, "Archived": 2}
		return rank[a.Section] - rank[b.Section]
	})
	m.goTo(t, "random")
	if strings.Contains(screen(m), "carol") {
		t.Fatal("an archived chat shows in the folded section")
	}
	if !strings.Contains(screen(m), "Archived") {
		t.Fatal("no Archived section")
	}
	run(m, ev(chat.Event{Kind: chat.EvMessage, ConvID: "C1", Msg: chat.Message{TS: "1", User: "ana", Text: "x"}}))
	if m.convs[0].ID != "C1" {
		t.Fatal("pinned moved")
	}
	run(m, ev(chat.Event{Kind: chat.EvMessage, ConvID: "D2", Msg: chat.Message{TS: "2", User: "bob", Text: "y"}}))
	if m.convs[1].ID != "D2" {
		t.Fatalf("not at the top of Chats: %v", m.convs[1].Name)
	}
	back := chat.Conversation{ID: "D3", Name: "carol", Kind: chat.DM, Section: "Chats"}
	run(m, ev(chat.Event{Kind: chat.EvMessage, ConvID: "D3", Conv: &back, Msg: chat.Message{TS: "3", User: "carol", Text: "z"}}))
	if c, _ := m.convByID("D3"); c.Section != "Chats" || m.convs[1].ID != "D3" || !strings.Contains(screen(m), "carol") {
		t.Fatalf("not back from Archived: %+v", c)
	}
	if m.current().Name != "random" {
		t.Fatalf("cursor moved to %s", m.current().Name)
	}
}

func TestMutedBadgeAndCompactCounts(t *testing.T) {
	if count(70934) != "70K" || count(3303) != "3.3K" || count(1000) != "1K" || count(42) != "42" {
		t.Fatal(count(70934), count(3303), count(1000))
	}
	m := newLiveTest(t)
	for i := range m.convs {
		if m.convs[i].ID == "C4" {
			m.convs[i].Muted, m.convs[i].Unread = true, 70934
		}
	}
	if !strings.Contains(screen(m), "70K") {
		t.Fatal("no compact count")
	}
}

func TestThreadlessHidesThreads(t *testing.T) {
	m := newModel(threadless{chat.NewFake()}, defaultKeymap(), Options{ConfigPath: "/tmp/x/config.json"})
	run(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	run(m, m.Init()())
	if strings.Contains(screen(m), "Threads") {
		t.Fatal("the Threads view shows")
	}
	if m.sideOrder()[0].kind != sideActivity {
		t.Fatal("j/k still walk Threads")
	}
}

type threadless struct{ *chat.Fake }

func (threadless) NoThreadsView() {}

func TestSkimmingTheSidebarLoadsOnlyWhereItRests(t *testing.T) {
	m := newLiveTest(t)
	delete(m.history, "C3")
	delete(m.history, "C7")
	// Passing by C3: by the time its delay ends the cursor is elsewhere.
	i3 := slices.IndexFunc(m.convs, func(c chat.Conversation) bool { return c.ID == "C3" })
	i7 := slices.IndexFunc(m.convs, func(c chat.Conversation) bool { return c.ID == "C7" })
	m.selectConv(i3)
	m.selectConv(i7)
	run(m, loadSoonMsg{"C3"})
	if _, ok := m.history["C3"]; ok {
		t.Fatal("a chat passed by was loaded")
	}
	run(m, loadSoonMsg{"C7"})
	if _, ok := m.history["C7"]; !ok {
		t.Fatal("the chat the cursor rests on was not loaded")
	}
}

func TestRateLimitedHistoryRetriesQuietly(t *testing.T) {
	m := newLiveTest(t)
	_, cmd := m.Update(historyMsg{id: "C1", err: &chat.WaitError{Method: "messages.getHistory", Wait: time.Millisecond}})
	if m.err != "" || !strings.Contains(m.status, "slow down") {
		t.Fatalf("err %q status %q", m.err, m.status)
	}
	if _, ok := cmd().(loadSoonMsg); !ok {
		t.Fatal("no retry scheduled")
	}
	if !m.connected {
		t.Fatal("rate limited shown as disconnected")
	}
}
