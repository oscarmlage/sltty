package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Mouse: a press on a divider starts a drag that follows the pointer until
// release; any other click focuses the pane under it and picks the chat or
// message under it (and opens the link under it, if any), or enters INSERT
// on a composer; the wheel moves in the pane under the pointer.

func (m *model) mouse(msg tea.MouseMsg) tea.Cmd {
	mo := msg.Mouse()
	if m.viewer != nil {
		return m.viewerMouse(msg)
	}
	if m.picker != nil {
		return m.pickerMouse(msg)
	}
	if m.help || m.settings != nil {
		return nil
	}
	switch msg.(type) {
	case tea.MouseClickMsg:
		if mo.Button != tea.MouseLeft || mo.Y >= m.bodyHeight() {
			return nil
		}
		near := func(x int) bool { return mo.X == x || boxed() && (mo.X == x-1 || mo.X == x+1) }
		switch {
		case near(m.side):
			m.drag = dragSide
			return nil
		case m.thread != nil && near(m.threadX()-1):
			m.drag = dragThread
			return nil
		case m.paneAt(mo.X) != paneSide && mo.Y-m.layout.top == m.layout.composerTop:
			m.drag = dragComposer // the composer's top edge
			return nil
		}
		p := m.paneAt(mo.X)
		if h, ok := m.picAt(p, mo.X, mo.Y-m.layout.top); ok {
			return m.clickPicture(h)
		}
		// A press still selects the message as before, and also begins a
		// drag-to-copy. A release without movement has no further action.
		if m.view == sideConv && (p == paneChat || p == paneThread) && m.messageAt(p, mo.Y-m.layout.top) >= 0 {
			cmd := m.click(mo.X, mo.Y)
			m.drag = dragText
			m.selectDrag = struct {
				pane                       pane
				startX, startY, endX, endY int
			}{p, mo.X, mo.Y, mo.X, mo.Y}
			return cmd
		}
		return m.click(mo.X, mo.Y)
	case tea.MouseMotionMsg:
		switch m.drag {
		case dragSide:
			m.setSide(mo.X)
		case dragThread:
			m.setThreadW(m.width - mo.X - 1)
		case dragComposer:
			m.setInputRows(m.contentRows() - (mo.Y - m.layout.top) - 2)
		case dragText:
			m.selectDrag.endX, m.selectDrag.endY = mo.X, mo.Y
		}
	case tea.MouseReleaseMsg:
		if m.drag == dragText {
			d := m.selectDrag
			m.drag = 0
			if d.startX == mo.X && d.startY == mo.Y {
				return m.clickLink(d.pane, mo.X, mo.Y-m.layout.top) // a click, not a drag: open the link under it
			}
			d.endX, d.endY = mo.X, mo.Y
			if text := m.draggedText(d); text != "" {
				m.status = "copied selection"
				return tea.SetClipboard(text)
			}
			return nil
		}
		m.drag = 0
	case tea.MouseWheelMsg:
		n := map[tea.MouseButton]int{tea.MouseWheelUp: -1, tea.MouseWheelDown: 1}[mo.Button]
		if n == 0 || m.mode == insert {
			return nil
		}
		prev := m.focus
		m.focus = m.paneAt(mo.X)
		cmd := m.move(n)
		if m.focus != paneSide {
			m.focus = prev // the wheel scrolls without stealing the focus
		}
		return cmd
	}
	return nil
}

// messageAt returns the message displayed at a content row of a message pane.
func (m *model) messageAt(p pane, y int) int {
	if y < 0 {
		return -1
	}
	if p == paneChat && y < len(m.layout.chatMsg) {
		return m.layout.chatMsg[y]
	}
	if p == paneThread && y < len(m.layout.threadMsg) {
		return m.layout.threadMsg[y]
	}
	return -1
}

// draggedText copies the visible text range. It intentionally uses the drawn
// rows (without ANSI styling), so wrapped multi-line selections read exactly
// like the terminal while pictures contribute no opaque bytes.
func (m *model) draggedText(d struct {
	pane                       pane
	startX, startY, endX, endY int
}) string {
	if d.pane != m.paneAt(d.endX) {
		return ""
	}
	y0, y1 := d.startY-m.layout.top, d.endY-m.layout.top
	x0, x1 := d.startX, d.endX
	if y1 < y0 || y1 == y0 && x1 < x0 {
		y0, y1, x0, x1 = y1, y0, x1, x0
	}
	var lines []string
	for y := y0; y <= y1; y++ {
		if m.messageAt(d.pane, y) < 0 {
			continue
		}
		var line string
		if d.pane == paneChat && y < len(m.layout.chatText) {
			line = m.layout.chatText[y]
		}
		if d.pane == paneThread && y < len(m.layout.threadText) {
			line = m.layout.threadText[y]
		}
		line = strings.TrimRight(ansi.Strip(line), " ")
		left, right := 0, len([]rune(line))
		origin := m.paneX(d.pane)
		if boxed() {
			origin++
		}
		if y == y0 {
			left = max(0, x0-origin)
		}
		if y == y1 {
			right = min(right, max(0, x1-origin+1))
		}
		r := []rune(line)
		if left < right && left < len(r) {
			lines = append(lines, string(r[left:min(right, len(r))]))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (m *model) paneX(p pane) int {
	if p == paneThread {
		return m.threadX()
	}
	return m.mainX()
}

// paintDragSelection gives the in-progress drag the same visible feedback a
// terminal text selection has. The selected span is redrawn plainly so ANSI
// formatting cannot leak through the selection background.
func (m *model) paintDragSelection(lines []string, p pane) {
	if m.drag != dragText || m.selectDrag.pane != p {
		return
	}
	d := m.selectDrag
	y0, y1, x0, x1 := d.startY-m.layout.top, d.endY-m.layout.top, d.startX, d.endX
	if y1 < y0 || y1 == y0 && x1 < x0 {
		y0, y1, x0, x1 = y1, y0, x1, x0
	}
	origin := m.paneX(p)
	if boxed() {
		origin++
	}
	for y := max(0, y0); y <= y1 && y < len(lines); y++ {
		if m.messageAt(p, y) < 0 {
			continue
		}
		plain := []rune(ansi.Strip(lines[y]))
		from, to := 0, len(plain)
		if y == y0 {
			from = max(0, x0-origin)
		}
		if y == y1 {
			to = min(to, max(0, x1-origin+1))
		}
		if from >= to || from >= len(plain) {
			continue
		}
		to = min(to, len(plain))
		lines[y] = string(plain[:from]) + dragSelBg + string(plain[from:to]) + "\x1b[0m" + string(plain[to:])
	}
}

func (m *model) paneAt(x int) pane {
	switch {
	case x < m.side:
		return paneSide
	case m.thread != nil && x >= m.threadX():
		return paneThread
	}
	return paneChat
}

func (m *model) click(x, y int) tea.Cmd {
	y -= m.layout.top // pane content rows
	p := m.paneAt(x)
	if p != paneSide && y >= m.layout.composerTop {
		m.leaveInsert()
		m.focus = p
		return m.enterInsert()
	}
	m.leaveInsert()
	m.focus = p
	if h, ok := m.picAt(p, x, y); ok {
		return m.clickPicture(h)
	}
	switch p {
	case paneSide:
		if y < 0 || y >= len(m.layout.side) {
			return nil
		}
		switch r := m.layout.side[y]; {
		case r == rowThreads:
			return m.openThreadsView()
		case r == rowActivity:
			return m.openActivityView()
		case r <= rowSection:
			if ss := m.sections(); rowSection-r < len(ss) {
				m.onSection = ss[rowSection-r].name
				m.toggleSection(m.onSection)
			}
			return nil
		case r >= 0:
			return m.selectConv(r)
		}
	case paneChat:
		c := m.current()
		if c == nil || y < 0 || y >= len(m.layout.chatMsg) || m.layout.chatMsg[y] < 0 {
			return nil
		}
		i, now := m.layout.chatMsg[y], m.now()
		key := c.ID
		if m.view != sideConv {
			key = fmt.Sprint("view", m.view)
			m.listSel[m.view] = i
		} else {
			m.sel[c.ID] = i
		}
		lc := &m.lastClick
		if lc.convID == key && lc.msg == i && now.Sub(lc.at) < doubleClick {
			lc.convID = ""
			if m.view != sideConv {
				return m.listOpen(false)
			}
			return m.openThread()
		}
		lc.convID, lc.msg, lc.at = key, i, now
	case paneThread:
		if t := m.thread; t != nil && y >= 0 && y < len(m.layout.threadMsg) && m.layout.threadMsg[y] >= 0 {
			t.sel = m.layout.threadMsg[y]
		}
	}
	return nil
}

// clickPicture selects the picture's message and shows the picture big.
func (m *model) clickPicture(h picHit) tea.Cmd {
	switch t := m.thread; {
	case h.pane == paneThread && t != nil && h.msg < len(t.msgs):
		t.sel = h.msg
		return m.openViewer(&t.msgs[h.msg], h.file)
	case h.pane == paneChat && m.current() != nil:
		c := m.current()
		if msgs := m.history[c.ID]; h.msg < len(msgs) {
			m.sel[c.ID] = h.msg
			return m.openViewer(&msgs[h.msg], h.file)
		}
	}
	return nil
}

// pickerMouse: a click on an entry picks it, the wheel moves, a click
// outside the box closes it.
func (m *model) pickerMouse(msg tea.MouseMsg) tea.Cmd {
	mo, l, p := msg.Mouse(), m.layout, m.picker
	switch msg.(type) {
	case tea.MouseWheelMsg:
		switch mo.Button {
		case tea.MouseWheelDown:
			m.pickerMove(1)
		case tea.MouseWheelUp:
			m.pickerMove(-1)
		}
	case tea.MouseClickMsg:
		if mo.Button != tea.MouseLeft || l.pickTop < 0 {
			return nil
		}
		if mo.X < l.pickX || mo.X >= l.pickX+l.pickW {
			m.closePicker()
			return nil
		}
		if r := mo.Y - l.pickTop; r >= 0 && r < l.pickRows && p.first+r < len(p.items) {
			p.cursor = p.first + r
			return m.pick()
		}
	}
	return nil
}
