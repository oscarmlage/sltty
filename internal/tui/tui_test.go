package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/chat"
)

// quick runs cmd but gives up on slow ones (the cursor blink ticks): the
// fake backend answers at once.
func quick(cmd tea.Cmd) tea.Msg {
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

// run feeds msg to the model and then the backend replies it asks for,
// unwrapping batches.
func run(m *model, msg tea.Msg) {
	queue := []tea.Msg{msg}
	for len(queue) > 0 {
		msg, queue = queue[0], queue[1:]
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				if c != nil {
					queue = append(queue, quick(c))
				}
			}
			continue
		}
		_, cmd := m.Update(msg)
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
		case convsMsg, historyMsg, repliesMsg, sentMsg, changedMsg, markedMsg, pictureMsg, customEmojiMsg, presenceMsg, searchMsg, threadsMsg, activityMsg, clipboardMsg:
			queue = append(queue, out)
		}
	}
}

var named = map[string]tea.KeyPressMsg{
	"esc":       {Code: tea.KeyEscape},
	"enter":     {Code: tea.KeyEnter},
	"tab":       {Code: tea.KeyTab},
	"ctrl+k":    {Code: 'k', Mod: tea.ModCtrl},
	"ctrl+p":    {Code: 'p', Mod: tea.ModCtrl},
	"ctrl+f":    {Code: 'f', Mod: tea.ModCtrl},
	"ctrl+c":    {Code: 'c', Mod: tea.ModCtrl},
	"up":        {Code: tea.KeyUp},
	"backspace": {Code: tea.KeyBackspace},
	"space":     {Code: tea.KeySpace, Text: " "},
}

func press(m *model, keys ...string) {
	for _, k := range keys {
		msg, ok := named[k]
		if !ok {
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		run(m, msg)
	}
}

func typeText(m *model, s string) {
	for _, r := range s {
		run(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func newTest(t *testing.T) *model {
	t.Helper()
	m := newModel(chat.NewFake(), defaultKeymap(), Options{ConfigPath: "/tmp/x/config.json"})
	run(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	run(m, m.Init()())
	if len(m.convs) == 0 {
		t.Fatal("no conversations loaded")
	}
	m.render()
	return m
}

func screen(m *model) string { return ansi.Strip(m.render()) }

func (m *model) goTo(t *testing.T, name string) {
	t.Helper()
	for i, c := range m.convs {
		if c.Name == name {
			if cmd := m.selectConv(i); cmd != nil {
				run(m, cmd())
			}
			return
		}
	}
	t.Fatalf("no conversation %q", name)
}

func TestSidebarSectionsChannelsFirst(t *testing.T) {
	m := newTest(t)
	s := screen(m)
	ch, dm := strings.Index(s, "Channels"), strings.Index(s, "Direct messages")
	if ch < 0 || dm < ch {
		t.Fatalf("sections missing or out of order:\n%s", s)
	}
	for _, c := range m.convs[:4] {
		if c.Kind != chat.Channel {
			t.Fatalf("a DM before the channels: %+v", c)
		}
	}
}

func TestMessageCursorAndThread(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "general")
	press(m, "l")
	if m.focus != paneChat || m.selected() != len(m.history["C1"])-1 {
		t.Fatalf("focus %v selected %d", m.focus, m.selected())
	}
	for !strings.Contains(m.selectedMsg().Text, "staging") && m.selected() > 0 {
		press(m, "k") // up to the staging runner message, which has a thread
	}
	if got := m.selectedMsg().Text; !strings.Contains(got, "runner") {
		t.Fatalf("selected %q", got)
	}
	press(m, "enter")
	if m.thread == nil || m.focus != paneThread || len(m.thread.msgs) != 4 {
		t.Fatalf("thread %+v focus %v", m.thread, m.focus)
	}
	if s := screen(m); !strings.Contains(s, "3 replies") || !strings.Contains(s, "out of memory") {
		t.Fatalf("thread not on screen:\n%s", s)
	}
	press(m, "i")
	typeText(m, "thanks Carol")
	press(m, "enter", "esc")
	if n := len(m.thread.msgs); n != 5 || m.thread.msgs[4].Text != "thanks Carol" {
		t.Fatalf("reply not in the thread: %d messages", n)
	}
	if got := m.history["C1"][m.selected()].Replies; got != 4 {
		t.Fatalf("root shows %d replies, want 4", got)
	}
	if strings.Contains(m.history["C1"][len(m.history["C1"])-1].Text, "gracias") {
		t.Fatal("thread reply went to the channel")
	}
	press(m, "esc")
	if m.thread != nil || m.focus != paneChat {
		t.Fatal("esc did not close the thread")
	}
}

func TestReactionDetailsKey(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "random")
	press(m, "l", "R")
	if !m.help || m.sheet == nil || m.sheet.title != "Reactions" {
		t.Fatalf("R did not open reaction details: %+v", m.sheet)
	}
}

func TestSendToChannelFollowsBottom(t *testing.T) {
	m := newTest(t)
	press(m, "i")
	typeText(m, "hola qué tal")
	press(m, "enter")
	h := m.history["C1"]
	if h[len(h)-1].Text != "hola qué tal" || m.selected() != len(h)-1 {
		t.Fatalf("last %q selected %d of %d", h[len(h)-1].Text, m.selected(), len(h))
	}
	if !strings.Contains(screen(m), "hola qué tal") {
		t.Fatal("sent message not on screen")
	}
}

func TestEmptyMessageNotSent(t *testing.T) {
	m := newTest(t)
	before := len(m.history["C1"])
	press(m, "i")
	typeText(m, "   ")
	press(m, "enter")
	if len(m.history["C1"]) != before {
		t.Fatal("blank message was sent")
	}
}

func TestDraftsPerChat(t *testing.T) {
	m := newTest(t)
	press(m, "i")
	typeText(m, "borrador")
	press(m, "esc", "J")
	if m.chatIn.Value() != "" {
		t.Fatalf("draft leaked: %q", m.chatIn.Value())
	}
	press(m, "K")
	if m.chatIn.Value() != "borrador" {
		t.Fatalf("draft lost: %q", m.chatIn.Value())
	}
}

func TestSelectionStaysOnScreen(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "deploys")
	press(m, "l", "g", "g")
	if m.selected() != 0 {
		t.Fatalf("gg: selected %d", m.selected())
	}
	if !strings.Contains(screen(m), "deploy #1001 ") {
		t.Fatal("first message not on screen after gg")
	}
	press(m, "G")
	if !strings.Contains(screen(m), "deploy #1040 ") {
		t.Fatal("last message not on screen after G")
	}
}

func TestSwitcher(t *testing.T) {
	m := newTest(t)
	press(m, "ctrl+k")
	if m.picker == nil || m.picker.kind != pickSwitch {
		t.Fatal("ctrl+k did not open the switcher")
	}
	typeText(m, "crl") // fuzzy: c-a-r-o-l
	if c := m.convs[m.picker.items[0].conv]; c.Name != "carol" {
		t.Fatalf("best match %q", c.Name)
	}
	press(m, "enter")
	if m.picker != nil || m.current().Name != "carol" || m.focus != paneChat {
		t.Fatalf("after enter: %v %q", m.picker != nil, m.current().Name)
	}
}

func TestLinesFitTheScreen(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "tools-team")
	press(m, "l", "enter", "ctrl+k")
	out := m.render()
	for i, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w != m.width {
			t.Fatalf("line %d is %d wide, want %d: %q", i, w, m.width, ansi.Strip(l))
		}
	}
	if n := strings.Count(out, "\n") + 1; n != m.height {
		t.Fatalf("%d lines, want %d", n, m.height)
	}
}

func TestResizeKeysClamp(t *testing.T) {
	m := newTest(t)
	for range 50 {
		press(m, ">")
	}
	if m.mainWidth() != minMain {
		t.Fatalf("conversation %d wide, want %d", m.mainWidth(), minMain)
	}
	for range 50 {
		press(m, "<")
	}
	if m.side != minSide {
		t.Fatalf("sidebar %d, want %d", m.side, minSide)
	}
}

func TestDragDividers(t *testing.T) {
	m := newTest(t)
	run(m, tea.MouseClickMsg{X: m.side, Y: 5, Button: tea.MouseLeft})
	run(m, tea.MouseMotionMsg{X: 40, Y: 5, Button: tea.MouseLeft})
	run(m, tea.MouseReleaseMsg{X: 40, Y: 5, Button: tea.MouseLeft})
	if m.drag != 0 || m.side != 40 {
		t.Fatalf("sidebar drag: drag %d side %d", m.drag, m.side)
	}
	press(m, "l", "enter") // open a thread
	x := m.threadX() - 1
	run(m, tea.MouseClickMsg{X: x, Y: 5, Button: tea.MouseLeft})
	run(m, tea.MouseMotionMsg{X: x + 10, Y: 5, Button: tea.MouseLeft})
	run(m, tea.MouseReleaseMsg{X: x + 10, Y: 5, Button: tea.MouseLeft})
	if m.threadX()-1 != x+10 {
		t.Fatalf("thread divider at %d, want %d", m.threadX()-1, x+10)
	}
	if m.mainWidth() < minMain {
		t.Fatalf("conversation squeezed to %d", m.mainWidth())
	}
}

func TestClickSelectsChatAndMessage(t *testing.T) {
	m := newTest(t)
	press(m, "l")
	for y, ci := range m.layout.side {
		if ci >= 0 && m.convs[ci].Name == "random" {
			run(m, tea.MouseClickMsg{X: 3, Y: y, Button: tea.MouseLeft})
			break
		}
	}
	if m.current().Name != "random" || m.focus != paneSide {
		t.Fatalf("click on the sidebar: %q focus %v", m.current().Name, m.focus)
	}
	m.render()
	for y, mi := range m.layout.chatMsg {
		if mi == 0 {
			run(m, tea.MouseClickMsg{X: m.mainX() + 3, Y: y, Button: tea.MouseLeft})
			break
		}
	}
	if m.selected() != 0 || m.focus != paneChat {
		t.Fatalf("click on a message: selected %d focus %v", m.selected(), m.focus)
	}
	run(m, tea.MouseClickMsg{X: m.mainX() + 3, Y: m.layout.composerTop + 1, Button: tea.MouseLeft})
	if m.mode != insert {
		t.Fatal("click on the composer did not enter INSERT")
	}
}
