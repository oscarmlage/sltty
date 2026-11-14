package tui

// Message search: the query, matches, and jumping to a hit.

import (
	"context"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/emoji"
)

// searchQuery is what goes to the backend: the filter, in Slack's syntax,
// with in: for the scope unless the filter has its own.
func (m *model) searchQuery() string {
	p := m.picker
	if p == nil || p.kind != pickSearch {
		return ""
	}
	q := strings.ToLower(strings.TrimSpace(p.input.Value()))
	if q != "" && p.scope != "" && !strings.Contains(q, "in:") {
		q = "in:" + scopeLabel(m, p.scope) + " " + q
	}
	return q
}

// scopeLabel is how in: names a conversation: #channel or @person.
func scopeLabel(m *model, id string) string {
	c, ok := m.convByID(id)
	switch {
	case !ok:
		return id
	case c.Kind == chat.DM:
		return "@" + c.Name
	}
	return "#" + c.Name
}

// openSearch opens the message search, in the conversation of the focused
// pane when here is set (tab switches).
// openStatus opens the presence and Do Not Disturb picker.
func (m *model) openStatus() tea.Cmd { return m.openPicker(pickStatus, nil) }

func (m *model) openSearch(here bool) tea.Cmd {
	cmd := m.openPicker(pickSearch, nil)
	p := m.picker
	if m.view == sideConv || m.thread != nil && m.focus == paneThread {
		p.here = m.convID()
	}
	if here {
		p.scope = p.here
	}
	return cmd
}

// nextMatch is n / N: the next (older) or previous result of the last
// search, in whatever conversation it is.
func (m *model) nextMatch(d int) tea.Cmd {
	f := m.found
	if f == nil || len(f.hits) == 0 {
		m.status = "no search to repeat: " + m.km.first(actFilter) + " or " + m.km.first(actSearch) + " first"
		return nil
	}
	f.i += d
	switch {
	case f.i >= len(f.hits):
		f.i, m.status = 0, "back to the newest result"
	case f.i < 0:
		f.i, m.status = len(f.hits)-1, "on to the oldest result"
	}
	return m.jumpToHit(f.hits[f.i])
}

func (m *model) jumpToHit(h chat.Hit) tea.Cmd {
	i := slices.IndexFunc(m.convs, func(c chat.Conversation) bool { return c.ID == h.ConvID })
	if i < 0 {
		return nil
	}
	m.jump = &jump{convID: h.ConvID, ts: h.Msg.TS, root: h.RootTS}
	if i != m.cursor || m.thread != nil && m.thread.root != h.RootTS {
		m.closeThread()
	}
	cmd := m.selectConv(i)
	if _, ok := m.history[h.ConvID]; !ok {
		return cmd // applyJump runs when the history arrives
	}
	return tea.Batch(cmd, m.applyJump())
}

func (m *model) foundWords() []string {
	if m.found == nil {
		return nil
	}
	return m.found.words
}

// searchNow runs the search once the query has stayed the same for the
// debounce time.
func (m *model) searchNow(q string) tea.Cmd {
	if q == "" || m.searchQuery() != q {
		return nil
	}
	return m.call(func(ctx context.Context) tea.Msg {
		hits, err := m.backend.Search(ctx, q)
		return searchMsg{q, hits, err}
	})
}

func (m *model) searchResults(msg searchMsg) {
	if m.searchQuery() != msg.query {
		return // the query changed meanwhile
	}
	p := m.picker
	p.waiting = false
	if msg.err != nil {
		m.err = msg.err.Error()
		return
	}
	p.items, p.cursor, p.first = nil, 0, 0
	now := time.Now()
	for _, h := range msg.hits {
		where := "#?"
		for _, c := range m.convs {
			if c.ID == h.ConvID {
				where = convLabel(c)
			}
		}
		if h.RootTS != "" {
			where += " › thread"
		}
		label := statusWhere.Render(where) + "  " + nameStyle.Render(h.Msg.User) + "  " + highlight(oneLine(h.Msg.Text), chat.ParseQuery(msg.query).Words)
		p.items = append(p.items, pickItem{label: label, right: clock(h.Msg.At, now), hit: h})
	}
}

// oneLine is a message's text as one plain line, links by their labels.
func oneLine(s string) string {
	s = markdownLink.ReplaceAllString(s, "$1")
	return strings.Join(strings.Fields(emoji.Expand(s)), " ")
}
