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
	"charm.land/lipgloss/v2"
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

// tabKey is 1…9 in NORMAL mode with nothing open over the panes (a list,
// a sheet, the command line take their own keys), and alt+1…9 from any
// mode: that tab.
func (a *app) tabKey(k tea.KeyPressMsg) (tabMsg, bool) {
	if len(a.tabs) < 2 {
		return tabMsg{}, false
	}
	s := k.String()
	switch {
	case len(s) == 1 && s[0] >= '1' && s[0] <= '9' && a.cur().idle():
		return tabMsg{index: int(s[0] - '0')}, true
	case len(s) == 5 && strings.HasPrefix(s, "alt+") && s[4] >= '1' && s[4] <= '9':
		return tabMsg{index: int(s[4] - '0')}, true
	}
	return tabMsg{}, false
}

// idle is NORMAL mode with only the panes on screen and no key sequence
// half typed: plain keys are commands.
func (m *model) idle() bool {
	return m.mode == normal && m.picker == nil && m.cmdline == nil && !m.help && m.settings == nil &&
		m.viewer == nil && !m.quitting && m.pending == ""
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
	to.setTheme(to.themeName, to.layoutName) // each account keeps its own
	// Different content everywhere: repaint the screen whole.
	return tea.Batch(tea.ClearScreen, a.wrap(next, to.imageCmds()))
}

// shareSettings gives to the settings of from that are one for every
// account: sizes, images, reading and the history cache. The theme and
// layout are each account's own.
func shareSettings(from, to *model) {
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

// accountTheme is the palette account m shows itself in.
func (m *model) accountTheme() Theme {
	if m.themes != nil {
		if t, err := m.themes.get(m.themeName, m.layoutName); err == nil {
			return t
		}
	}
	return theme
}

// onColor is black or white, whichever reads better on hex.
func onColor(hex string) string {
	r, g, b := rgb(hex)
	if 299*r+587*g+114*b > 140000 {
		return "#000000"
	}
	return "#ffffff"
}

// tab is one account's tab: its number and name in its own theme's colors,
// filled when active, tinted otherwise, with its unread count as a badge.
func (a *app) tab(i int) string {
	m := a.tabs[i]
	t := m.accountTheme()
	bar := a.barColor()
	label := fmt.Sprintf(" %d %s ", i+1, m.backend.Workspace())
	var style lipgloss.Style
	if i == a.active {
		style = lipgloss.NewStyle().Background(lipgloss.Color(t.Primary)).Foreground(lipgloss.Color(onColor(t.Primary))).Bold(true)
	} else {
		// Inactive: the account's sidebar color, so each tab is recognizable.
		tint := cmpOr(t.SidebarBg, mix(bar, t.Primary, 0.22))
		style = lipgloss.NewStyle().Background(lipgloss.Color(tint)).Foreground(lipgloss.Color(cmpOr(t.SidebarText, t.Text)))
	}
	out := style.Render(label)
	if n := unread(m); n > 0 {
		badge := cmpOr(t.Badge, t.Error)
		out += lipgloss.NewStyle().Background(lipgloss.Color(badge)).Foreground(lipgloss.Color(onColor(badge))).Bold(true).Render(" " + count(n) + " ")
	}
	return out
}

// barColor is the tab bar's background: the active theme's surface.
func (a *app) barColor() string { return cmpOr(theme.Surface, "#262626") }

// tabSpans are the columns each tab takes on the bar.
func (a *app) tabSpans() [][2]int {
	out := make([][2]int, len(a.tabs))
	x := 1
	for i := range a.tabs {
		w := ansi.StringWidth(a.tab(i))
		out[i] = [2]int{x, x + w}
		x += w + 1
	}
	return out
}

func (a *app) tabBar() string {
	parts := make([]string, len(a.tabs))
	for i := range a.tabs {
		parts[i] = a.tab(i)
	}
	return paint(" "+strings.Join(parts, " "), a.width, bgSeq(a.barColor()))
}

func (a *app) View() tea.View {
	v := a.cur().View()
	if a.bar() > 0 && a.width > 0 {
		v.Content = a.tabBar() + "\n" + v.Content
	}
	return v
}
