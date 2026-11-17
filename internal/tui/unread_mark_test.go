package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
)

// marker is a Fake that marks conversations unread as Telegram does.
type marker struct {
	*chat.Fake
	marks []bool
}

func (k *marker) MarkUnread(_ context.Context, _ string, unread bool) error {
	k.marks = append(k.marks, unread)
	return nil
}

func TestMarkUnreadOnTheServer(t *testing.T) {
	k := &marker{Fake: chat.NewFake()}
	m := newModel(k, defaultKeymap(), Options{})
	run(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	run(m, m.Init()())
	m.goTo(t, "deploys")
	press(m, "enter") // read it
	if m.current().Unread != 0 {
		t.Fatal("enter did not read")
	}
	press(m, "l", "k", "k", "u") // on a message: still the whole chat
	if m.current().Unread != 1 || len(k.marks) != 1 || !k.marks[0] {
		t.Fatalf("u: %d unread, marks %v", m.current().Unread, k.marks)
	}
	press(m, "u") // and read again
	if m.current().Unread != 0 {
		t.Fatal("second u did not read")
	}
	// Marked or unmarked on another device.
	id := m.current().ID
	m.live(chat.Event{Kind: chat.EvUnreadMark, ConvID: id, Unread: 1})
	if m.current().Unread != 1 {
		t.Fatal("a mark from elsewhere does not show")
	}
	m.live(chat.Event{Kind: chat.EvUnreadMark, ConvID: id})
	if m.current().Unread != 0 {
		t.Fatal("an unmark from elsewhere does not clear it")
	}
}
