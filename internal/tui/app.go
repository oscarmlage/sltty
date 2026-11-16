package tui

// The accounts: several chat services (a Slack workspace, Telegram...) in
// one terminal, each in a tab. Every account is a whole model of its own
// (its conversations, history, drafts, searches); the app shows the active
// one under a tab bar, routes what each one's commands bring back to it,
// and keeps the visual settings the same in all of them.

import (
	"fmt"
	"reflect"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/chat"
)

// tabMsg asks for another account: delta tabs away, or tab index when set.
type tabMsg struct {
	delta int
	index int // 1-based; 0 uses delta
}

// acctMsg is what one account's command brought back, for that account.
type acctMsg struct {
	i   int
	msg tea.Msg
}

type app struct {
	tabs          []*model
	active        int
	width, height int
}

func newApp(tabs []*model) *app { return &app{tabs: tabs} }

var ownPkg = reflect.TypeFor[app]().PkgPath()

// owned tells the messages an account's commands produce for that account
// (this package's, and the spinner's ticks) from the program's own (raw
// output, quit, batches), which must reach Bubble Tea untouched.
func owned(msg tea.Msg) bool {
	if _, ok := msg.(spinner.TickMsg); ok {
		return true
	}
	t := reflect.TypeOf(msg)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t != nil && t.PkgPath() == ownPkg
}

// wrap tags what cmd brings back with account i.
func (a *app) wrap(i int, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg { return tag(i, cmd()) }
}

func tag(i int, msg tea.Msg) tea.Msg {
	switch msg := msg.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		out := make(tea.BatchMsg, 0, len(msg))
		for _, c := range msg {
			if c != nil {
				c := c
				out = append(out, func() tea.Msg { return tag(i, c()) })
			}
		}
		return out
	}
	if owned(msg) {
		return acctMsg{i, msg}
	}
	return msg
}

func (a *app) cur() *model { return a.tabs[a.active] }

// bar is the tab bar's height: none for a single account.
func (a *app) bar() int {
	if len(a.tabs) > 1 {
		return 1
	}
	return 0
}

func (a *app) Init() tea.Cmd {
	cmds := make([]tea.Cmd, len(a.tabs))
	for i, m := range a.tabs {
		cmds[i] = a.wrap(i, m.Init())
	}
	return tea.Batch(cmds...)
}

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case acctMsg:
		if t, ok := msg.msg.(tabMsg); ok {
			return a, a.switchTab(t)
		}
		return a, a.send(msg.i, msg.msg)
	case tabMsg:
		return a, a.switchTab(msg)
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		return a, a.broadcast(tea.WindowSizeMsg{Width: msg.Width, Height: max(1, msg.Height-a.bar())})
	case uv.CellSizeEvent:
		return a, a.broadcast(msg)
	case tea.KeyPressMsg:
		if t, ok := a.tabKey(msg); ok {
			return a, a.switchTab(t)
		}
	case tea.MouseMsg:
		return a, a.mouse(msg)
	}
	return a, a.send(a.active, msg)
}

func (a *app) send(i int, msg tea.Msg) tea.Cmd {
	if i < 0 || i >= len(a.tabs) {
		return nil
	}
	_, cmd := a.tabs[i].Update(msg)
	return a.wrap(i, cmd)
}

func (a *app) broadcast(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(a.tabs))
	for i := range a.tabs {
		cmds[i] = a.send(i, msg)
	}
	return tea.Batch(cmds...)
}

// tabKey is alt+1…9: that tab, from any mode (no text is typed with alt).
func (a *app) tabKey(k tea.KeyPressMsg) (tabMsg, bool) {
	s := k.String()
	if len(a.tabs) < 2 || !strings.HasPrefix(s, "alt+") || len(s) != 5 || s[4] < '1' || s[4] > '9' {
		return tabMsg{}, false
	}
	return tabMsg{index: int(s[4] - '0')}, true
}

// switchTab moves to another account, carrying the visual settings over:
// they are one set for the whole program.
func (a *app) switchTab(t tabMsg) tea.Cmd {
	n := len(a.tabs)
	if n < 2 {
		return nil
	}
	next := (a.active + t.delta%n + n) % n
	if t.index > 0 {
		if t.index > n {
			return nil
		}
		next = t.index - 1
	}
	if next == a.active {
		return nil
	}
	from := a.cur()
	a.active = next
	to := a.cur()
	shareSettings(from, to)
	// Different content everywhere: repaint the screen whole.
	return tea.Batch(tea.ClearScreen, a.wrap(next, to.imageCmds()))
}

// shareSettings gives to the visual settings of from: theme, layout,
// sizes, images, reading and the history cache.
func shareSettings(from, to *model) {
	to.themeName, to.layoutName = from.themeName, from.layoutName
	to.setInputRows(from.inputRows)
	to.setSide(from.side)
	if to.imgs.on != from.imgs.on {
		to.toggleImages()
	}
	to.imgs.emoji = from.imgs.emoji
	to.readOnEnter = from.readOnEnter
	if to.opts.HistoryCacheTTL != from.opts.HistoryCacheTTL {
		to.opts.HistoryCacheTTL = from.opts.HistoryCacheTTL
		to.cache = newHistoryCache(to.backend.Workspace(), to.opts.HistoryCacheTTL)
	}
}

// mouse goes to the active account, under the tab bar; a click on the bar
// picks a tab.
func (a *app) mouse(msg tea.MouseMsg) tea.Cmd {
	if a.bar() == 0 {
		return a.send(a.active, msg)
	}
	mo := msg.Mouse()
	if mo.Y < a.bar() {
		if _, ok := msg.(tea.MouseClickMsg); ok && mo.Button == tea.MouseLeft {
			for i, r := range a.tabSpans() {
				if mo.X >= r[0] && mo.X < r[1] {
					return a.switchTab(tabMsg{index: i + 1})
				}
			}
		}
		return nil
	}
	mo.Y -= a.bar()
	var shifted tea.Msg
	switch msg.(type) {
	case tea.MouseClickMsg:
		shifted = tea.MouseClickMsg(mo)
	case tea.MouseReleaseMsg:
		shifted = tea.MouseReleaseMsg(mo)
	case tea.MouseWheelMsg:
		shifted = tea.MouseWheelMsg(mo)
	case tea.MouseMotionMsg:
		shifted = tea.MouseMotionMsg(mo)
	default:
		return nil
	}
	return a.send(a.active, shifted)
}

// serviceName names the chat service behind an account, for messages.
func (m *model) serviceName() string {
	switch m.backend.(type) {
	case *chat.Telegram:
		return "Telegram"
	case *chat.Slack:
		return "Slack"
	}
	return "The chat service"
}

// reactHint is the reaction picker's placeholder: what the service allows.
func (m *model) reactHint() string {
	if _, ok := m.backend.(*chat.Telegram); ok {
		return "emoji name (Telegram allows a set of reactions per chat)"
	}
	return "emoji name: any of Slack's or the workspace's"
}

// unread is what a tab counts: the unread messages of the account's
// conversations, muted ones aside.
func unread(m *model) int {
	n := 0
	for _, c := range m.convs {
		if !c.Muted {
			n += c.Unread
		}
	}
	return n
}

func (a *app) tabLabel(i int) string {
	m := a.tabs[i]
	s := fmt.Sprintf(" %d %s ", i+1, m.backend.Workspace())
	if n := unread(m); n > 0 {
		s += count(n) + " "
	}
	return s
}

// tabSpans are the columns each tab takes on the bar.
func (a *app) tabSpans() [][2]int {
	out := make([][2]int, len(a.tabs))
	x := 0
	for i := range a.tabs {
		w := ansi.StringWidth(a.tabLabel(i))
		out[i] = [2]int{x, x + w}
		x += w + 1
	}
	return out
}

func (a *app) tabBar() string {
	var b strings.Builder
	for i := range a.tabs {
		if i > 0 {
			b.WriteString(" ")
		}
		label := a.tabLabel(i)
		if i == a.active {
			b.WriteString(sbSelText.Render(label))
			continue
		}
		style := sbMuted
		if unread(a.tabs[i]) > 0 {
			style = sbBright
		}
		b.WriteString(style.Render(label))
	}
	return paint(b.String(), a.width, sbBg)
}

// activeTab paints the active tab's background over its label.
func (a *app) activeTab(bar string) string {
	r := a.tabSpans()[a.active]
	before := ansi.Cut(bar, 0, r[0])
	label := ansi.Cut(bar, r[0], r[1])
	after := ansi.Cut(bar, r[1], a.width)
	return before + paint(label, r[1]-r[0], sbSelFocusBg) + sbBg + after
}

func (a *app) View() tea.View {
	v := a.cur().View()
	if a.bar() > 0 && a.width > 0 {
		v.Content = a.activeTab(a.tabBar()) + "\n" + v.Content
	}
	return v
}
