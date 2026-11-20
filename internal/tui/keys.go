package tui

import (
	"cmp"
	"context"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/emoji"
)

const (
	resizeStep = 4
	pageMsgs   = 5 // page_down / page_up
)

// key dispatches a key press: to the composer in INSERT, to a picker's
// filter while typing in it, to NORMAL mode otherwise (panes, picker lists,
// the help panel).
func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	m.err, m.status = "", ""
	if m.quitting {
		return m.quitKey(k)
	}
	if m.viewer != nil {
		return m.viewerKey(k)
	}
	switch {
	case m.mode == insert:
		return m.insertKey(msg, k)
	case m.cmdline != nil:
		return m.commandKey(msg, k)
	case m.help && m.sheet != nil && m.sheet.searching:
		return m.sheetTypingKey(msg, k)
	case m.picker != nil && m.picker.typing:
		return m.pickerTypingKey(msg, k)
	}
	return m.normalKey(k)
}

// activeInput is the composer INSERT mode types into: the thread's while the
// thread panel has the focus, the conversation's otherwise.
func (m *model) activeInput() *textarea.Model {
	if m.focus == paneThread && m.thread != nil {
		return &m.threadIn
	}
	return &m.chatIn
}

func (m *model) enterInsert() tea.Cmd {
	if m.current() == nil {
		return nil
	}
	if m.view != sideConv && (m.focus != paneThread || m.thread == nil) {
		return nil // a list view has no composer
	}
	if m.focus != paneThread || m.thread == nil {
		m.focus = paneChat
	}
	m.mode = insert
	m.lastInsert = ""
	return m.activeInput().Focus()
}

func (m *model) leaveInsert() {
	m.suggest = nil
	m.mode = normal
	m.chatIn.Blur()
	m.threadIn.Blur()
}

// typedSequence reports whether k completes a two-key binding of act whose
// first key was just typed as text (like "j k" for normal_mode), and then
// takes that first key back out of the text.
func (m *model) typedSequence(s scope, k string, act action, undo func()) bool {
	prev := m.lastInsert
	m.lastInsert = ""
	if prev != "" && m.km.is(s, prev+" "+k, act) {
		undo()
		return true
	}
	if m.km.prefix[s][k] {
		m.lastInsert = k
	}
	return false
}

func backspace() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyBackspace} }

func (m *model) insertKey(msg tea.KeyPressMsg, k string) tea.Cmd {
	in := m.activeInput()
	if cmd, ok := m.suggestKey(in, k); ok {
		return cmd
	}
	undo := func() { *in, _ = in.Update(backspace()) }
	if m.typedSequence(scInsert, k, actNormalMode, undo) {
		return m.normalModeFromInsert()
	}
	switch m.km.lookup[scInsert][k] {
	case actNormalMode:
		m.lastInsert = ""
		return m.normalModeFromInsert()
	case actSend:
		m.lastInsert = ""
		return m.submit(in)
	case actEditLast:
		if in.Value() == "" && m.editing == nil {
			return m.editLast()
		}
	case actPasteImage:
		if m.editing == nil {
			if _, ok := m.backend.(chat.Uploader); ok {
				return m.pasteImage()
			}
		}
	case actEmoji:
		return m.openEmoji(in)
	}
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	return tea.Batch(cmd, m.updateSuggest(in))
}

// normalModeFromInsert is normal_mode in a composer: it cancels an edit
// (staying in INSERT when the edit started there) or leaves INSERT.
func (m *model) normalModeFromInsert() tea.Cmd {
	m.replying = nil
	if e := m.editing; e != nil {
		m.cancelEdit()
		if e.fromInsert {
			return nil
		}
	}
	m.leaveInsert()
	return nil
}

// submit is send: it posts the composer's text, or saves the edit.
func (m *model) submit(in *textarea.Model) tea.Cmd {
	text := emoji.Expand(m.encodeMentions(m.attachKey(in), strings.TrimSpace(in.Value())))
	m.suggest = nil
	c := m.current()
	if c == nil {
		return nil
	}
	if e := m.editing; e != nil {
		m.cancelEdit() // puts the draft back
		if text == "" {
			return nil
		}
		if !e.fromInsert {
			m.leaveInsert()
		}
		return m.call(func(ctx context.Context) tea.Msg {
			msg, err := m.backend.Edit(ctx, e.convID, e.ts, text)
			return changedMsg{e.convID, msg, err}
		})
	}
	if f := m.attach[m.attachKey(in)]; f != nil {
		delete(m.attach, m.attachKey(in))
		in.Reset()
		up := *f
		if t := m.thread; in == &m.threadIn && t != nil {
			m.drafts[threadKey(t.convID, t.root)] = ""
			return m.upload(t.convID, t.root, text, up)
		}
		m.drafts[c.ID] = ""
		if r := m.replying; r != nil && r.convID == c.ID {
			m.replying = nil
			up.ReplyTo = r.ts
		}
		return m.upload(c.ID, "", text, up)
	}
	if text == "" {
		return nil
	}
	in.Reset()
	if t := m.thread; in == &m.threadIn && t != nil {
		m.drafts[threadKey(t.convID, t.root)] = ""
		return m.send(t.convID, t.root, text)
	}
	m.drafts[c.ID] = ""
	if r := m.replying; r != nil && r.convID == c.ID {
		m.replying = nil
		if rp, ok := m.backend.(chat.Replier); ok {
			return m.call(func(ctx context.Context) tea.Msg {
				msg, err := rp.Reply(ctx, r.convID, r.ts, text)
				if err == nil && msg.Quote == nil { // shown at once as what it answers
					msg.Quote = &chat.Quote{Who: r.who, Text: r.text}
				}
				return sentMsg{r.convID, "", msg, err}
			})
		}
	}
	return m.send(c.ID, "", text)
}

// replyTo is the message a reply being written answers.
type replyTo struct {
	convID, ts, who, text string
}

// startReply writes a reply to msg in the chat composer, Telegram style:
// it is posted in the chat, quoting msg.
func (m *model) startReply(msg *chat.Message) tea.Cmd {
	c := m.current()
	if msg == nil || c == nil {
		return m.enterInsert()
	}
	m.cancelEdit()
	text := strings.Join(strings.Fields(cmp.Or(msg.Text, "a message")), " ")
	m.replying = &replyTo{convID: c.ID, ts: msg.TS, who: msg.User, text: text}
	m.focus = paneChat
	return m.enterInsert()
}

// startEdit loads one of the user's messages into the composer of the pane
// it is in.
func (m *model) startEdit(msg *chat.Message, fromInsert bool) tea.Cmd {
	if msg == nil {
		return nil
	}
	if msg.User != m.backend.Me() {
		m.status = "only your own messages can be edited"
		return nil
	}
	cmd := m.enterInsert()
	in := m.activeInput()
	m.editing = &edit{convID: m.convID(), ts: msg.TS, draft: in.Value(), fromInsert: fromInsert}
	in.SetValue(m.decodeMentions(m.attachKey(in), cmp.Or(msg.Raw, msg.Text))) // the markup: mentions stay mentions
	return cmd
}

// editLast is edit_last: the user's newest message of the thread or the
// conversation being typed in.
func (m *model) editLast() tea.Cmd {
	msgs := m.history[m.current().ID]
	if m.activeInput() == &m.threadIn {
		msgs = m.thread.msgs
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].User == m.backend.Me() {
			if m.activeInput() == &m.threadIn {
				m.thread.sel = i
			} else {
				m.sel[m.current().ID] = i
			}
			return m.startEdit(&msgs[i], true)
		}
	}
	m.status = "nothing of yours to edit here"
	return nil
}

func (m *model) cancelEdit() {
	e := m.editing
	if e == nil {
		return
	}
	m.editing = nil
	m.activeInput().SetValue(e.draft)
}

func (m *model) normalKey(k string) tea.Cmd {
	key := k
	if m.pending != "" {
		if _, ok := m.km.lookup[scNormal][m.pending+" "+k]; ok {
			key = m.pending + " " + k
		}
		m.pending = ""
	}
	if m.km.prefix[scNormal][key] {
		m.pending = key
		return nil
	}
	a := m.km.lookup[scNormal][key]
	switch {
	case m.picker != nil:
		return m.pickerListAction(a)
	case m.help && m.sheet != nil && m.sheet.links != nil:
		if cmd, ok := m.linkKey(k, a); ok {
			return cmd
		}
		return m.sheetAction(a)
	case m.help:
		return m.sheetAction(a)
	case m.settings != nil:
		return m.settingsAction(a)
	}
	return m.paneAction(a)
}

func (m *model) paneAction(a action) tea.Cmd {
	if a == actWho {
		return m.who()
	}
	if a == actYank {
		if msg := m.target(); msg != nil {
			m.status = "copied message"
			return tea.SetClipboard(msg.Text)
		}
		m.status = "no message selected"
		return nil
	}
	if a == actThread {
		if m.focus == paneChat && m.view == sideConv {
			return m.openThread()
		}
		a = actOpen // elsewhere it opens as enter does
	}
	if n := accountOf(a); n > 0 {
		return func() tea.Msg { return tabMsg{index: n} }
	}
	if m.view != sideConv && m.focus == paneChat {
		if cmd, done := m.listAction(a); done {
			return cmd
		}
	}
	switch a {
	case actQuit:
		m.quitting = true
	case actBackOrQuit:
		m.backOrQuit()
	case actToggleRead:
		return m.toggleRead()
	case actFold:
		if m.focus == paneSide {
			m.toggleSection("")
		}
	case actUnreadOnly:
		m.unreadOnly = !m.unreadOnly
		m.status = map[bool]string{true: "sidebar: unread chats only", false: "sidebar: all chats"}[m.unreadOnly]
	case actGrow:
		m.setInputRows(m.inputRows + 1)
	case actShrink:
		m.setInputRows(m.inputRows - 1)
	case actHelp:
		m.openKeys()
	case actSwitcher:
		return m.openPicker(pickSwitch, nil)
	case actSearch:
		return m.openSearch(false)
	case actFilter:
		if m.focus == paneSide {
			return m.openPicker(pickSwitch, nil)
		}
		return m.openSearch(true)
	case actNextMatch:
		return m.nextMatch(1)
	case actPrevMatch:
		return m.nextMatch(-1)
	case actInsert:
		return m.enterInsert()
	case actFocusLeft:
		m.cycleFocus(-1)
	case actFocusRight:
		m.cycleFocus(1)
	case actOpenRight:
		// The right arrow enters the selected message's thread when the chat
		// is the rightmost open pane; once it is open, it moves into that pane.
		if m.focus == paneChat && m.thread == nil {
			return m.openThread()
		}
		m.cycleFocus(1)
	case actToggleFocus:
		// Tab deliberately ignores the optional thread pane: it is the quick
		// sidebar ⇄ current-chat toggle, like the two primary Slack regions.
		if m.focus == paneSide {
			m.focus = paneChat
		} else {
			m.focus = paneSide
		}
	case actRedraw:
		return tea.ClearScreen
	case actRefresh:
		m.status = "refreshing…"
		return m.refresh()
	case actNextTab:
		return func() tea.Msg { return tabMsg{delta: 1} }
	case actPrevTab:
		return func() tea.Msg { return tabMsg{delta: -1} }
	case actNextChat:
		return m.sideMove(1)
	case actPrevChat:
		return m.sideMove(-1)
	case actDown:
		return m.move(1)
	case actUp:
		return m.move(-1)
	case actPageDown:
		return m.move(pageMsgs)
	case actPageUp:
		return m.move(-pageMsgs)
	case actTop:
		return m.moveTo(0)
	case actBottom:
		return m.moveTo(-1)
	case actDivLeft, actDivRight:
		m.nudgeDivider(map[action]int{actDivLeft: -resizeStep, actDivRight: resizeStep}[a])
	case actDivReset:
		m.setSide(defaultSide)
		if m.thread != nil {
			m.setThreadW(m.width * 2 / 5)
		}
	case actTheme:
		return m.openPicker(pickTheme, nil)
	case actSettings:
		m.openSettings()
	case actStatus:
		return m.openStatus()
	case actCommand:
		return m.openCommand()
	case actImages:
		m.toggleImages()
	case actLayout:
		l := layoutPanels
		if boxed() {
			l = layoutSlack
		}
		m.setTheme(m.themeName, l)
		m.status = `layout ` + l + ` · "layout": "` + l + `" in the config keeps it`
	case actOpen:
		switch m.focus {
		case paneSide:
			if m.onSection != "" { // enter on a header folds it, as space
				m.toggleSection(m.onSection)
				return nil
			}
			m.focus = paneChat // Update reads it
		case paneChat:
			if _, ok := m.backend.(chat.Replier); ok {
				return m.startReply(m.selectedMsg())
			}
			return m.openThread()
		}
	case actReply:
		if m.focus == paneSide {
			return nil
		}
		return tea.Batch(m.openThread(), m.enterInsert())
	case actReact:
		if t := m.target(); t != nil && m.focus != paneSide {
			return m.openPicker(pickReact, t)
		}
	case actReactions:
		if m.focus != paneSide {
			m.openReactions(m.target())
		}
	case actEdit:
		if m.focus != paneSide {
			return m.startEdit(m.target(), false)
		}
	case actLinks:
		if m.focus != paneSide {
			m.openLinks(m.target())
		}
	case actView:
		if m.focus != paneSide && m.view == sideConv || m.focus == paneThread {
			return m.openViewer(m.target(), -1)
		}
	case actBack:
		if m.found != nil && m.focus != paneSide {
			m.found = nil // esc first drops the search marks
			return nil
		}
		switch m.focus {
		case paneChat:
			m.focus = paneSide
		case paneThread:
			m.closeThread()
		}
	}
	return nil
}

// cycleFocus moves the focus to the pane on the left (-1) or right (+1).
func (m *model) cycleFocus(d int) {
	last := paneChat
	if m.thread != nil {
		last = paneThread
	}
	m.focus = max(paneSide, min(last, m.focus+pane(d)))
}

// nudgeDivider moves the divider next to the focused pane: the thread
// panel's when it has the focus, the sidebar's otherwise.
func (m *model) nudgeDivider(d int) {
	if m.focus == paneThread && m.thread != nil {
		m.setThreadW(m.threadW - d) // the divider moves left: the panel grows
		return
	}
	m.setSide(m.side + d)
}

// move is down/up: the sidebar cursor, or the selected message of the
// conversation or the thread (positive is newer).
func (m *model) move(n int) tea.Cmd {
	switch m.focus {
	case paneSide:
		return m.sideMove(n)
	case paneChat:
		if c := m.current(); c != nil && len(m.history[c.ID]) > 0 {
			m.sel[c.ID] = max(0, min(m.selected()+n, len(m.history[c.ID])-1))
		}
	case paneThread:
		if t := m.thread; t != nil && len(t.msgs) > 0 {
			t.sel = max(0, min(t.sel+n, len(t.msgs)-1))
		}
	}
	return nil
}

// moveTo is top (0) and bottom (-1).
func (m *model) moveTo(i int) tea.Cmd {
	switch m.focus {
	case paneSide:
		return m.sideTo(i == 0)
	case paneChat:
		if c := m.current(); c != nil && len(m.history[c.ID]) > 0 {
			if i < 0 {
				i = len(m.history[c.ID]) - 1
			}
			m.sel[c.ID] = i
		}
	case paneThread:
		if t := m.thread; t != nil && len(t.msgs) > 0 {
			if i < 0 {
				i = len(t.msgs) - 1
			}
			t.sel = i
		}
	}
	return nil
}

// backOrQuit is ctrl+c in NORMAL mode: it steps back (thread, then the
// conversation's focus) and asks to quit only from the sidebar.
func (m *model) backOrQuit() {
	switch {
	case m.found != nil:
		m.found = nil
	case m.thread != nil:
		m.closeThread()
		m.focus = paneChat
	case m.focus != paneSide:
		m.focus = paneSide
	default:
		m.quitting = true
	}
}

// quitKey answers the quit confirmation: y, enter or the quit keys again
// quit; anything else stays.
func (m *model) quitKey(k string) tea.Cmd {
	m.quitting = false
	if k == "y" || k == "enter" || m.km.is(scNormal, k, actQuit) || m.km.is(scNormal, k, actBackOrQuit) {
		if seq := m.forgetImages(); seq != "" {
			return tea.Sequence(tea.Raw(seq), tea.Quit)
		}
		return tea.Quit
	}
	return nil
}

// listAction is what the keys do in the Threads and Activity lists; done is
// false for keys that mean the same as anywhere else.
func (m *model) listAction(a action) (tea.Cmd, bool) {
	switch a {
	case actDown:
		m.listMove(1)
	case actUp:
		m.listMove(-1)
	case actPageDown:
		m.listMove(pageMsgs)
	case actPageUp:
		m.listMove(-pageMsgs)
	case actTop:
		m.listTo(true)
	case actBottom:
		m.listTo(false)
	case actOpen:
		return m.listOpen(false), true
	case actReply, actInsert:
		if m.view == sideThreads {
			return m.listOpen(true), true
		}
	case actReact, actEdit:
	default:
		return nil, false
	}
	return nil, true
}
