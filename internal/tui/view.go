package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/chat"
)

// Screen: the panes on rows 0..height-2, a vim-like status line on the last
// row. Across: the sidebar [0, side), a divider, the conversation and, when a
// thread is open, another divider and the thread panel. The conversation and
// thread panes are a header, a rule, the messages and a composer box.
const paneTop = 2 // header and rule above the messages

// layout is where the last frame drew things, for the mouse. Pane rows are
// relative to the panes' content, which starts top rows down (1 inside the
// boxes of the panels layout).
type layout struct {
	top         int
	side        []int    // conversation index per content row, -1 for none
	chatMsg     []int    // message index per content row of the conversation, -1
	threadMsg   []int    // the same for the thread
	chatText    []string // rendered chat rows, for drag-to-copy
	threadText  []string // rendered thread rows, for drag-to-copy
	composerTop int      // content row where the composers start
	pics        []picHit // the pictures on screen, for a click to open

	// the picker's list: screen row of its first entry, its left column,
	// width and rows; pickTop is -1 without a picker
	pickTop, pickX, pickW, pickRows int
}

func boxed() bool { return theme.Layout == layoutPanels }

func (m *model) bodyHeight() int { return max(1, m.height-1) }

// contentRows is the height of the panes' content.
func (m *model) contentRows() int {
	if boxed() {
		return max(1, m.bodyHeight()-2)
	}
	return m.bodyHeight()
}

// inner is the content width of a pane w columns wide.
func inner(w int) int {
	if boxed() {
		return max(1, w-4)
	}
	return w
}

func (m *model) msgRows() int { return max(1, m.contentRows()-paneTop-m.composerH()) }
func (m *model) mainX() int   { return m.side + 1 }
func (m *model) threadX() int { return m.width - m.threadWidth() }

func (m *model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion // motion while a button is held: divider drag
	return v
}

func (m *model) render() string {
	if m.width == 0 {
		return ""
	}
	if m.viewer != nil {
		out := m.viewerScreen()
		if m.quitting {
			m.overlayQuit(out)
		}
		return strings.Join(out, "\n")
	}
	h, rows := m.bodyHeight(), m.contentRows()
	m.layout.top, m.layout.pics = 0, nil
	if boxed() {
		m.layout.top = 1
	}
	side := m.pane(m.sidebar(inner(m.side), rows), m.side, m.focus == paneSide, sbBg)
	mainContent := m.chatPane(inner(m.mainWidth()), rows)
	m.paintDragSelection(mainContent, paneChat)
	mainP := m.pane(mainContent, m.mainWidth(), m.focus == paneChat, paneBg)
	var threadP []string
	if m.thread != nil {
		threadContent := m.threadPane(inner(m.threadW), rows)
		m.paintDragSelection(threadContent, paneThread)
		threadP = m.pane(threadContent, m.threadW, m.focus == paneThread, paneBg)
	}
	div := dividerStyle.Render("│")
	switch {
	case m.drag != 0 && boxed():
		div = dragStyle.Render("┃")
	case m.drag != 0:
		div = dragStyle.Render("│")
	case boxed():
		div = " "
	}
	div = paint(div, 1, paneBg)
	out := make([]string, 0, m.height)
	for i := range h {
		row := side[i] + div + mainP[i]
		if threadP != nil {
			row += div + threadP[i]
		}
		out = append(out, row)
	}
	out = append(out, paint(m.statusLine(), m.width, paneBg))
	m.overlayLoading(out)
	if m.picker != nil && m.picker.kind != pickTheme || m.help || m.quitting {
		m.backdrop(out) // not under the theme picker: it previews the theme
	}
	if m.settings != nil {
		m.overlaySettings(out)
	}
	switch {
	case m.picker != nil:
		m.overlayPicker(out)
	case m.help:
		m.overlaySheet(out)
	}
	if m.quitting {
		m.overlayQuit(out)
	}
	return strings.Join(out, "\n")
}

// pane puts a pane's content on its background, inside a box in the panels
// layout.
func (m *model) pane(content []string, w int, focused bool, bg string) []string {
	if boxed() {
		content = frame("", content, w, focused)
	}
	for i, l := range content {
		content[i] = paint(l, w, bg)
	}
	return content
}

func convLabel(c chat.Conversation) string {
	switch {
	case c.Kind == chat.DM:
		return "@" + c.Name
	case c.Private:
		return privateIcon + " " + c.Name
	}
	return "#" + c.Name
}

// fit truncates or pads a styled line to exactly w columns.
func fit(s string, w int) string {
	s = ansi.Truncate(expandTabs(s), w, "…")
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

// expandTabs replaces tabs with spaces. Width functions count a tab as zero
// columns, but the terminal jumps to the next tab stop: a tab in a Slack
// message made its line wider than the screen, the terminal wrapped and
// scrolled it, and rows of every pane were repeated or left with stale
// colours until the next full repaint.
func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// paint fits a line to w columns on a background: bg is re-applied after
// each segment's reset, which would otherwise end it.
func paint(s string, w int, bg string) string {
	s = fit(s, w)
	if bg == "" {
		return s
	}
	for _, reset := range []string{"\x1b[0m", "\x1b[m"} {
		s = strings.ReplaceAll(s, reset, reset+bg)
	}
	return bg + s + "\x1b[0m"
}

func (m *model) chatPane(w, rows int) []string {
	if m.view != sideConv {
		return m.listPane(w, rows)
	}
	out := make([]string, 0, rows)
	c := m.current()
	if c == nil {
		for range rows {
			out = append(out, fit("", w))
		}
		return out
	}
	header := paneHeader(convLabel(*c), c.Topic, w, m.focus == paneChat)
	if t := m.typingText(c.ID); t != "" {
		ts := headerStyle
		if m.focus == paneChat {
			ts = headerFocus
		}
		header = fit(" "+ts.Render(strings.Join(strings.Fields(convLabel(*c)), " "))+"  "+sbOnline.Render(t), w)
	}
	out = append(out, header, ruleStyle.Render(strings.Repeat("─", w)))
	n := m.msgRows()
	m.layout.chatMsg, m.layout.chatText = make([]int, rows), make([]string, rows)
	for i := range m.layout.chatMsg {
		m.layout.chatMsg[i] = -1
	}
	msgs, loaded := m.history[c.ID]
	if !loaded {
		out = append(out, fit(dimStyle.Render(" loading…"), w))
	} else {
		openRoot := ""
		if m.thread != nil {
			openRoot = m.thread.root
		}
		vis, at, off, pics := m.msgList(msgs, false, w, n, m.chatOff, m.selected(), m.focus == paneChat, openRoot, m.newFrom[c.ID])
		m.chatOff = off
		pad := n - len(vis) // the newest message sits on top of the composer
		m.addPics(paneChat, pics, paneTop+pad, m.mainX())
		for range pad {
			out = append(out, fit("", w))
		}
		for i, mi := range at {
			y := paneTop + pad + i
			m.layout.chatMsg[y], m.layout.chatText[y] = mi, vis[i]
		}
		out = append(out, vis...)
	}
	for len(out) < paneTop+n {
		out = append(out, fit("", w))
	}
	m.layout.composerTop = len(out)
	return append(out, m.composer(&m.chatIn, w)...)
}

// msgList renders msgs scrolled off lines from the bottom so the selected
// message is on screen, marking it (a blue bar while its pane has the focus
// in NORMAL mode) and the root of the open thread. It returns the visible
// lines, the message index of each (-1 between messages) and the new off.
// The pictures on screen come too, at their visible lines.
func (m *model) msgList(msgs []chat.Message, inThread bool, w, rows, off, sel int, focused bool, openRoot, newTS string) ([]string, []int, int, []picSpot) {
	lines, blocks := renderMsgs(msgs, w, inThread, m.backend.Me(), time.Now(), newTS, m.foundWords(), m)
	dim := func(bi int) bool { return openRoot != "" && msgs[bi].TS == openRoot }
	vis, at, off := blockList(lines, blocks, w, rows, off, sel, focused && m.mode == normal, dim)
	end := max(0, len(lines)-off)
	start := max(0, end-rows) // as window has it
	var pics []picSpot
	for _, b := range blocks {
		for _, s := range b.pics {
			if s.line < end && s.line+s.rows > start {
				s.line -= start
				pics = append(pics, s)
			}
		}
	}
	return vis, at, off, pics
}

// picHit is a picture on screen: content rows and screen columns.
type picHit struct {
	pane pane
	picSpot
}

// addPics notes where a pane's pictures are on screen: its content starts
// top rows down and x columns across.
func (m *model) addPics(p pane, pics []picSpot, top, x int) {
	if boxed() {
		x++ // the box's border
	}
	for _, s := range pics {
		s.line += top
		s.x += x
		m.layout.pics = append(m.layout.pics, picHit{p, s})
	}
}

// picAt is the picture at content row y and column x.
func (m *model) picAt(p pane, x, y int) (picHit, bool) {
	for _, h := range m.layout.pics {
		if h.pane == p && y >= h.line && y < h.line+h.rows && x >= h.x && x < h.x+h.cols {
			return h, true
		}
	}
	return picHit{}, false
}

func (m *model) threadPane(w, rows int) []string {
	t := m.thread
	out := []string{paneHeader("Thread", m.whereLabel(t.convID), w, m.focus == paneThread), ruleStyle.Render(strings.Repeat("─", w))}
	n := m.msgRows()
	if t.msgs == nil {
		out = append(out, fit(dimStyle.Render(" loading…"), w))
	} else {
		vis, at, off, pics := m.msgList(t.msgs, true, w, n, t.off, t.sel, m.focus == paneThread, "", "")
		t.off = off
		m.addPics(paneThread, pics, paneTop, m.threadX())
		m.layout.threadMsg, m.layout.threadText = make([]int, rows), make([]string, rows)
		for i := range m.layout.threadMsg {
			m.layout.threadMsg[i] = -1
		}
		for i, mi := range at {
			y := paneTop + i
			m.layout.threadMsg[y], m.layout.threadText[y] = mi, vis[i]
		}
		out = append(out, vis...)
	}
	for len(out) < paneTop+n {
		out = append(out, fit("", w))
	}
	return append(out, m.composer(&m.threadIn, w)...)
}

func paneHeader(title, sub string, w int, focused bool) string {
	// Channel topics can contain newlines. A pane header occupies exactly one
	// row; letting one through shifts its rule and all subsequent content.
	title = strings.Join(strings.Fields(title), " ")
	sub = strings.Join(strings.Fields(sub), " ")

	ts := headerStyle
	if focused {
		ts = headerFocus
	}
	s := " " + ts.Render(title)
	if sub != "" {
		s += dimStyle.Render("  " + sub)
	}
	return fit(s, w)
}

func (m *model) composer(in *textarea.Model, w int) []string {
	in.SetHeight(m.inputRows)
	focused := m.mode == insert && in == m.activeInput()
	title := ""
	if m.editing != nil && focused {
		title = editTitle.Render(" editing ") + dimStyle.Render(" "+m.km.first(actSend)+" save · "+m.km.first(actNormalMode)+" cancel ")
	} else if r := m.replying; r != nil && in == &m.chatIn {
		title = editTitle.Render(" ↩ "+r.who+" ") + dimStyle.Render(" "+r.text)
		title = ansi.Truncate(title, max(0, w-6-lipgloss.Width(m.km.first(actNormalMode))-8), "…") + dimStyle.Render(" · "+m.km.first(actNormalMode)+" cancel ")
	}
	if !boxed() {
		in.SetWidth(max(1, w-4))
		return frame(title, strings.Split(in.View(), "\n"), w, focused)
	}
	// panels: a filled block, the hint to start typing in it.
	in.SetWidth(max(1, w-2))
	body := strings.Split(in.View(), "\n")
	if !focused && in.Value() == "" {
		body = []string{dimStyle.Render(in.Placeholder + "  (" + m.km.first(actInsert) + " to insert)")}
	}
	bg := bgSeq(theme.Surface)
	if focused {
		bg = bgSeq(mix(theme.Surface, theme.Primary, 0.15))
	}
	out := []string{paint(" "+title, w, bg)}
	for i := range m.inputRows {
		l := ""
		if i < len(body) {
			l = body[i]
		}
		out = append(out, paint(" "+l, w, bg))
	}
	return append(out, paint("", w, bg))
}

func (m *model) statusLine() string {
	pill := normalPill.Render("NORMAL")
	if m.mode == insert {
		pill = insertPill.Render("INSERT")
	}
	where := ""
	switch {
	case m.view == sideThreads:
		where = "Threads"
	case m.view == sideActivity:
		where = "Activity"
	case m.current() != nil:
		where = convLabel(*m.current())
	}
	if m.thread != nil {
		where += " › thread " + m.whereLabel(m.thread.convID)
	}
	left := pill + " " + statusWhere.Render(where) + "  "
	ind := m.indicators()
	if m.cmdline != nil {
		m.cmdline.SetWidth(max(10, m.width-lipgloss.Width(ind)-16))
		left = commandPill.Render("COMMAND") + " " + keyStyle.Render(":") + m.cmdline.View()
		return fit(fit(left, m.width-lipgloss.Width(ind))+ind, m.width)
	}
	var right string
	switch {
	case m.err != "":
		right = errStyle.Render(m.err)
	case m.status != "":
		right = dimStyle.Render(m.status)
	case m.found != nil && len(m.found.hits) > 0:
		f := m.found
		right = matchStyle.Render("/"+f.query) + dimStyle.Render(fmt.Sprintf(" %d/%d  ", f.i+1, len(f.hits))) +
			hints([][2]string{{m.km.first(actNextMatch) + "/" + m.km.first(actPrevMatch), "next/prev"}, {m.km.first(actBack), "clear"}})
	default:
		right = hints(m.hintList())
	}
	return fit(fit(left+right, m.width-lipgloss.Width(ind))+ind, m.width)
}

// hintList is what the status line advertises, with the configured keys.
func (m *model) hintList() [][2]string {
	k := m.km.first
	if m.mode == insert {
		return [][2]string{{k(actSend), "send"}, {k(actNewline), "new line"}, {k(actNormalMode), "normal"}, {k(actEditLast), "edit last"}}
	}
	switch m.focus {
	case paneChat:
		return [][2]string{{k(actDown) + "/" + k(actUp), "messages"}, {k(actOpen), m.openHint()}, {k(actReact), "react"}, {k(actEdit), "edit"}, {k(actInsert), "write"}, {k(actFilter), "search"}, {k(actHelp), "help"}}
	case paneThread:
		return [][2]string{{k(actDown) + "/" + k(actUp), "messages"}, {k(actInsert), "reply"}, {k(actReact), "react"}, {k(actEdit), "edit"}, {k(actBack), "close"}, {k(actHelp), "help"}}
	}
	return [][2]string{{k(actDown) + "/" + k(actUp), "chats"}, {k(actFocusRight), "messages"}, {k(actFilter), "find chat"}, {k(actSearch), "search"}, {k(actSettings), "settings"}, {k(actHelp), "help"}, {k(actBackOrQuit), "quit"}}
}

func hints(hs [][2]string) string {
	var parts []string
	for _, h := range hs {
		parts = append(parts, keyStyle.Render(h[0])+" "+dimStyle.Render(h[1]))
	}
	return strings.Join(parts, "  ")
}

// frame draws a rounded box of width columns around body, with an optional
// title in the top border; a box grows to fit its body.
func frame(title string, body []string, width int, focused bool) []string {
	border := boxBorder
	if focused {
		border = boxBorderFocus
	}
	inner := max(0, width-4)
	top := border.Render("╭" + strings.Repeat("─", max(0, width-2)) + "╮")
	if title != "" {
		if !strings.Contains(title, "\x1b") { // plain: give it the title look
			title = titleStyle.Render(" " + title + " ")
		}
		title = ansi.Truncate(title, max(0, width-4), "…")
		top = border.Render("╭─") + title + border.Render(strings.Repeat("─", max(0, width-3-lipgloss.Width(title)))+"╮")
	}
	out := []string{top}
	for _, line := range body {
		out = append(out, border.Render("│ ")+fit(line, inner)+border.Render(" │"))
	}
	return append(out, border.Render("╰"+strings.Repeat("─", max(0, width-2))+"╯"))
}

// openHint is what enter does on a message: Telegram replies, Slack opens
// the thread.
func (m *model) openHint() string {
	if _, ok := m.backend.(chat.Replier); ok {
		return "reply"
	}
	return "thread"
}
