package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/config"
	"github.com/oscarmlage/sltty/internal/diag"
	"github.com/oscarmlage/sltty/internal/emoji"
)

// A sheet is slk's keybindings box: a title, a two-column list with a cursor
// and a scrollbar, "/" to filter it. The keys (?) and the config reference
// are sheets.

type sheetTab struct {
	name string
	rows [][2]string
}

type sheet struct {
	title, sub string
	rows       [][2]string // left (keys, a config key), right (what it does)
	tabs       []sheetTab
	tab        int
	leftW      int
	cursor     int
	first      int
	filter     textinput.Model
	searching  bool
}

func newSheet(title, sub string, rows [][2]string) *sheet {
	in := textinput.New()
	in.Prompt = "/"
	w := 0
	for _, r := range rows {
		w = max(w, lipgloss.Width(r[0]))
	}
	return &sheet{title: title, sub: sub, rows: rows, leftW: min(w, 26), filter: in}
}

// visible are the active tab's rows matching the filter. A search spans all
// tabs, so a binding can always be found without first guessing its category.
func (s *sheet) visible() [][2]string {
	q := strings.ToLower(strings.TrimSpace(s.filter.Value()))
	rows := s.rows
	if q == "" && len(s.tabs) > 0 {
		rows = s.tabs[s.tab].rows
	}
	if q == "" {
		return rows
	}
	var out [][2]string
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r[0]+" "+r[1]), q) {
			out = append(out, r)
		}
	}
	return out
}

// openKeys is ?: every binding, from the keymap in use, and the commands,
// sorted by what they do.
func (m *model) openKeys() {
	var rows [][2]string
	var tabs []sheetTab
	for _, b := range defaults {
		keys := m.km.keys[b.act]
		if len(keys) == 0 {
			continue
		}
		group := b.group
		if group == "Lists (ctrl+k, ctrl+f, r, T)" {
			group = "Lists"
		}
		if len(tabs) == 0 || tabs[len(tabs)-1].name != group {
			tabs = append(tabs, sheetTab{name: group})
		}
		r := [2]string{strings.Join(keys[:min(2, len(keys))], " / "), b.help}
		tabs[len(tabs)-1].rows = append(tabs[len(tabs)-1].rows, r)
		rows = append(rows, r)
	}
	commandsTab := sheetTab{name: "Commands"}
	for _, c := range commands {
		r := [2]string{":" + c.name, c.help}
		commandsTab.rows = append(commandsTab.rows, r)
		rows = append(rows, r)
	}
	// Aliases are command-line bindings too. Keep familiar vim forms visible.
	for _, a := range [][2]string{{":q", "alias for :quit"}, {":w", "alias for :save"}, {":wq", "alias for :savequit"}, {":h", "alias for :help"}, {":f", "alias for :find"}, {":s", "alias for :search"}} {
		commandsTab.rows = append(commandsTab.rows, a)
		rows = append(rows, a)
	}
	// Keep a real, copyable JSON example in its own horizontal tab. The
	// bindings below replace only those actions' defaults.
	jsonTab := sheetTab{name: "config.json"}
	for _, line := range []string{
		"{",
		`  "keys": {`,
		`    "react": ["r", "*"],`,
		`    "normal_mode": ["esc", "ctrl+c", "j j"]`,
		"  }",
		"}",
	} {
		r := [2]string{"", keyStyle.Render(line)}
		jsonTab.rows = append(jsonTab.rows, r)
		rows = append(rows, r)
	}
	tabs = append(tabs, commandsTab, jsonTab)
	m.sheet = newSheet("Keybindings", "Action ID → accepted keys · each array replaces that action's defaults", rows)
	m.sheet.tabs = tabs
	m.help = true
}

// openReactions shows the people behind each reaction on the selected message.
// Each emoji is a tab so a busy message remains short and keyboard-friendly.
func (m *model) openReactions(msg *chat.Message) {
	if msg == nil || len(msg.Reactions) == 0 {
		m.status = "no reactions on this message"
		return
	}
	var rows [][2]string
	tabs := make([]sheetTab, 0, len(msg.Reactions))
	for _, r := range msg.Reactions {
		code := r.Emoji
		if n, ok := emoji.Name(r.Emoji); ok {
			code = ":" + n + ":"
		}
		name := m.reactionEmoji(r.Emoji) + " " + code + " " + fmt.Sprint(r.Count)
		users := r.Users
		if len(users) == 0 {
			users = []string{"Slack did not return the people for this reaction"}
		}
		tr := make([][2]string, 0, len(users))
		for _, user := range users {
			row := [2]string{"", user}
			tr = append(tr, row)
			rows = append(rows, row)
		}
		tabs = append(tabs, sheetTab{name: name, rows: tr})
	}
	m.sheet = newSheet("Reactions", "", rows)
	m.sheet.tabs = tabs
	m.help = true
}

func (m *model) keysFrom() string {
	if m.opts.ConfigFound {
		return "keys from " + m.opts.ConfigPath
	}
	return "`sltty keys` prints a config with them all"
}

// openConfigRef lists what the config file takes.
func (m *model) openConfigRef() {
	var rows [][2]string
	for _, f := range config.Reference {
		what := f.Help + dimStyle.Render("  ("+f.Type)
		if f.Default != "" {
			what += dimStyle.Render(", default " + f.Default)
		}
		rows = append(rows, [2]string{f.Key, what + dimStyle.Render(")")})
	}
	where := m.opts.ConfigPath
	if !m.opts.ConfigFound {
		where += " (not there yet)"
	}
	m.sheet = newSheet("Config file", where, rows)
	m.help = true
}

// openLog displays the application and connection log without leaving the TUI.
func (m *model) openLog() {
	lines, err := diag.Tail(200)
	var rows [][2]string
	if err != nil {
		rows = append(rows, [2]string{"log", err.Error()})
	} else {
		for _, line := range lines {
			rows = append(rows, [2]string{"", line})
		}
	}
	where := m.opts.LogPath
	if where == "" {
		where = "log not initialized"
	}
	m.sheet = newSheet("Logs", where, rows)
	m.help = true
}

func (m *model) closeSheet() { m.help, m.sheet = false, nil }

// sheetTypingKey is a key while the filter of a sheet has the focus.
func (m *model) sheetTypingKey(msg tea.KeyPressMsg, k string) tea.Cmd {
	s := m.sheet
	undo := func() { s.filter, _ = s.filter.Update(backspace()) }
	switch {
	case m.typedSequence(scInsert, k, actNormalMode, undo) || m.km.is(scInsert, k, actNormalMode) || k == "enter":
		s.searching, m.lastInsert = false, ""
		s.filter.Blur()
		return nil
	case k == "backspace" && s.filter.Value() == "":
		s.searching = false
		return nil
	}
	var cmd tea.Cmd
	s.filter, cmd = s.filter.Update(msg)
	s.cursor, s.first = 0, 0
	return cmd
}

func (m *model) sheetAction(a action) tea.Cmd {
	s := m.sheet
	if s == nil {
		m.closeSheet()
		return nil
	}
	n := len(s.visible())
	switch a {
	case actFocusLeft:
		if len(s.tabs) > 0 && s.filter.Value() == "" {
			s.tab = (s.tab - 1 + len(s.tabs)) % len(s.tabs)
			s.cursor, s.first = 0, 0
		}
	case actFocusRight:
		if len(s.tabs) > 0 && s.filter.Value() == "" {
			s.tab = (s.tab + 1) % len(s.tabs)
			s.cursor, s.first = 0, 0
		}
	case actDown:
		s.cursor++
	case actUp:
		s.cursor--
	case actPageDown:
		s.cursor += pickerRows
	case actPageUp:
		s.cursor -= pickerRows
	case actTop:
		s.cursor = 0
	case actBottom:
		s.cursor = n - 1
	case actFilter, actSearch:
		s.searching = true
		return s.filter.Focus()
	case actBack, actBackOrQuit, actQuit, actHelp:
		if s.filter.Value() != "" && a == actBack {
			s.filter.SetValue("") // esc first clears the filter
			return nil
		}
		m.closeSheet()
	}
	s.cursor = max(0, min(s.cursor, n-1))
	return nil
}

// overlaySheet draws the open sheet in the middle of the screen.
func (m *model) overlaySheet(rows []string) {
	s := m.sheet
	w := min(90, m.width-4)
	if w < 30 || s == nil {
		return
	}
	inner := w - 4
	n := max(3, m.height-1-4-7) // borders, padding, title, sub, gaps, foot, hints
	// Help and the config reference are short lists, not full-screen views.
	// Logs retain the available height so their scrolling remains useful.
	if len(s.tabs) > 0 {
		n = min(10, n)
	} else if s.title == "Config file" {
		n = min(14, n)
	}
	vis := s.visible()
	if s.cursor < s.first {
		s.first = s.cursor
	}
	if s.cursor >= s.first+n {
		s.first = s.cursor - n + 1
	}
	s.first = max(0, min(s.first, len(vis)-n))
	sub := dimStyle.Render(s.sub)
	switch {
	case s.searching:
		sub = s.filter.View()
	case s.filter.Value() != "":
		sub = keyStyle.Render("/"+s.filter.Value()) + dimStyle.Render(fmt.Sprintf("  %d found · esc clears", len(vis)))
	}
	body := []string{}
	if len(s.tabs) > 0 {
		var labels []string
		for i, tab := range s.tabs {
			label := " " + tab.name + " "
			if i == s.tab && s.filter.Value() == "" {
				label = modalSel.Render(label)
			} else {
				label = dimStyle.Render(label)
			}
			labels = append(labels, label)
		}
		body = append(body, strings.Join(labels, ruleStyle.Render("│")))
	}
	if sub != "" {
		body = append(body, sub)
	}
	body = append(body, "")
	scrolls := len(vis) > n
	ts, te := thumb(len(vis), s.first, n)
	for r := range n {
		i := s.first + r
		var line string
		switch {
		case i < len(vis):
			// A row with no left column is a full-width note/code row. Ruled
			// labels are group headings; this also lets config.json be shown as
			// readable JSON instead of squeezing it into the key column.
			if vis[i][0] == "" {
				if strings.HasPrefix(vis[i][1], "── ") {
					line = "  " + dimStyle.Render(vis[i][1])
				} else {
					line = "  " + vis[i][1]
				}
				break
			}
			left := fit(keyStyle.Render(ansi.Truncate(vis[i][0], s.leftW, "…")), s.leftW)
			right := textStyle.Render(vis[i][1])
			mark := "  "
			if i == s.cursor {
				mark = modalMark.Render("▌") + " "
				right = modalSel.Render(ansi.Strip(vis[i][1]))
			}
			line = mark + left + "  " + right
		case r == 0:
			line = modalEmpty.Render("  nothing matches")
		}
		if scrolls {
			line = fit(line, inner-1)
			if r >= ts && r < te {
				line += modalMark.Render("█")
			} else {
				line += ruleStyle.Render("│")
			}
		}
		body = append(body, line)
	}
	k := m.km.first
	hintsRows := [][2]string{{k(actFilter), "search"}, {k(actDown) + "/" + k(actUp), "move"}, {k(actBack) + "/" + k(actBackOrQuit), "close"}}
	if len(s.tabs) > 0 {
		hintsRows = append([][2]string{{k(actFocusLeft) + "/" + k(actFocusRight), "tabs"}}, hintsRows...)
	}
	box := modal(s.title, body, hints(hintsRows), w)
	m.float(rows, box, (m.width-w)/2, max(0, (m.height-1-len(box))/2))
}
