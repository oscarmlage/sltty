package tui

// Live events: what a chat.Live backend pushes while the client runs (new,
// edited and deleted messages, reactions, reads elsewhere, presence).

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
)

type eventMsg chat.Event

// waitEvent delivers the next event; it is issued again after each one.
func waitEvent(ch <-chan chat.Event) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return nil
		}
		return eventMsg(e)
	}
}

func (m *model) live(e chat.Event) tea.Cmd {
	if m.loading { // the chat list on its way already has it
		return nil
	}
	switch e.Kind {
	case chat.EvMessage:
		return m.liveMessage(e)
	case chat.EvEdited:
		m.replaceMsg(e.ConvID, e.Msg)
	case chat.EvReactions:
		m.eachMsg(e.ConvID, func(x *chat.Message) {
			if x.TS == e.Msg.TS {
				x.Reactions = e.Msg.Reactions
			}
		})
	case chat.EvDeleted:
		m.deleteMsgs(e.ConvID, e.TS)
	case chat.EvRead:
		if c, ok := m.convByID(e.ConvID); ok && e.Unread < c.Unread {
			m.setUnread(e.ConvID, e.Unread)
			if e.Unread == 0 {
				delete(m.newFrom, e.ConvID)
			}
		}
	case chat.EvTyping:
		return m.noteTyping(e)
	case chat.EvConv:
		if e.Conv != nil {
			m.convChanged(*e.Conv)
		}
	case chat.EvPresence:
		for i := range m.convs {
			if m.convs[i].ID == e.ConvID {
				m.convs[i].Online = e.Online
			}
		}
	}
	return nil
}

// eachMsg runs f on every loaded copy of the conversation's messages.
func (m *model) eachMsg(id string, f func(*chat.Message)) {
	for i := range m.history[id] {
		f(&m.history[id][i])
	}
	if t := m.thread; t != nil && t.convID == id {
		for i := range t.msgs {
			f(&t.msgs[i])
		}
	}
}

func (m *model) deleteMsgs(id string, ts []string) {
	gone := func(x chat.Message) bool { return slices.Contains(ts, x.TS) }
	if h, ok := m.history[id]; ok {
		m.history[id] = slices.DeleteFunc(h, gone)
		if n := len(m.history[id]); m.sel[id] >= n {
			m.sel[id] = max(0, n-1)
		}
	}
	if t := m.thread; t != nil && t.convID == id && t.msgs != nil {
		if len(t.msgs) > 0 && gone(t.msgs[0]) {
			m.closeThread() // its root is gone
			return
		}
		t.msgs = slices.DeleteFunc(t.msgs, gone)
		t.sel = min(t.sel, len(t.msgs)-1)
	}
}

func (m *model) liveMessage(e chat.Event) tea.Cmd {
	id, msg := e.ConvID, e.Msg
	if c, ok := m.convByID(id); !ok {
		if e.Conv == nil {
			return nil
		}
		m.addConv(*e.Conv)
	} else if e.Conv != nil && e.Conv.Section != c.Section {
		m.convChanged(*e.Conv) // back from Archived
	}
	m.bump(id)
	delete(m.typing[id], msg.User) // what they typed has arrived
	mine := msg.User == m.backend.Me()
	same := func(x chat.Message) bool { return x.TS == msg.TS }
	known := slices.ContainsFunc(m.history[id], same) ||
		m.thread != nil && m.thread.convID == id && slices.ContainsFunc(m.thread.msgs, same)

	// The conversation, when loaded. A message already there (one this
	// client sent) is replaced, not repeated.
	if h, ok := m.history[id]; ok {
		if i := slices.IndexFunc(h, same); i >= 0 {
			h[i] = msg
		} else {
			follow := m.sel[id] >= len(h)-1 || len(h) == 0
			m.history[id] = append(h, msg)
			if follow {
				m.sel[id] = len(h)
				if c := m.current(); c != nil && c.ID == id {
					m.chatOff = 0
				}
			}
		}
	}
	if e.RootTS != "" && !known {
		for i, x := range m.history[id] {
			if x.TS == e.RootTS && x.Replies > 0 {
				m.history[id][i].Replies++
				m.history[id][i].LastReply = msg.At
			}
		}
		if t := m.thread; t != nil && t.convID == id && t.root == e.RootTS && t.msgs != nil {
			follow := t.sel >= len(t.msgs)-1
			t.msgs = append(t.msgs, msg)
			if follow {
				t.sel, t.off = len(t.msgs)-1, 0
			}
		}
	}
	if mine || known {
		return nil
	}
	// Read at once where the user is reading; counted unread elsewhere.
	if m.inChat() == id {
		return m.mark(id, msg.TS)
	}
	if c, ok := m.convByID(id); ok {
		m.setUnread(id, c.Unread+1)
		if _, ok := m.history[id]; ok && m.newFrom[id] == "" {
			m.newFrom[id] = msg.TS
		}
		if !c.Muted {
			m.status = "💬 " + convLabel(c) + " · " + msg.User
		}
	}
	return nil
}

// addConv puts a conversation the sidebar did not have at the top of its
// section (or of the list).
func (m *model) addConv(c chat.Conversation) {
	at := slices.IndexFunc(m.convs, func(x chat.Conversation) bool { return x.Section == c.Section })
	if at < 0 {
		at = len(m.convs)
		if c.Section == "" {
			at = 0
		}
	}
	m.moveConv(func() { m.convs = slices.Insert(m.convs, at, c) })
}

// bump moves a conversation with a new message to the top of its section,
// as Telegram orders its chat list. Not while a picker lists them by
// position, nor for sections the config file orders.
func (m *model) bump(id string) {
	if m.picker != nil || len(m.opts.Sections) > 0 {
		return
	}
	i := slices.IndexFunc(m.convs, func(x chat.Conversation) bool { return x.ID == id })
	if i < 0 || m.convs[i].Section == "" || m.convs[i].Pinned {
		return
	}
	first := slices.IndexFunc(m.convs, func(x chat.Conversation) bool { return x.Section == m.convs[i].Section })
	if first == i {
		return
	}
	m.moveConv(func() {
		c := m.convs[i]
		m.convs = slices.Insert(slices.Delete(m.convs, i, i+1), first, c)
	})
}

// moveConv reorders the sidebar keeping the cursor on its conversation.
func (m *model) moveConv(f func()) {
	cur := ""
	if c := m.current(); c != nil {
		cur = c.ID
	}
	f()
	if i := slices.IndexFunc(m.convs, func(x chat.Conversation) bool { return x.ID == cur }); i >= 0 {
		m.cursor = i
	}
}

// convChanged takes a conversation's new place and state (archived,
// pinned, muted); a move puts it at the top of its new section.
func (m *model) convChanged(c chat.Conversation) {
	i := slices.IndexFunc(m.convs, func(x chat.Conversation) bool { return x.ID == c.ID })
	if i < 0 {
		m.addConv(c)
		return
	}
	old := m.convs[i]
	x := &m.convs[i]
	x.Section, x.SectionEmoji, x.Muted, x.Pinned, x.Folded = c.Section, c.SectionEmoji, c.Muted, c.Pinned, c.Folded
	if old.Section == c.Section {
		return
	}
	moved := *x
	m.moveConv(func() { m.convs = slices.Delete(m.convs, i, i+1) })
	m.addConv(moved)
}

// typist is someone doing something in a conversation, until when.
type typist struct {
	action string
	until  time.Time
}

type typingTick struct{}

// typingFor is how long a typing notice lasts without being repeated
// (Telegram repeats it every few seconds while it lasts).
const typingFor = 6 * time.Second

func (m *model) noteTyping(e chat.Event) tea.Cmd {
	if e.Action == "" {
		delete(m.typing[e.ConvID], e.User)
		return nil
	}
	if m.typing[e.ConvID] == nil {
		m.typing[e.ConvID] = map[string]typist{}
	}
	m.typing[e.ConvID][e.User] = typist{e.Action, m.now().Add(typingFor)}
	return tea.Tick(typingFor+100*time.Millisecond, func(time.Time) tea.Msg { return typingTick{} })
}

func (m *model) expireTyping() {
	now := m.now()
	for id, who := range m.typing {
		for u, t := range who {
			if now.After(t.until) {
				delete(who, u)
			}
		}
		if len(who) == 0 {
			delete(m.typing, id)
		}
	}
}

// typingText is "Ana is typing…", "Ana and Bob are typing…", "3 people
// are typing…" or "Ana is recording a voice message…"; empty for nobody.
func (m *model) typingText(id string) string {
	who := m.typing[id]
	if len(who) == 0 {
		return ""
	}
	var names []string
	for u := range who {
		names = append(names, u)
	}
	slices.Sort(names)
	switch len(names) {
	case 1:
		return names[0] + " is " + who[names[0]].action + "…"
	case 2:
		return names[0] + " and " + names[1] + " are typing…"
	}
	return strconv.Itoa(len(names)) + " people are typing…"
}

// typedBefore notes the composer before a key; the func it returns tells
// a chat.Typer backend the user is typing when the key changed the text,
// at most every few seconds.
func (m *model) typedBefore(msg tea.Msg) func() tea.Cmd {
	none := func() tea.Cmd { return nil }
	tb, ok := m.backend.(chat.Typer)
	if _, key := msg.(tea.KeyPressMsg); !ok || !key || m.mode != insert || m.editing != nil {
		return none
	}
	in := m.activeInput()
	before := in.Value()
	conv, root := "", ""
	switch {
	case m.focus == paneThread && m.thread != nil:
		conv, root = m.thread.convID, m.thread.root
	case m.current() != nil:
		conv = m.current().ID
	}
	return func() tea.Cmd {
		after := m.activeInput().Value()
		if conv == "" || m.mode != insert || after == before || strings.TrimSpace(after) == "" {
			return nil
		}
		if m.lastTyping.conv == conv && m.now().Sub(m.lastTyping.at) < 5*time.Second {
			return nil
		}
		m.lastTyping.conv, m.lastTyping.at = conv, m.now()
		return m.call(func(ctx context.Context) tea.Msg {
			_ = tb.Typing(ctx, conv, root) // best effort: nothing to show if it fails
			return nil
		})
	}
}
