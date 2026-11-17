package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
)

// counter is a Fake polled as Slack is: counts set by the test, history
// calls counted, and maybe a conversation the sidebar does not have yet.
type counter struct {
	*chat.Fake
	counts    map[string]int
	histories map[string]int
	extra     []chat.Conversation
}

func (c *counter) UnreadCounts(context.Context) (map[string]int, error) { return c.counts, nil }

func (c *counter) History(ctx context.Context, id string) ([]chat.Message, error) {
	c.histories[id]++
	return c.Fake.History(ctx, id)
}

func (c *counter) Conversations(ctx context.Context) ([]chat.Conversation, error) {
	cs, err := c.Fake.Conversations(ctx)
	return append(cs, c.extra...), err
}

func newPolled(t *testing.T) (*model, *counter) {
	t.Helper()
	c := &counter{Fake: chat.NewFake(), counts: map[string]int{}, histories: map[string]int{}}
	m := newModel(c, defaultKeymap(), Options{})
	run(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	run(m, m.Init()())
	if m.pollEvery() != defaultPoll {
		t.Fatalf("poll every %v", m.pollEvery())
	}
	// What the sidebar has now is what the server says.
	for _, cv := range m.convs {
		c.counts[cv.ID] = cv.Unread
	}
	return m, c
}

func TestPollBringsNewUnreads(t *testing.T) {
	m, c := newPolled(t)
	open := m.current().ID
	var other string
	for _, cv := range m.convs {
		if cv.ID != open && cv.Kind == chat.Channel {
			other = cv.ID
			break
		}
	}
	m.history[other] = []chat.Message{{TS: "1", Text: "stale"}}
	c.counts[other] += 2
	c.counts[open]++
	before := c.histories[open]
	applyCounts(m, c.counts)
	if cv, _ := m.convByID(other); cv.Unread != c.counts[other] {
		t.Fatalf("%s: %d unread, want %d", other, cv.Unread, c.counts[other])
	}
	if _, ok := m.history[other]; ok {
		t.Fatal("a conversation with new messages keeps its old history")
	}
	if c.histories[open] != before+1 {
		t.Fatal("the open conversation did not load its new messages")
	}
}

func TestPollRespectsWhatWasJustDone(t *testing.T) {
	m, c := newPolled(t)
	id := m.current().ID
	m.marked[id] = time.Now()
	c.counts[id] = 9
	applyCounts(m, c.counts)
	if cv, _ := m.convByID(id); cv.Unread == 9 {
		t.Fatal("a count older than the user's read replaced it")
	}
	// Marked unread here: Slack says 0 but it stays unread.
	delete(m.marked, id)
	m.keepUnread[id] = true
	m.setUnread(id, 2)
	c.counts[id] = 0
	applyCounts(m, c.counts)
	if cv, _ := m.convByID(id); cv.Unread != 2 {
		t.Fatal("a poll undid u")
	}
}

func TestPollAddsNewConversations(t *testing.T) {
	m, c := newPolled(t)
	c.extra = []chat.Conversation{{ID: "DNEW", Name: "newcomer", Kind: chat.DM, Unread: 1}}
	c.counts["DNEW"] = 1
	cur := m.current().ID
	applyCounts(m, c.counts)
	if _, ok := m.convByID("DNEW"); !ok {
		t.Fatal("the new conversation is not in the sidebar")
	}
	if m.current().ID != cur {
		t.Fatal("adding a conversation moved the selection")
	}
}

func TestReloadKeepsTheSelection(t *testing.T) {
	m, _ := newPolled(t)
	id := m.current().ID
	h := m.history[id]
	if len(h) < 3 {
		t.Skip("demo history too short")
	}
	m.sel[id] = 1
	ts := h[1].TS
	run(m, historyMsg{id: id, msgs: append([]chat.Message{}, h[1:]...)}) // the oldest dropped off
	if got := m.history[id][m.selected()].TS; got != ts {
		t.Fatalf("selection moved to %s, want %s", got, ts)
	}
}

func TestNoPollWithoutCountsOrWhenLive(t *testing.T) {
	m := newTest(t) // the plain Fake has no counts
	if m.pollEvery() != 0 {
		t.Fatal("polling a backend that cannot count")
	}
	m2, _ := newPolled(t)
	m2.opts.PollInterval = -1
	if m2.pollEvery() != 0 {
		t.Fatal("poll_seconds -1 still polls")
	}
}

// applyCounts runs a poll's answer and what it asks for.
func applyCounts(m *model, counts map[string]int) {
	if cmd := m.gotCounts(countsMsg{counts: counts}); cmd != nil {
		run(m, tea.BatchMsg{cmd})
	}
}
