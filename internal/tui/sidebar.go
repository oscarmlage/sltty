package tui

import (
	"cmp"
	"fmt"
	"path"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/chat"
)

// The sidebar: Threads and Activity, then the custom sections of the config
// in their order, then Channels and Direct messages with whatever no
// section took.

// Section is a custom sidebar group. Channels are name patterns (path.Match
// globs: "tools-*"); "@name" patterns match DMs.
type Section struct {
	Name     string
	Channels []string
	// Collapsed folds it at start. A section named Channels or Direct
	// messages with no patterns only sets this for the built-in one.
	Collapsed bool
}

func validateSections(ss []Section) error {
	seen := map[string]bool{}
	for _, s := range ss {
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("sections: a section needs a name")
		}
		if seen[s.Name] {
			return fmt.Errorf("sections: %q twice", s.Name)
		}
		seen[s.Name] = true
		for _, p := range s.Channels {
			if _, err := path.Match(strings.TrimPrefix(p, "@"), ""); err != nil {
				return fmt.Errorf("sections: %s: bad pattern %q", s.Name, p)
			}
		}
	}
	return nil
}

type sideKind int

const (
	sideConv sideKind = iota
	sideThreads
	sideActivity
	sideSection // a section header: the sidebar cursor stops there, no view
)

// Sidebar rows that are no conversation, in layout.side.
const (
	rowNone     = -1
	rowThreads  = -2
	rowActivity = -3
	rowSection  = -10 // header of section i: rowSection - i
)

type sideItem struct {
	kind sideKind
	conv int
	sec  string // sideSection
}

type section struct {
	name  string
	emoji string
	convs []int
}

// sections groups the conversations: each into the first custom section
// with a matching pattern, the rest into Channels and Direct messages.
func (m *model) sections() []section {
	// A live Slack backend preserves native section membership and order.
	// Explicit config sections remain an intentional override.
	if len(m.opts.Sections) == 0 {
		var out []section
		byName := map[string]int{}
		for i, c := range m.convs {
			name := c.Section
			if name == "" {
				if c.Kind == chat.DM {
					name = builtinDMs
				} else {
					name = builtinChannels
				}
			}
			at, ok := byName[name]
			if !ok {
				at = len(out)
				byName[name] = at
				out = append(out, section{name: name, emoji: c.SectionEmoji})
			}
			out[at].convs = append(out[at].convs, i)
		}
		return m.unreadSections(m.withVIPs(out))
	}
	custom := m.customSections()
	out := make([]section, 0, len(custom)+2)
	for _, s := range custom {
		out = append(out, section{name: s.Name})
	}
	channels, dms := section{name: builtinChannels}, section{name: builtinDMs}
	for i, c := range m.convs {
		placed := false
		for si, s := range custom {
			if matchesAny(s.Channels, c) {
				out[si].convs = append(out[si].convs, i)
				placed = true
				break
			}
		}
		switch {
		case placed:
		case c.Kind == chat.DM:
			dms.convs = append(dms.convs, i)
		default:
			channels.convs = append(channels.convs, i)
		}
	}
	// In a custom section, the order of its patterns, then the backend's.
	for si, s := range custom {
		slices.SortStableFunc(out[si].convs, func(a, b int) int {
			return cmp.Compare(matchIndex(s.Channels, m.convs[a]), matchIndex(s.Channels, m.convs[b]))
		})
	}
	return m.unreadSections(m.withVIPs(append(out, channels, dms)))
}

// vipSection holds, above everything, the DMs with the user's VIPs that
// have unread messages (Slack's VIPs).
const vipSection = "VIP"

// vipNow is whether conversation i sits in the VIP section: a VIP's DM
// with unread messages, or the one the user went into from there, until
// they leave it (reading it must not move it from under the cursor).
func (m *model) vipNow(i int) bool {
	c := m.convs[i]
	return c.VIP && (c.Unread > 0 && !c.Muted || c.ID == m.vipKeep)
}

// withVIPs moves the VIP conversations with unread messages to a section
// of their own, first.
func (m *model) withVIPs(in []section) []section {
	vip := section{name: vipSection, emoji: "star"}
	for si := range in {
		in[si].convs = slices.DeleteFunc(in[si].convs, func(i int) bool {
			if m.vipNow(i) {
				vip.convs = append(vip.convs, i)
				return true
			}
			return false
		})
	}
	if len(vip.convs) == 0 {
		return in
	}
	slices.Sort(vip.convs) // the backend's order, whatever section each came from
	return append([]section{vip}, in...)
}

// unreadSections removes read conversations from the sidebar view only.
// m.convs stays complete, so the switcher and search always see every chat.
// The open conversation stays too: reading it (u, or entering it) must not
// pull the sidebar cursor out from under the user.
func (m *model) unreadSections(in []section) []section {
	if !m.unreadOnly {
		return in
	}
	open := m.openConv()
	out := make([]section, 0, len(in))
	for _, s := range in {
		// The chat with yourself (Saved Messages) is always there too.
		s.convs = slices.DeleteFunc(s.convs, func(i int) bool { return m.convs[i].Unread == 0 && i != open && !m.convs[i].Self })
		if len(s.convs) > 0 {
			out = append(out, s)
		}
	}
	return out
}

const (
	builtinChannels = "Channels"
	builtinDMs      = "Direct messages"
)

// customSections are the config's sections, less the entries that only
// fold a built-in one.
func (m *model) customSections() []Section {
	var out []Section
	for _, s := range m.opts.Sections {
		if !builtin(s) {
			out = append(out, s)
		}
	}
	return out
}

func builtin(s Section) bool {
	return len(s.Channels) == 0 && (s.Name == builtinChannels || s.Name == builtinDMs)
}

func (m *model) folded(name string) bool {
	if v, ok := m.collapsed[name]; ok {
		return v
	}
	// The backend folds some (Telegram's Archived) until the user opens them.
	if slices.ContainsFunc(m.convs, func(c chat.Conversation) bool { return c.Section == name && c.Folded }) {
		return true
	}
	for _, s := range m.opts.Sections {
		if s.Name == name {
			return s.Collapsed
		}
	}
	return false
}

// toggleSection folds or unfolds a section; "" is the one the sidebar
// cursor is on or in. Folding from inside a section leaves the cursor on
// its header: the conversation under it would be the only one left.
func (m *model) toggleSection(name string) {
	if name == "" {
		cur := m.sideCurrent()
		switch cur.kind {
		case sideSection:
			name = cur.sec
		case sideConv:
			for _, s := range m.sections() {
				if slices.Contains(s.convs, cur.conv) {
					name = s.name
				}
			}
		}
	}
	if name == "" {
		return
	}
	if m.collapsed == nil { // a saved state with nothing folded
		m.collapsed = map[string]bool{}
	}
	m.collapsed[name] = !m.folded(name)
	if m.focus == paneSide && m.collapsed[name] {
		m.onSection = name
	}
}

// openConv is the conversation the chat pane shows, or -1.
func (m *model) openConv() int {
	if m.view != sideConv || len(m.convs) == 0 {
		return -1
	}
	return m.cursor
}

// visible is what a section shows: all, or folded, only the open
// conversation, as slk does.
func (m *model) visible(s section) []int {
	if !m.folded(s.name) {
		return s.convs
	}
	if open := m.openConv(); slices.Contains(s.convs, open) {
		return []int{open}
	}
	return nil
}

// sectionUnread is what a folded header counts: the unread messages of
// its conversations.
func (m *model) sectionUnread(s section) int {
	n := 0
	for _, i := range s.convs {
		if !m.convs[i].Muted {
			n += m.convs[i].Unread
		}
	}
	return n
}

func matchesAny(patterns []string, c chat.Conversation) bool { return matchIndex(patterns, c) >= 0 }

// matchIndex is the first pattern matching c, or -1.
func matchIndex(patterns []string, c chat.Conversation) int {
	for i, p := range patterns {
		dm := strings.HasPrefix(p, "@")
		if dm != (c.Kind == chat.DM) {
			continue
		}
		if ok, _ := path.Match(strings.TrimPrefix(p, "@"), c.Name); ok {
			return i
		}
	}
	return -1
}

// sideOrder is every sidebar entry j/k walks, top to bottom: section
// headers too, in the sidebar itself, so a folded section can be unfolded.
func (m *model) sideOrder() []sideItem {
	out := []sideItem{{kind: sideThreads}, {kind: sideActivity}}
	if m.noThreads {
		out = out[1:]
	}
	for _, s := range m.sections() {
		if len(s.convs) == 0 {
			continue
		}
		if m.focus == paneSide {
			out = append(out, sideItem{kind: sideSection, sec: s.name})
		}
		for _, i := range m.visible(s) {
			out = append(out, sideItem{kind: sideConv, conv: i})
		}
	}
	return out
}

func (m *model) sideCurrent() sideItem {
	switch {
	case m.onSection != "":
		return sideItem{kind: sideSection, sec: m.onSection}
	case m.view != sideConv:
		return sideItem{kind: m.view}
	}
	return sideItem{kind: sideConv, conv: m.cursor}
}

// sideMove moves n entries down the sidebar and opens what it lands on.
func (m *model) sideMove(n int) tea.Cmd {
	order := m.sideOrder()
	cur, pos := m.sideCurrent(), 0
	if cur.kind == sideSection && m.focus != paneSide {
		cur = sideItem{kind: m.view, conv: m.cursor} // J/K from a pane: what it shows
		if m.view != sideConv {
			cur.conv = 0
		}
	}
	for i, it := range order {
		if it == cur {
			pos = i
		}
	}
	return m.openSide(order[max(0, min(pos+n, len(order)-1))])
}

func (m *model) sideTo(first bool) tea.Cmd {
	order := m.sideOrder()
	if first {
		return m.openSide(order[0])
	}
	return m.openSide(order[len(order)-1])
}

func (m *model) openSide(it sideItem) tea.Cmd {
	m.onSection = ""
	switch it.kind {
	case sideThreads:
		return m.openThreadsView()
	case sideActivity:
		return m.openActivityView()
	case sideSection:
		m.onSection = it.sec // the chat pane keeps what it shows
		return nil
	}
	return m.selectConv(it.conv)
}

// sidebar is the workspace name, Threads and Activity, and the sections,
// scrolled to keep the selection on screen.
func (m *model) sidebar(w, rows int) []string {
	type entry struct {
		text string
		row  int
	}
	cur := m.sideCurrent()
	all := []entry{{sbTitle.Render(" " + m.backend.Workspace()), rowNone}, {"", rowNone}}
	cursorLine := 0
	add := func(text string, row int, selected bool) {
		if selected {
			cursorLine = len(all)
		}
		all = append(all, entry{text, row})
	}
	if !m.noThreads {
		add(m.sideLine("💬", "Threads", m.threadsUnread(), cur.kind == sideThreads, w), rowThreads, cur.kind == sideThreads)
	}
	add(m.sideLine("🔔", "Activity", m.activityUnread(), cur.kind == sideActivity, w), rowActivity, cur.kind == sideActivity)
	for si, s := range m.sections() {
		if len(s.convs) == 0 {
			continue
		}
		all = append(all, entry{"", rowNone})
		sel := cur.kind == sideSection && cur.sec == s.name
		add(m.sectionHeader(s, sel, w), rowSection-si, sel)
		for _, i := range m.visible(s) {
			sel := cur.kind == sideConv && cur.conv == i
			add(m.sideEntry(m.convs[i], sel, w), i, sel)
		}
	}
	if len(m.convs) == 0 && !m.loading { // while loading, the loading modal says it
		all = append(all, entry{sbMuted.Render("  no conversations"), rowNone})
	}
	first := max(0, cursorLine-rows+1)
	out := make([]string, rows)
	m.layout.side = make([]int, rows)
	for r := range rows {
		m.layout.side[r] = rowNone
		if e := first + r; e < len(all) {
			out[r] = paint(all[e].text, w, sbBg)
			m.layout.side[r] = all[e].row
			continue
		}
		out[r] = paint("", w, sbBg)
	}
	return out
}

// sectionHeader is a section's row: its fold arrow, emoji and name and,
// folded, the unread it hides (slk's "▸ Channels •3").
func (m *model) sectionHeader(s section, selected bool, w int) string {
	arrow, unread := "▾ ", 0
	if m.folded(s.name) {
		arrow, unread = "▸ ", m.sectionUnread(s)
	}
	pic := m.emojiText(s.emoji) // a custom one is a picture: never restyled
	if pic != "" {
		pic += " "
	}
	slk := theme.SidebarSelBg == ""
	badge := ""
	switch {
	case unread > 0 && slk:
		badge = " " + sbDot.Render(fmt.Sprintf("•%d", unread))
	case unread > 0:
		badge = sbBadge.Render(count(unread))
	}
	leftW := max(1, w-2-lipgloss.Width(badge))
	name := ansi.Truncate(s.name, max(1, leftW-3-lipgloss.Width(pic)), "…")
	style, mark := sbMuted, " "
	if selected || unread > 0 {
		style = sbBright
	}
	focused := selected && m.focus == paneSide
	switch {
	case selected && slk && focused:
		mark = sbMark.Render("▌")
	case selected && slk:
		mark = sbMuted.Render("▌")
	case focused:
		style = sbSelText
	}
	line := mark + style.Render(arrow) + pic + style.Render(name)
	switch {
	case slk:
		return line + badge
	case focused:
		return paint(fit(line, leftW)+sbSelText.Render(ansi.Strip(badge)), w, sbSelFocusBg)
	case selected:
		return paint(fit(line, leftW)+badge, w, sbSelBg)
	}
	return fit(line, leftW) + badge
}

// privateIcon marks private channels; Options.PrivateIcon replaces it (a
// narrow glyph like "◆" where the terminal draws emoji at odd widths).
var privateIcon = "🔒"

func convIcon(c chat.Conversation) string {
	switch {
	case c.Group > 0 && c.Group < 10: // a group DM: how many, as Slack shows it
		return sbMuted.Render(fmt.Sprint(c.Group)) + " "
	case c.Group > 0:
		return sbMuted.Render("+") + " "
	case c.Kind == chat.DM && c.Online:
		return sbOnline.Render("●") + " "
	case c.Kind == chat.DM:
		return "○ "
	case c.Private:
		if lipgloss.Width(privateIcon) == 1 {
			return privateIcon + " "
		}
		return privateIcon // a wide one takes the two columns of "# "
	}
	return "# "
}

func (m *model) sideEntry(c chat.Conversation, selected bool, w int) string {
	icon := convIcon(c)
	if len(m.typing[c.ID]) > 0 {
		icon = sbOnline.Render("✎") + " "
	}
	return m.sideLineM(icon, c.Name, c.Unread, c.Muted, selected, w)
}

// count is an unread count as the badge shows it: 3303 is 3.3K.
func count(n int) string {
	switch {
	case n >= 10000:
		return fmt.Sprintf("%dK", n/1000)
	case n >= 1000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "K"
	}
	return fmt.Sprint(n)
}

// sideLine is one sidebar entry: icon, name and unread count, styled for
// the theme (a filled row, or slk's cursor bar and dot).
func (m *model) sideLine(icon, name string, unread int, selected bool, w int) string {
	return m.sideLineM(icon, name, unread, false, selected, w)
}

// sideLineM is sideLine with a muted entry's quiet badge.
func (m *model) sideLineM(icon, name string, unread int, muted, selected bool, w int) string {
	if lipgloss.Width(icon) == 1 {
		icon += " "
	}
	badge := ""
	if unread > 0 {
		badge = sbBadge.Render(count(unread))
		if muted {
			badge = sbBadgeMuted.Render(count(unread))
		}
	}
	// Reserve a fixed right-hand column for the badge. Padding the complete
	// left side (rather than adding a gap after each variable-width name) keeps
	// every unread badge on the same terminal column.
	leftW := max(1, w-2-lipgloss.Width(badge))
	name = ansi.Truncate(name, max(1, leftW-2-lipgloss.Width(icon)), "…")
	if theme.SidebarSelBg == "" {
		mark := "  "
		if selected {
			mark = sbMark.Render("▌") + " "
			if m.focus != paneSide {
				mark = sbMuted.Render("▌") + " "
			}
		}
		switch {
		case unread > 0:
			return mark + icon + sbBright.Render(name) + " " + sbDot.Render("•")
		case selected:
			return mark + icon + sbBright.Render(name)
		}
		return mark + sbMuted.Render(ansi.Strip(icon)+name)
	}
	left := func(icon, label string) string { return fit("  "+icon+label, leftW) }
	switch {
	case selected && m.focus == paneSide:
		// A selected read channel has no badge. Rendering one unconditionally
		// leaves a stray, clipped glyph at the right edge on narrow sidebars.
		selectedBadge := ""
		if unread > 0 {
			selectedBadge = sbSelBadge.Render(count(unread))
		}
		return paint(sbSelText.Render(left(ansi.Strip(icon), name))+selectedBadge, w, sbSelFocusBg)
	case selected:
		return paint(left(icon, sbBright.Render(name))+badge, w, sbSelBg)
	case unread > 0:
		return left(icon, sbBright.Render(name)) + badge
	}
	return "  " + sbMuted.Render(ansi.Strip(icon)+name)
}
