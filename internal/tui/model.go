// Package tui is sltty's terminal UI: a sidebar of conversations, the open
// conversation and, on demand, a thread panel, side by side with draggable
// dividers, driven by vim-like NORMAL and INSERT modes and a configurable
// keymap.
package tui

import (
	"cmp"
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/img"
)

type mode int

const (
	normal mode = iota
	insert
)

type pane int

const (
	paneSide pane = iota
	paneChat
	paneThread
)

const (
	minSide      = 14 // sidebar columns
	minMain      = 30
	minThread    = 28
	defaultSide  = 26
	defaultInput = 2 // text lines of a composer
	doubleClick  = 400 * time.Millisecond
	callTimeout  = 15 * time.Second
)

type (
	cachedHistoryMsg struct {
		id   string
		msgs []chat.Message
		hit  bool
	}
	convsMsg struct {
		convs []chat.Conversation
		err   error
	}
	historyMsg struct {
		id   string
		msgs []chat.Message
		err  error
	}
	repliesMsg struct {
		id, root string
		msgs     []chat.Message
		err      error
	}
	sentMsg struct {
		id, root string
		msg      chat.Message
		err      error
	}
	// replyPollMsg refreshes after sending because app/bot replies arrive
	// asynchronously and this client deliberately has no socket connection.
	replyPollMsg struct {
		id, root string
		attempt  int
	}
	// markedMsg is the backend's answer to a read cursor move; the TUI
	// has already counted what it leaves unread.
	markedMsg struct {
		id  string
		err error
	}
	// changedMsg is an edited or reacted-to message, to replace in place.
	changedMsg struct {
		id  string
		msg chat.Message
		err error
	}
)

// thread is the open thread panel.
type thread struct {
	convID, root string
	msgs         []chat.Message // root first; nil while loading
	sel          int            // selected message
	off          int            // lines above the bottom
}

// edit is a message being edited in a composer.
type edit struct {
	convID, ts string
	draft      string // the composer's text before, put back after
	fromInsert bool   // started with edit_last: INSERT stays on after it
}

// jump is a search result to select once its conversation (and thread) is
// loaded.
type jump struct{ convID, ts, root string }

// Options configures the TUI.
type Options struct {
	Keys           map[string][]string // overrides from the config file
	Theme          string              // theme name; empty for the default
	Layout         string              // "slack" or "panels"; empty for the theme's
	Clock          string              // "24h" (15:04) or "12h" (3:04 PM); empty for 24h
	Themes         map[string]json.RawMessage
	Sections       []Section // custom sidebar groups
	PrivateIcon    string    // replaces 🔒 for private channels
	Images         string    // "auto", "kitty", "halfblock" or "off"
	CustomEmoji    bool      // custom :emoji: as pictures (kitty)
	ImageWidth     int       // the largest picture, in cells; 0 for the default
	ImageHeight    int
	ComposerHeight int // text rows; 0 for the default
	SidebarWidth   int // columns; 0 for the default
	// HistoryCacheTTL is the on-disk history cache lifetime; 0 disables it.
	HistoryCacheTTL time.Duration
	// ReadOnEnter marks a conversation read when its messages receive focus.
	ReadOnEnter *bool

	Version     string
	ConfigPath  string
	ConfigFound bool
	LogPath     string
	StatePath   string // non-empty enables local UI state persistence
}

type model struct {
	backend chat.Backend
	km      *keymap
	opts    Options
	themes  *themeSet
	// The theme and layout in use: the config's, or what T and L picked.
	themeName, layoutName string

	convs   []chat.Conversation // channels first, then DMs
	cursor  int                 // sidebar selection
	history map[string][]chat.Message
	cache   *historyCache
	sel     map[string]int // selected message per conversation
	chatOff int            // chat lines above the bottom
	thread  *thread
	editing *edit

	// The Threads and Activity views (view != sideConv) and what was read
	// in them here.
	view         sideKind
	threads      []chat.ThreadSummary
	activity     []chat.Activity
	listSel      [3]int // selected entry per view
	listOff      int
	readThreads  map[string]int    // thread key → replies seen
	readActivity map[string]bool   // activity ID → read (true) or marked unread
	unreadThread map[string]bool   // thread key → marked unread with u
	collapsed    map[string]bool   // section name → folded, once toggled
	onSection    string            // the sidebar cursor is on this section's header
	newFrom      map[string]string // conversation → first unread message, for the New line
	found        *found            // the last search picked from: n/N walk it, its words are marked
	imgs         images
	viewer       *viewer // a picture full size, while set

	cmdline     *textinput.Model // COMMAND mode while set
	sheet       *sheet           // the keybindings or config sheet, while help is set
	settings    *settings        // the settings panel while set
	cmdHistory  []string
	cmdHist     int  // the entry up/down are on, -1 for none
	away        bool // presence
	readOnEnter bool
	snoozeTill  time.Time
	connected   bool // the last backend reply worked
	replied     bool // there was one
	jump        *jump
	// Last position restored from local state after its conversation loads.
	restoreConv, restoreTS string
	restoreOff             int

	mode       mode
	focus      pane
	side       int // sidebar width
	threadW    int // thread panel width; 0 until first opened
	drag       int // divider or text selection being dragged
	selectDrag struct {
		pane           pane
		startX, startY int // terminal cells, including the frame
		endX, endY     int
	}
	pending    string
	lastInsert string // the key just typed, for two-key bindings in INSERT
	help       bool
	helpOff    int
	quitting   bool // the quit confirmation is up
	inputRows  int  // text lines of the composers; drag their top border
	lastClick  struct {
		convID string
		msg    int
		at     time.Time
	}
	now    func() time.Time
	picker *picker

	loading  bool // the conversations are on their way: the loading modal is up
	spin     spinner.Model
	spinning bool // a spinner tick is in flight

	chatIn, threadIn textarea.Model
	drafts           map[string]string // unsent text per conversation or thread

	width, height int
	status, err   string
	layout        layout
}

const (
	dragSide = iota + 1
	dragThread
	dragComposer
	dragText
)

func (m *model) composerH() int { return m.inputRows + 2 }

// setInputRows sets the composers' height, leaving the messages room.
func (m *model) setInputRows(n int) {
	m.inputRows = max(1, min(n, m.contentRows()/2))
}

func newComposer(km *keymap) textarea.Model {
	in := textarea.New()
	in.Prompt = ""
	in.ShowLineNumbers = false
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys(km.keys[actNewline]...))
	s := in.Styles()
	s.Focused.CursorLine = lipgloss.NewStyle()
	s.Focused.Placeholder = dimStyle
	s.Blurred.Placeholder = dimStyle
	in.SetStyles(s)
	in.MaxHeight = 0 // the user can drag it taller
	in.SetHeight(defaultInput)
	return in
}

func newModel(b chat.Backend, km *keymap, opts Options) *model {
	ts, _ := loadThemes(nil)
	hour12 = opts.Clock == "12h"
	return &model{
		backend:      b,
		km:           km,
		opts:         opts,
		themes:       ts,
		themeName:    cmpOr(opts.Theme, defaultTheme),
		layoutName:   opts.Layout,
		history:      map[string][]chat.Message{},
		cache:        newHistoryCache(b.Workspace(), opts.HistoryCacheTTL),
		sel:          map[string]int{},
		drafts:       map[string]string{},
		readThreads:  map[string]int{},
		readActivity: map[string]bool{},
		unreadThread: map[string]bool{},
		collapsed:    map[string]bool{},
		newFrom:      map[string]string{},
		imgs:         newImages(opts),
		side:         cmp.Or(opts.SidebarWidth, defaultSide),
		inputRows:    cmp.Or(opts.ComposerHeight, defaultInput),
		readOnEnter:  opts.ReadOnEnter == nil || *opts.ReadOnEnter,
		now:          time.Now,
		spin:         spinner.New(spinner.WithSpinner(spinner.Dot)),
		chatIn:       newComposer(km),
		threadIn:     newComposer(km),
	}
}

func (m *model) setTheme(name, layout string) {
	if t, err := m.themes.get(name, layout); err == nil {
		m.themeName, m.layoutName = name, layout
		applyTheme(t)
	}
}

func (m *model) Init() tea.Cmd {
	m.loading = true
	return tea.Batch(m.call(func(ctx context.Context) tea.Msg {
		c, err := m.backend.Conversations(ctx)
		return convsMsg{c, err}
	}), m.loadPresence(), m.loadThreads(), m.loadActivity(), m.startSpinner(), graphemeWidthCmd(os.Getenv))
}

// graphemeWidthCmd makes the renderer measure text as the layout does.
// Bubble Tea only switches to grapheme widths when a terminal answers the
// mode 2027 query; kitty, Ghostty and WezTerm already draw emoji presentation
// sequences (⚠️, ✈️) two cells wide but do not always answer it. Measured
// with wcwidth as one cell, such a line overflows and wraps, and the next
// frames repeat rows. The mode report is synthesized for those terminals.
func graphemeWidthCmd(getenv func(string) string) tea.Cmd {
	term, prog := getenv("TERM"), strings.ToLower(getenv("TERM_PROGRAM"))
	if getenv("KITTY_WINDOW_ID") == "" && term != "xterm-kitty" && term != "xterm-ghostty" && prog != "ghostty" && prog != "wezterm" {
		return nil
	}
	return func() tea.Msg { return tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet} }
}

// busy is whether something on screen waits for the backend with a spinner.
func (m *model) busy() bool { return m.loading }

// startSpinner sets the spinner going when work starts; it stops itself on
// the first tick after everything has loaded, so an idle screen never
// redraws.
func (m *model) startSpinner() tea.Cmd {
	if m.spinning || !m.busy() {
		return nil
	}
	m.spinning = true
	return m.spin.Tick
}

// call runs a backend call off the UI loop, with a timeout.
func (m *model) call(f func(context.Context) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		return f(ctx)
	}
}

func (m *model) loadHistory(id string) tea.Cmd {
	return m.call(func(ctx context.Context) tea.Msg {
		h, err := m.backend.History(ctx, id)
		if err == nil {
			m.cache.save(id, h)
		}
		return historyMsg{id, h, err}
	})
}

func (m *model) loadCachedHistory(id string) tea.Cmd {
	if m.cache == nil {
		return m.loadHistory(id)
	}
	return func() tea.Msg {
		h, hit := m.cache.load(id)
		return cachedHistoryMsg{id: id, msgs: h, hit: hit}
	}
}

func (m *model) loadReplies(id, root string) tea.Cmd {
	return m.call(func(ctx context.Context) tea.Msg {
		r, err := m.backend.Replies(ctx, id, root)
		return repliesMsg{id, root, r, err}
	})
}

func (m *model) send(id, root, text string) tea.Cmd {
	return m.call(func(ctx context.Context) tea.Msg {
		msg, err := m.backend.Send(ctx, id, root, text)
		return sentMsg{id, root, msg, err}
	})
}

// pollForReply checks a few times after posting. This makes responses from
// Slack apps visible without continuously polling every open conversation.
func pollForReply(id, root string, attempt int) tea.Cmd {
	return tea.Tick(time.Second*time.Duration(1<<attempt), func(time.Time) tea.Msg {
		return replyPollMsg{id: id, root: root, attempt: attempt}
	})
}

// current is the conversation selected in the sidebar, if any.
func (m *model) current() *chat.Conversation {
	if m.cursor < 0 || m.cursor >= len(m.convs) {
		return nil
	}
	return &m.convs[m.cursor]
}

// selected is the index of the selected message of the open conversation,
// or -1 when it has none.
func (m *model) selected() int {
	c := m.current()
	if c == nil {
		return -1
	}
	n := len(m.history[c.ID])
	i, ok := m.sel[c.ID]
	if !ok || i >= n {
		i = n - 1
	}
	return i
}

func (m *model) selectedMsg() *chat.Message {
	c, i := m.current(), m.selected()
	if c == nil || i < 0 {
		return nil
	}
	return &m.history[c.ID][i]
}

// target is the message NORMAL-mode actions (react, edit) apply to: the
// selected one of the thread when it has the focus, of the conversation
// otherwise.
func (m *model) target() *chat.Message {
	if t := m.thread; m.focus == paneThread && t != nil {
		if t.sel >= 0 && t.sel < len(t.msgs) {
			return &t.msgs[t.sel]
		}
		return nil
	}
	return m.selectedMsg()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.noteConnection(msg)
	before, beforeView := m.spot(), m.view
	_, cmd := m.update(msg)
	if read := m.entered(before); read != nil {
		cmd = tea.Batch(cmd, read)
	}
	if more := m.imageCmds(); more != nil {
		cmd = tea.Batch(cmd, more)
	}
	if after := m.spot(); after.conv != before.conv || m.view != beforeView {
		// The renderer only rewrites cells it believes changed. If the terminal
		// ever disagrees with it (a glyph drawn at another width), stale cells
		// survive and leak colours or rows across panes. Switching what the
		// main pane shows nearly replaces the screen anyway: repaint it whole.
		cmd = tea.Batch(cmd, tea.ClearScreen)
	}
	return m, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.busy() {
			m.spinning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case pictureMsg:
		return m, m.gotPicture(msg)
	case bigPictureMsg:
		return m, m.gotBigPicture(msg)
	case presenceMsg:
		if msg.err != nil {
			m.err = "presence: " + msg.err.Error()
			return m, nil
		}
		m.away, m.snoozeTill = msg.away, time.Time{}
		if msg.set {
			m.status = "you are active"
			if msg.away {
				m.status = "you are away"
			}
		}
		return m, nil
	case snoozeMsg:
		if msg.err != nil {
			m.err = "snooze: " + msg.err.Error()
			return m, nil
		}
		m.snoozeTill = time.Time{}
		if msg.minutes > 0 {
			m.snoozeTill = time.Now().Add(time.Duration(msg.minutes) * time.Minute)
			m.status = "Do Not Disturb enabled"
		} else {
			m.status = "Do Not Disturb ended"
		}
		return m, nil
	case customEmojiMsg:
		for _, n := range msg.names {
			m.imgs.custom[n] = true
		}
		if p := m.picker; p != nil && p.kind == pickReact && len(msg.names) > 0 {
			m.refreshPicker() // what was typed may name one
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.setSide(m.side)
		var cmds []tea.Cmd
		if m.imgs.mode != img.Off {
			// The cell size in pixels (a font change resizes too): pictures
			// fit to it leave no blank around them.
			cmds = append(cmds, tea.Raw(cellSizeQuery))
		}
		if m.viewer != nil {
			cmds = append(cmds, m.loadViewer()) // at the new size
		}
		return m, tea.Batch(cmds...)
	case uv.CellSizeEvent:
		return m, m.setCell(msg.Width, msg.Height)
	case convsMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.convs = msg.convs
		slices.SortStableFunc(m.convs, func(a, b chat.Conversation) int { return cmp.Compare(a.Kind, b.Kind) })
		i := 0
		if m.restoreConv != "" {
			for n, c := range m.convs {
				if c.ID == m.restoreConv {
					i = n
					break
				}
			}
		}
		return m, m.selectConv(i)
	case cachedHistoryMsg:
		if msg.hit {
			m.history[msg.id] = msg.msgs
		}
		// The cached view is immediate; Slack refreshes it in the background.
		return m, m.loadHistory(msg.id)
	case historyMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.history[msg.id] = msg.msgs
		if msg.id == m.restoreConv {
			if i := slices.IndexFunc(msg.msgs, func(x chat.Message) bool { return x.TS == m.restoreTS }); i >= 0 {
				m.sel[msg.id] = i
			}
			m.chatOff = m.restoreOff
		}
		if c, ok := m.convByID(msg.id); ok {
			m.noteNew(msg.id, c.Unread)
		}
		var read tea.Cmd
		if m.inChat() == msg.id { // entered before it loaded
			read = m.readIn(msg.id)
		}
		if m.jump != nil && m.jump.convID == msg.id {
			return m, tea.Batch(read, m.applyJump())
		}
		return m, read
	case repliesMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		if t := m.thread; t != nil && t.convID == msg.id && t.root == msg.root {
			t.msgs, t.sel = msg.msgs, len(msg.msgs)-1
			if j := m.jump; j != nil && j.root == msg.root {
				t.sel = max(0, slices.IndexFunc(t.msgs, func(x chat.Message) bool { return x.TS == j.ts }))
				m.jump = nil
			}
		}
		return m, nil
	case sentMsg:
		cmd := m.sent(msg)
		if msg.err == nil {
			cmd = tea.Batch(cmd, pollForReply(msg.id, msg.root, 0))
		}
		if msg.root != "" && msg.err == nil {
			cmd = tea.Batch(cmd, m.loadThreads()) // the thread is now one of the user's
		}
		return m, cmd
	case replyPollMsg:
		var refresh tea.Cmd
		if msg.root != "" {
			refresh = m.loadReplies(msg.id, msg.root)
		} else {
			refresh = m.loadHistory(msg.id)
		}
		if msg.attempt >= 3 {
			return m, refresh
		}
		return m, tea.Batch(refresh, pollForReply(msg.id, msg.root, msg.attempt+1))
	case threadsMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.threads = msg.threads
		if m.threads == nil {
			m.threads = []chat.ThreadSummary{}
		}
		return m, m.imageCmds()
	case activityMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.activity = msg.items
		if m.activity == nil {
			m.activity = []chat.Activity{}
		}
		return m, m.imageCmds()
	case markedMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		return m, nil
	case changedMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.replaceMsg(msg.id, msg.msg)
		return m, nil
	case searchTick:
		return m, m.searchNow(msg.query)
	case searchMsg:
		m.searchResults(msg)
		return m, nil
	case openedMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		return m, nil
	case tea.MouseMsg:
		return m, m.mouse(msg)
	case tea.KeyPressMsg:
		return m, m.key(msg)
	}
	// Anything else (the cursor blink) goes to whatever is being typed in.
	var cmd tea.Cmd
	switch {
	case m.picker != nil && m.picker.typing:
		m.picker.input, cmd = m.picker.input.Update(msg)
	case m.mode == insert:
		in := m.activeInput()
		*in, cmd = in.Update(msg)
	}
	return m, cmd
}
