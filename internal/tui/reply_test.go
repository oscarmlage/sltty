package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
)

// replier is a Fake that replies Telegram style.
type replier struct {
	*chat.Fake
	got []string // "conv ts text"
}

func (r *replier) Reply(_ context.Context, conv, ts, text string) (chat.Message, error) {
	r.got = append(r.got, conv+" "+ts+" "+text)
	return chat.Message{TS: "999", User: "you", Text: text}, nil
}

func TestEnterRepliesOnTelegram(t *testing.T) {
	r := &replier{Fake: chat.NewFake()}
	m := newModel(r, defaultKeymap(), Options{})
	run(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	run(m, m.Init()())
	press(m, "l") // into the messages
	sel := m.selectedMsg()
	press(m, "enter")
	if m.mode != insert || m.replying == nil || m.replying.ts != sel.TS || m.thread != nil {
		t.Fatalf("enter did not start a reply: mode %v replying %+v thread %v", m.mode, m.replying, m.thread != nil)
	}
	if !strings.Contains(screen(m), "↩ "+sel.User) {
		t.Fatal("the composer does not say what it answers")
	}
	typeText(m, "claro")
	press(m, "enter")
	id := m.current().ID
	if len(r.got) != 1 || r.got[0] != id+" "+sel.TS+" claro" {
		t.Fatalf("replies %v", r.got)
	}
	h := m.history[id]
	if h[len(h)-1].Text != "claro" || m.replying != nil {
		t.Fatal("the reply is not in the chat")
	}
	// What it answers is drawn above it, as Telegram does: the bar, who,
	// then the beginning of the message.
	s := screen(m)
	who, said, reply := strings.Index(s, "▎ "+sel.User), strings.Index(s, "▎ "+strings.Fields(sel.Text)[0]), strings.LastIndex(s, "claro")
	if who < 0 || said < 0 || !(who < said && said < reply) {
		t.Fatalf("no quote above the reply:\n%s", s)
	}
	// esc drops a reply; t still opens the thread.
	press(m, "esc", "enter", "esc")
	if m.replying != nil || m.mode != normal {
		t.Fatal("esc did not cancel the reply")
	}
	press(m, "t")
	if m.thread == nil {
		t.Fatal("t did not open the thread")
	}
}

func TestEnterOpensTheThreadOnSlack(t *testing.T) {
	m := newTest(t)
	press(m, "l", "enter")
	if m.thread == nil || m.replying != nil {
		t.Fatal("enter on Slack must open the thread")
	}
}
