package tui

// Polling, for backends without live updates (Slack here: no socket). Every
// pollEvery the unread counts are asked for in one call; a conversation
// with new messages gets its history again if it is the open one (and is
// read, if the user is in it), or is fetched afresh when opened. Threads and
// Activity are refreshed every few polls, and the sidebar when a
// conversation it does not have shows up.
//
// refresh (ctrl+r) asks for all of it now, for any backend; and a poll that
// comes much later than due (the laptop slept: the timer does not count
// that time) does the same, since a whole night of news is more than the
// unread counts can tell.

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
)

const (
	defaultPoll = 20 * time.Second
	// markGrace ignores a polled count for a conversation the user has just
	// read or marked: the answer may predate it.
	markGrace = 15 * time.Second
	// listsEvery refreshes Threads and Activity every so many polls.
	listsEvery = 3
	// wokeAfter is how many intervals late a poll must be to count as a
	// wake from sleep.
	wokeAfter = 3
)

type (
	pollTick  struct{}
	countsMsg struct {
		counts map[string]int
		err    error
	}
	// convsRefreshMsg is the sidebar again: only the conversations it did
	// not have are added.
	convsRefreshMsg struct {
		convs []chat.Conversation
		err   error
		// counts takes the unread counts from convs too (refresh, on a
		// backend that has no separate call for them).
		counts bool
	}
)

// pollEvery is how often to poll, 0 for never (a live backend).
func (m *model) pollEvery() time.Duration {
	if _, live := m.backend.(chat.Live); live {
		return 0
	}
	if _, ok := m.backend.(chat.Counter); !ok {
		return 0
	}
	if m.opts.PollInterval < 0 {
		return 0
	}
	if m.opts.PollInterval > 0 {
		return m.opts.PollInterval
	}
	return defaultPoll
}

func (m *model) schedulePoll() tea.Cmd {
	d := m.pollEvery()
	if d == 0 {
		return nil
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return pollTick{} })
}

func (m *model) poll() tea.Cmd {
	c, ok := m.backend.(chat.Counter)
	if !ok {
		return nil
	}
	// Wall clock (Round(0) drops the monotonic reading, which stops while
	// the machine sleeps): a poll far later than due follows a sleep.
	now := m.now().Round(0)
	last := m.lastPoll
	m.lastPoll = now
	if d := m.pollEvery(); d > 0 && !last.IsZero() && now.Sub(last) > wokeAfter*d {
		return tea.Batch(m.refresh(), m.schedulePoll())
	}
	m.polls++
	cmds := []tea.Cmd{m.call(func(ctx context.Context) tea.Msg {
		n, err := c.UnreadCounts(ctx)
		return countsMsg{n, err}
	}), m.schedulePoll()}
	if m.polls%listsEvery == 0 {
		cmds = append(cmds, m.loadThreads(), m.loadActivity())
	}
	// The thread on screen gets its new replies.
	if t := m.thread; t != nil && t.msgs != nil {
		cmds = append(cmds, m.loadReplies(t.convID, t.root))
	}
	return tea.Batch(cmds...)
}

// gotCounts applies polled counts. A count that went up means new
// messages: the open conversation loads them, the others drop their
// history so opening them fetches it again.
func (m *model) gotCounts(msg countsMsg) tea.Cmd {
	if msg.err != nil {
		return nil // quiet: the next poll tries again
	}
	now := m.now()
	open := ""
	if c := m.current(); c != nil && m.view == sideConv {
		open = c.ID
	}
	var cmds []tea.Cmd
	known := map[string]bool{}
	for i := range m.convs {
		c := &m.convs[i]
		known[c.ID] = true
		n, ok := msg.counts[c.ID]
		if !ok {
			n = 0 // client.counts leaves read conversations out
		}
		if at, ok := m.marked[c.ID]; ok && now.Sub(at) < markGrace {
			continue
		}
		if m.keepUnread[c.ID] {
			if n == 0 {
				continue // marked unread here: Slack cannot keep it
			}
			delete(m.keepUnread, c.ID)
		}
		if n == c.Unread {
			continue
		}
		grew := n > c.Unread
		c.Unread = n
		if !grew {
			continue
		}
		if c.ID == open {
			cmds = append(cmds, m.loadHistory(c.ID))
		} else {
			delete(m.history, c.ID)
		}
	}
	for id, n := range msg.counts {
		if n > 0 && !known[id] {
			// A conversation the sidebar does not have (a new DM...).
			cmds = append(cmds, m.call(func(ctx context.Context) tea.Msg {
				c, err := m.backend.Conversations(ctx)
				return convsRefreshMsg{convs: c, err: err}
			}))
			break
		}
	}
	return tea.Batch(cmds...)
}

func (m *model) gotConvsRefresh(msg convsRefreshMsg) tea.Cmd {
	if msg.err != nil {
		return nil
	}
	have := map[string]bool{}
	for _, c := range m.convs {
		have[c.ID] = true
	}
	counts := map[string]int{}
	for _, c := range msg.convs {
		if !have[c.ID] {
			m.addConv(c)
		}
		if c.Unread > 0 {
			counts[c.ID] = c.Unread
		}
	}
	if !msg.counts {
		return nil
	}
	return m.gotCounts(countsMsg{counts: counts})
}

// refresh loads what the account shows again, now: the sidebar (new
// conversations and unread counts), the open conversation and thread,
// Threads and Activity. The other conversations drop their history, so
// opening one fetches it afresh. It does not touch the poll's schedule.
func (m *model) refresh() tea.Cmd {
	c, counter := m.backend.(chat.Counter)
	cmds := []tea.Cmd{m.call(func(ctx context.Context) tea.Msg {
		cs, err := m.backend.Conversations(ctx)
		return convsRefreshMsg{convs: cs, err: err, counts: !counter}
	}), m.loadThreads(), m.loadActivity()}
	if counter {
		cmds = append(cmds, m.call(func(ctx context.Context) tea.Msg {
			n, err := c.UnreadCounts(ctx)
			return countsMsg{n, err}
		}))
	}
	open := ""
	if c := m.current(); c != nil {
		open = c.ID
		cmds = append(cmds, m.loadHistory(open))
	}
	for id := range m.history {
		if id != open {
			delete(m.history, id)
		}
	}
	if t := m.thread; t != nil && t.msgs != nil {
		cmds = append(cmds, m.loadReplies(t.convID, t.root))
	}
	return tea.Batch(cmds...)
}
