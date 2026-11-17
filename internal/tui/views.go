package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/chat"
)

// The Threads and Activity views take the conversation pane: a list of
// entries, j/k to move, enter (or a double click) to open one. Threads
// opens the thread on the right, where i replies; Activity jumps to the
// message.

type (
	threadsMsg struct {
		threads []chat.ThreadSummary
		err     error
	}
	activityMsg struct {
		items []chat.Activity
		err   error
	}
)

func (m *model) loadThreads() tea.Cmd {
	return m.call(func(ctx context.Context) tea.Msg {
		t, err := m.backend.Threads(ctx)
		return threadsMsg{t, err}
	})
}

func (m *model) loadActivity() tea.Cmd {
	return m.call(func(ctx context.Context) tea.Msg {
		a, err := m.backend.Activity(ctx)
		return activityMsg{a, err}
	})
}

func threadKey(conv, root string) string { return conv + "/" + root }

// threadUnread is a thread's unread replies, less what was seen here.
func (m *model) threadUnread(s chat.ThreadSummary) int {
	k := threadKey(s.ConvID, s.Root.TS)
	if m.unreadThread[k] {
		return max(1, s.Unread)
	}
	if m.readThreads[k] >= s.Replies {
		return 0
	}
	return s.Unread
}

func (m *model) threadsUnread() int {
	n := 0
	for _, s := range m.threads {
		if m.threadUnread(s) > 0 {
			n++
		}
	}
	return n
}

func (m *model) activityUnread() int {
	n := 0
	for _, a := range m.activity {
		if m.activityIsUnread(a) {
			n++
		}
	}
	return n
}

func (m *model) activityIsUnread(a chat.Activity) bool {
	if read, ok := m.readActivity[a.ID]; ok {
		return !read
	}
	return a.Unread
}

func (m *model) readThread(s chat.ThreadSummary) {
	k := threadKey(s.ConvID, s.Root.TS)
	m.readThreads[k] = s.Replies
	delete(m.unreadThread, k)
}

// toggleRead is u: read ↔ unread for what the focused pane is on. A
// conversation marked unread from the chat pane is unread from the
// selected message on, like Slack's "mark unread".
func (m *model) toggleRead() tea.Cmd {
	switch {
	case m.focus == paneThread && m.thread != nil:
		for _, s := range m.threads {
			if s.ConvID == m.thread.convID && s.Root.TS == m.thread.root {
				m.toggleThread(s)
				return nil
			}
		}
		m.status = "not one of your threads"
	case m.view == sideThreads && m.focus == paneChat:
		if i := m.listSel[sideThreads]; i < len(m.threads) {
			m.toggleThread(m.threads[i])
		}
	case m.view == sideActivity && m.focus == paneChat:
		if i := m.listSel[sideActivity]; i < len(m.activity) {
			a := m.activity[i]
			m.readActivity[a.ID] = m.activityIsUnread(a)
		}
	case m.view == sideThreads:
		for _, s := range m.threads {
			m.readThread(s)
		}
	case m.view == sideActivity:
		m.seeActivity()
	default:
		c := m.current()
		if c == nil {
			return nil
		}
		msgs := m.history[c.ID]
		if len(msgs) == 0 {
			m.status = "no messages to mark"
			return nil
		}
		if c.Unread > 0 {
			delete(m.keepUnread, c.ID)
			m.status = "marked read"
			return m.readTo(c.ID, msgs[len(msgs)-1].TS)
		}
		if u, ok := m.backend.(chat.UnreadMarker); ok {
			// The service keeps a read cursor that only moves forward and a
			// "marked unread" flag, as its own apps show: the flag it is.
			m.setUnread(c.ID, 1)
			delete(m.newFrom, c.ID)
			m.status = "marked unread"
			id := c.ID
			return m.call(func(ctx context.Context) tea.Msg { return markedMsg{id, u.MarkUnread(ctx, id, true)} })
		}
		from := len(msgs) - 1 // the sidebar: the last message
		if m.focus == paneChat {
			from = m.selected()
		}
		ts := ""
		if from > 0 {
			ts = msgs[from-1].TS
		}
		m.status = "marked unread"
		m.keepUnread[c.ID] = true
		return m.readTo(c.ID, ts)
	}
	return nil
}

func (m *model) toggleThread(s chat.ThreadSummary) {
	if m.threadUnread(s) > 0 {
		m.readThread(s)
		return
	}
	m.unreadThread[threadKey(s.ConvID, s.Root.TS)] = true
}

// readTo moves id's read cursor to ts ("" for nothing read): the messages
// after it are unread, with the New line above them. It shows at once and
// is counted here, from the history: Slack's conversations.mark returns no
// count.
func (m *model) readTo(id, ts string) tea.Cmd {
	h := m.history[id]
	n := len(h)
	if i := slices.IndexFunc(h, func(x chat.Message) bool { return x.TS == ts }); i >= 0 {
		n = len(h) - 1 - i
	}
	m.setUnread(id, n)
	delete(m.newFrom, id)
	m.noteNew(id, n)
	return m.mark(id, ts)
}

// readIn reads the conversation the user is in, up to its last message;
// the New line stays while they are there.
func (m *model) readIn(id string) tea.Cmd {
	c, ok := m.convByID(id)
	h := m.history[id]
	if !ok || c.Unread == 0 || len(h) == 0 {
		return nil
	}
	m.setUnread(id, 0)
	delete(m.keepUnread, id)
	return m.mark(id, h[len(h)-1].TS)
}

// inChat is the conversation whose messages have the focus, "" when the
// focus is on the sidebar, a list view or a thread.
func (m *model) inChat() string {
	if m.hidden { // another account's tab is in front: nobody reads this one
		return ""
	}
	if s := m.spot(); s.focus == paneChat {
		return s.conv
	}
	return ""
}

// spot is where the user is: the focused pane and the conversation next to
// the sidebar ("" for a list view).
type spot struct {
	focus pane
	conv  string
}

func (m *model) spot() spot {
	s := spot{focus: m.focus}
	if c := m.current(); c != nil && m.view == sideConv {
		s.conv = c.ID
	}
	return s
}

// entered reads the conversation the focus has just entered. Browsing the
// sidebar only previews one: it is read on entering it from the sidebar
// (enter, l, a click) or from elsewhere (the switcher, a search hit), and
// when its history arrives with the focus already there (historyMsg).
// Back from its own thread it stays as it was, so what u marked unread
// stays unread.
func (m *model) entered(before spot) tea.Cmd {
	now := m.spot()
	if now.focus != paneChat || now.conv == "" || before.focus != paneSide && before.conv == now.conv {
		return nil
	}
	if !m.readOnEnter {
		return nil
	}
	return m.readIn(now.conv)
}

func (m *model) mark(id, ts string) tea.Cmd {
	m.marked[id] = m.now()
	return m.call(func(ctx context.Context) tea.Msg {
		return markedMsg{id, m.backend.Mark(ctx, id, ts)}
	})
}

func (m *model) openThreadsView() tea.Cmd {
	m.onSection = ""
	if m.view == sideActivity {
		m.seeActivity()
	}
	m.leaveConv()
	m.view = sideThreads
	return m.loadThreads()
}

// openActivityView shows the activity; what is on it counts as read.
func (m *model) openActivityView() tea.Cmd {
	m.onSection = ""
	m.leaveConv()
	m.view = sideActivity
	return m.loadActivity()
}

// leaveConv keeps the draft of the conversation and closes its thread when
// the conversation pane switches to a list view.
func (m *model) leaveConv() {
	if m.view == sideConv {
		m.cancelEdit()
		if c := m.current(); c != nil {
			m.drafts[c.ID] = m.chatIn.Value()
			delete(m.newFrom, c.ID)
		}
	}
	m.closeThread()
	m.listOff = 0
}

func (m *model) listLen() int {
	if m.view == sideThreads {
		return len(m.threads)
	}
	return len(m.activity)
}

func (m *model) listMove(n int) {
	m.listSel[m.view] = max(0, min(m.listSel[m.view]+n, m.listLen()-1))
}

func (m *model) listTo(first bool) {
	m.listSel[m.view] = 0
	if !first {
		m.listSel[m.view] = max(0, m.listLen()-1)
	}
}

// listOpen opens the selected entry: a thread on the right, or the message
// an activity is about.
func (m *model) listOpen(write bool) tea.Cmd {
	i := m.listSel[m.view]
	if i >= m.listLen() {
		return nil
	}
	if m.view == sideThreads {
		s := m.threads[i]
		m.readThread(s)
		cmd := m.openThreadAt(s.ConvID, s.Root.TS)
		if write {
			return tea.Batch(cmd, m.enterInsert())
		}
		return cmd
	}
	a := m.activity[i]
	m.readActivity[a.ID] = true
	ci := slices.IndexFunc(m.convs, func(c chat.Conversation) bool { return c.ID == a.ConvID })
	if ci < 0 {
		return nil
	}
	m.jump = &jump{convID: a.ConvID, ts: a.Msg.TS, root: a.RootTS}
	cmd := m.selectConv(ci)
	if _, ok := m.history[a.ConvID]; !ok {
		return cmd // applyJump runs when the history arrives
	}
	return tea.Batch(cmd, m.applyJump())
}

// listPane is the Threads or Activity view in the conversation pane.
func (m *model) listPane(w, rows int) []string {
	title, sub := "Threads", "threads you are in"
	if m.view == sideActivity {
		title, sub = "Activity", "mentions, replies and reactions"
	}
	out := []string{paneHeader(title, sub, w, m.focus == paneChat), ruleStyle.Render(strings.Repeat("─", w))}
	m.layout.chatMsg = make([]int, rows)
	for i := range m.layout.chatMsg {
		m.layout.chatMsg[i] = -1
	}
	m.layout.composerTop = rows // no composer here
	var lines []string
	var blocks []block
	now := time.Now()
	entry := func(ls []string) {
		if len(blocks) > 0 {
			lines = append(lines, "")
		}
		from := len(lines)
		for _, l := range ls {
			lines = append(lines, " "+ansi.Truncate(l, max(1, w-2), "…"))
		}
		blocks = append(blocks, block{from: from, to: len(lines)})
	}
	if m.view == sideThreads {
		for _, s := range m.threads {
			entry(m.threadEntry(s, now))
		}
	} else {
		for _, a := range m.activity {
			entry(m.activityEntry(a, now))
		}
	}
	n := max(1, rows-paneTop)
	if len(blocks) == 0 {
		empty := " nothing here yet"
		if m.view == sideThreads && m.threads == nil || m.view == sideActivity && m.activity == nil {
			empty = " loading…"
		}
		out = append(out, fit(dimStyle.Render(empty), w))
	} else {
		var vis []string
		var at []int
		vis, at, m.listOff = blockList(lines, blocks, w, n, m.listOff, m.listSel[m.view], m.focus == paneChat && m.mode == normal, nil)
		for i, bi := range at {
			m.layout.chatMsg[paneTop+i] = bi
		}
		// a list reads from the top: fill below
		out = append(out, vis...)
	}
	for len(out) < rows {
		out = append(out, fit("", w))
	}
	return out
}

func (m *model) convByID(id string) (chat.Conversation, bool) {
	for _, c := range m.convs {
		if c.ID == id {
			return c, true
		}
	}
	return chat.Conversation{}, false
}

func (m *model) whereLabel(id string) string {
	if c, ok := m.convByID(id); ok {
		return convLabel(c)
	}
	return "?"
}

func (m *model) userStyle(u string) func(...string) string {
	if u == m.backend.Me() {
		return meStyle.Render
	}
	return nameStyle.Render
}

// conversationLines makes a compact, avatar-backed excerpt for list views.
func (m *model) conversationLines(user, userID, prefix, text string) []string {
	first := m.userStyle(user)(user)
	second := dimStyle.Render(prefix) + textStyle.Render(oneLine(text))
	if !m.avatars() || userID == "" {
		return []string{first + "  " + second}
	}
	avatar := m.avatarLines(userID)
	// List headers identify the conversation; inset the avatar-backed excerpt
	// beneath it so the two visual levels do not compete.
	return []string{"  " + avatar[0] + " " + first, "  " + avatar[1] + " " + second}
}

func (m *model) threadEntry(s chat.ThreadSummary, now time.Time) []string {
	last := s.Root.At
	if len(s.Latest) > 0 {
		last = s.Latest[len(s.Latest)-1].At
	}
	n := "1 reply"
	if s.Replies != 1 {
		n = fmt.Sprintf("%d replies", s.Replies)
	}
	head := statusWhere.Render(m.whereLabel(s.ConvID)) + dimStyle.Render(" · "+n+" · "+clock(last, now))
	if u := m.threadUnread(s); u > 0 {
		head = sbDot.Render("• ") + head + "  " + sbBadge.Render(fmt.Sprintf("%d new", u))
	}
	ls := []string{head}
	ls = append(ls, m.conversationLines(s.Root.User, s.Root.UserID, "", s.Root.Text)...)
	if len(s.Latest) > 0 {
		r := s.Latest[len(s.Latest)-1]
		ls = append(ls, m.conversationLines(r.User, r.UserID, "↳ ", r.Text)...)
	}
	return ls
}

func (m *model) activityEntry(a chat.Activity, now time.Time) []string {
	var what string
	switch a.Kind {
	case chat.ActMention:
		what = "mention"
	case chat.ActReply:
		what = "thread reply"
	case chat.ActReaction:
		what = "reaction"
	case chat.ActDM:
		what = "direct message"
	}
	head := statusWhere.Render(m.whereLabel(a.ConvID)) + dimStyle.Render(" · "+what+" · "+clock(a.At, now))
	if m.activityIsUnread(a) {
		head = sbDot.Render("• ") + head
	}
	prefix := ""
	if a.Kind == chat.ActReaction {
		prefix = "reacted with " + m.reactionEmoji(a.Emoji) + " to "
	}
	return append([]string{head}, m.conversationLines(a.By, a.ByID, prefix, a.Msg.Text)...)
}

// blockList shows lines scrolled off from the bottom so block sel is on
// screen, marking it while focused. dim marks other blocks quietly (the
// root of the open thread). It returns the visible lines, the block of each
// (-1 between blocks) and the new off.
func blockList(lines []string, blocks []block, w, rows, off, sel int, focused bool, dim func(int) bool) ([]string, []int, int) {
	if sel >= 0 && sel < len(blocks) {
		off = follow(off, len(lines), rows, blocks[sel])
	}
	vis, start := window(lines, rows, off)
	out := make([]string, len(vis))
	at := make([]int, len(vis))
	bi := 0
	for i, l := range vis {
		li := start + i
		for bi < len(blocks) && blocks[bi].to <= li {
			bi++
		}
		at[i] = -1
		in := bi < len(blocks) && li >= blocks[bi].from
		if in {
			at[i] = bi
		}
		switch {
		case in && bi == sel && focused:
			out[i] = paint(selMark.Render("▌")+l[1:], w, selBg)
		case in && (bi == sel || dim != nil && dim(bi)):
			out[i] = fit(openMark.Render("▌")+l[1:], w)
		default:
			out[i] = fit(l, w)
		}
	}
	return out, at, off
}
