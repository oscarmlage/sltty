package tui

// Navigation state: unread bookkeeping, conversations, threads, jumps and pane widths.

import (
	"cmp"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
)

// noteNew puts the New line above the last unread messages of id.
func (m *model) noteNew(id string, unread int) {
	if h := m.history[id]; unread > 0 && unread <= len(h) {
		m.newFrom[id] = h[len(h)-unread].TS
	}
}

func (m *model) setUnread(id string, n int) {
	for i := range m.convs {
		if m.convs[i].ID == id {
			m.convs[i].Unread = n
		}
	}
}

// replaceMsg puts a changed message in place, in the conversation and in
// the open thread.
func (m *model) replaceMsg(id string, msg chat.Message) {
	for i := range m.history[id] {
		if m.history[id][i].TS == msg.TS {
			m.history[id][i] = msg
		}
	}
	if t := m.thread; t != nil && t.convID == id {
		for i := range t.msgs {
			if t.msgs[i].TS == msg.TS {
				t.msgs[i] = msg
			}
		}
	}
}

func (m *model) sent(msg sentMsg) tea.Cmd {
	if msg.err != nil {
		// Never retried: a second Send could post twice.
		m.err = "not sent: " + msg.err.Error()
		return nil
	}
	m.status = "sent"
	h := m.history[msg.id]
	if msg.root == "" {
		delete(m.newFrom, msg.id) // writing in it, you have read it
		follow := m.sel[msg.id] >= len(h)-1 || len(h) == 0
		m.history[msg.id] = append(h, msg.msg)
		if follow {
			m.sel[msg.id] = len(h)
			m.chatOff = 0
		}
		return nil
	}
	for i := range h {
		if h[i].TS == msg.root {
			h[i].Replies++
			h[i].LastReply = msg.msg.At
		}
	}
	if t := m.thread; t != nil && t.convID == msg.id && t.root == msg.root && t.msgs != nil {
		follow := t.sel >= len(t.msgs)-1
		t.msgs = append(t.msgs, msg.msg)
		if len(t.msgs) > 0 && t.msgs[0].TS == msg.root {
			t.msgs[0].Replies++
		}
		if follow {
			t.sel, t.off = len(t.msgs)-1, 0
		}
	}
	return nil
}

// selectConv moves the sidebar to conversation i, keeping the draft of the
// one it leaves, and loads its history the first time. An open thread of
// another conversation closes.
func (m *model) selectConv(i int) tea.Cmd {
	if len(m.convs) == 0 {
		return nil
	}
	i = max(0, min(i, len(m.convs)-1))
	m.onSection = ""
	switch {
	case m.view != sideConv:
		if m.view == sideActivity {
			m.seeActivity()
		}
		m.closeThread()
		m.view = sideConv
	case i != m.cursor:
		m.cancelEdit()
		if c := m.current(); c != nil {
			m.drafts[c.ID] = m.chatIn.Value()
			delete(m.newFrom, c.ID) // the New line lasts while you are in it
		}
		m.closeThread()
	}
	m.cursor, m.chatOff = i, 0
	c := m.current()
	m.chatIn.SetValue(m.drafts[c.ID])
	m.chatIn.Placeholder = "Message " + convLabel(*c)
	if _, ok := m.history[c.ID]; ok {
		if c.Unread > 0 {
			m.noteNew(c.ID, c.Unread)
		}
		return nil
	}
	return m.loadCachedHistory(c.ID)
}

// openThread opens the thread of the selected message (an empty one starts
// a new thread) and moves the focus there.
func (m *model) openThread() tea.Cmd {
	c, msg := m.current(), m.selectedMsg()
	if c == nil || msg == nil || m.view != sideConv {
		return nil
	}
	return m.openThreadAt(c.ID, msg.TS)
}

// openThreadAt opens a thread of any conversation in the thread panel.
func (m *model) openThreadAt(convID, root string) tea.Cmd {
	m.focus = paneThread
	if t := m.thread; t != nil && t.convID == convID && t.root == root {
		return nil
	}
	m.closeThread()
	m.focus = paneThread
	m.thread = &thread{convID: convID, root: root}
	m.threadIn.SetValue(m.drafts[threadKey(convID, root)])
	m.threadIn.Placeholder = "Reply…"
	if m.threadW == 0 {
		m.threadW = max(minThread, m.width*2/5)
	}
	m.setThreadW(m.threadW)
	return m.loadReplies(convID, root)
}

// seeActivity marks everything in the Activity view read, when leaving it.
func (m *model) seeActivity() {
	for _, a := range m.activity {
		if _, ok := m.readActivity[a.ID]; !ok { // keeps what u marked unread
			m.readActivity[a.ID] = true
		}
	}
}

// convID is the conversation an action in the focused pane is about: the
// thread's in the thread panel, the selected one otherwise.
func (m *model) convID() string {
	if m.focus == paneThread && m.thread != nil {
		return m.thread.convID
	}
	if c := m.current(); c != nil {
		return c.ID
	}
	return ""
}

func (m *model) closeThread() {
	t := m.thread
	if t == nil {
		return
	}
	if m.editing != nil && m.activeInput() == &m.threadIn {
		m.cancelEdit()
	}
	m.drafts[t.convID+"/"+t.root] = m.threadIn.Value()
	m.thread = nil
	if m.focus == paneThread {
		m.focus = paneChat
		m.leaveInsert()
	}
}

// applyJump selects the message a search picked, opening its thread when it
// is a reply. It waits for the history (and the replies) to load.
func (m *model) applyJump() tea.Cmd {
	j := m.jump
	h, ok := m.history[j.convID]
	if !ok {
		return nil
	}
	ts := cmp.Or(j.root, j.ts)
	if i := slices.IndexFunc(h, func(x chat.Message) bool { return x.TS == ts }); i >= 0 {
		m.sel[j.convID] = i
	}
	m.focus = paneChat
	if j.root == "" {
		m.jump = nil
		return nil
	}
	cmd := m.openThread()
	if t := m.thread; t != nil && t.msgs != nil { // that thread was already open
		t.sel = max(0, slices.IndexFunc(t.msgs, func(x chat.Message) bool { return x.TS == j.ts }))
		m.jump = nil
	}
	return cmd
}

// Pane widths. The sidebar and the thread panel keep their widths and the
// conversation takes the rest, never less than minMain while it fits.
func (m *model) threadWidth() int {
	if m.thread == nil {
		return 0
	}
	return m.threadW
}

func (m *model) mainWidth() int {
	w := m.width - m.side - 1
	if m.thread != nil {
		w -= m.threadW + 1
	}
	return max(1, w)
}

func (m *model) setSide(w int) {
	if m.width > 0 {
		room := m.width - 1 - minMain
		if m.thread != nil {
			room -= m.threadW + 1
		}
		w = min(w, room)
	}
	m.side = max(minSide, w)
}

func (m *model) setThreadW(w int) {
	if m.width > 0 {
		w = min(w, m.width-m.side-2-minMain)
	}
	m.threadW = max(minThread, w)
}
