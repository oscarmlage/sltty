package tui

// Drawing the picker and the other floating boxes.

import (
	"cmp"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/img"
)

// overlayPicker draws the picker as slk's finder: a box in the middle of
// the screen, title, filter line, the list with a scrollbar when it does
// not fit, and the keys. Its height does not change as the list filters.
func (m *model) overlayPicker(rows []string) {
	p := m.picker
	w := max(34, min(80, m.width/2))
	if p.kind == pickSearch {
		w = max(44, min(110, m.width*3/4))
	}
	w = min(w, m.width-2)
	n := min(pickerRows, max(1, m.height-12))
	if w < 26 {
		return
	}
	inner := w - 4
	chip := ""
	if p.kind == pickSearch && p.scope != "" {
		chip = scopeChip.Render("in:"+scopeLabel(m, p.scope)) + " "
	}
	p.input.SetWidth(max(4, inner-6-lipgloss.Width(chip)))
	in := chip + p.input.View()
	if !p.typing {
		in = dimStyle.Render(ansi.Strip(in))
	}
	title := p.title
	if len(p.items) > 0 {
		title += fmt.Sprintf(" · %d/%d", p.cursor+1, len(p.items))
	}
	body := []string{modalInput.Render("▌") + " " + in, ""}
	scrolls := len(p.items) > n
	cw := inner - 2 // the marker and its space
	if scrolls {
		cw--
	}
	ts, te := thumb(len(p.items), p.first, n)
	m.layout.pickTop = -1
	for r := range n {
		i := p.first + r
		var line string
		switch {
		case i < len(p.items):
			it := p.items[i]
			label, right, icon := it.label, "", ""
			if it.right != "" {
				right = " " + dimStyle.Render(it.right)
			}
			if p.kind == pickReact && strings.HasPrefix(it.emoji, ":") {
				// A workspace emoji's picture, kept out of the selection's
				// restyling: its colour is its id.
				icon = cmp.Or(m.emojiText(it.emoji), "  ") + "  "
			}
			label = ansi.Truncate(label, max(1, cw-lipgloss.Width(icon)-lipgloss.Width(right)), "…")
			if i == p.cursor {
				label = modalSel.Render(ansi.Strip(label))
				line = modalMark.Render("▌") + " "
			} else {
				line = "  "
			}
			line += icon + label + strings.Repeat(" ", max(0, cw-lipgloss.Width(icon)-lipgloss.Width(label)-lipgloss.Width(right))) + right
		case r == 0 && p.waiting:
			line = modalEmpty.Render("  searching…")
		case r == 0 && p.input.Value() != "":
			line = modalEmpty.Render("  nothing found")
		case r == 0 && p.kind == pickSearch && p.scope != "":
			line = modalEmpty.Render("  type to search " + scopeLabel(m, p.scope))
		case r == 0 && p.kind == pickSearch:
			line = modalEmpty.Render("  type to search all conversations")
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
	var hs [][2]string
	if p.typing {
		hs = [][2]string{{m.km.first(actPickSelect), "pick"}, {m.km.first(actPickDown) + "/" + m.km.first(actPickUp), "move"}, {m.km.first(actNormalMode), "list"}}
		if p.kind == pickSearch && p.here != "" {
			where := "only " + scopeLabel(m, p.here)
			if p.scope != "" {
				where = "everywhere"
			}
			hs = append(hs, [2]string{m.km.first(actPickScope), where})
		}
	} else {
		hs = [][2]string{{m.km.first(actDown) + "/" + m.km.first(actUp), "move"}, {m.km.first(actOpen), "pick"}, {m.km.first(actInsert), "filter"}, {m.km.first(actBack), "close"}}
	}
	box := modal(title, body, hints(hs), w)
	x, y := (m.width-w)/2, max(0, (m.height-1-len(box))/2)
	m.layout.pickTop, m.layout.pickX, m.layout.pickW = y+2+3, x, w // border, padding, title, filter, gap
	m.layout.pickRows = n
	m.float(rows, box, x, y)
}

// thumb is the rows of a scrollbar thumb for total items, first on top, n
// on screen.
func thumb(total, first, n int) (int, int) {
	if total <= n {
		return 0, n
	}
	size := max(1, n*n/total)
	top := min(n-size, first*n/total)
	if first+n >= total {
		top = n - size
	}
	return top, top + size
}

// modal draws every dialog with the same three parts: a title and rule,
// its content, and a keyboard-help footer behind its own rule. Empty footers
// are for progress dialogs, which have no action to explain.
func modal(title string, body []string, footer string, w int) []string {
	inner := w - 4
	b := modalBorder.Render
	line := func(s string) string { return b("│") + " " + fit(s, inner) + " " + b("│") }
	blank := line("")
	rule := line(ruleStyle.Render(strings.Repeat("─", inner)))
	out := []string{
		b("╭" + strings.Repeat("─", w-2) + "╮"),
		line(modalTitle.Render(ansi.Truncate(title, inner, "…"))),
		rule,
		blank,
	}
	for _, l := range body {
		out = append(out, line(l))
	}
	if footer != "" {
		// The rules already separate the footer; avoid wasting two extra rows
		// below every modal just to pad keyboard hints.
		out = append(out, rule, line(footer))
		return append(out, b("╰"+strings.Repeat("─", w-2)+"╯"))
	}
	return append(out, blank, b("╰"+strings.Repeat("─", w-2)+"╯"))
}

// highlight marks the words of a search in text.
func highlight(text string, words []string) string {
	low := strings.ToLower(text)
	if len(low) != len(text) || len(words) == 0 {
		return textStyle.Render(text)
	}
	mark := make([]bool, len(text))
	for _, w := range words {
		w = strings.ToLower(w)
		for i := 0; w != "" && i < len(low); {
			j := strings.Index(low[i:], w)
			if j < 0 {
				break
			}
			for k := i + j; k < i+j+len(w); k++ {
				mark[k] = true
			}
			i += j + len(w)
		}
	}
	var b strings.Builder
	for i := 0; i < len(text); {
		j := i
		for j < len(text) && mark[j] == mark[i] {
			j++
		}
		if mark[i] {
			b.WriteString(matchStyle.Render(text[i:j]))
		} else {
			b.WriteString(textStyle.Render(text[i:j]))
		}
		i = j
	}
	return b.String()
}

// backdrop dims the screen behind a dialog, cell by cell, like slk.
func (m *model) backdrop(rows []string) {
	c := lipgloss.NewCanvas(m.width, len(rows))
	c.Compose(lipgloss.NewLayer(strings.Join(rows, "\n")))
	for y := range len(rows) {
		for x := range m.width {
			cell := c.CellAt(x, y)
			if cell == nil || cell.Width == 0 {
				continue
			}
			// change the cell in place: SetCell on a wide character drops it
			if img.IsPlaceholder(cell.Content) { // its color names the image: no dimming it
				cell.Content, cell.Width, cell.Style.Fg = " ", 1, nil
			}
			if cell.Style.Bg != nil {
				cell.Style.Bg = lipgloss.Darken(cell.Style.Bg, 0.5)
			}
			if cell.Style.Fg != nil {
				cell.Style.Fg = lipgloss.Darken(cell.Style.Fg, 0.5)
			} else {
				cell.Style.Fg = lipgloss.Color(theme.TextMuted)
			}
		}
	}
	copy(rows, strings.Split(c.Render(), "\n"))
}

// float draws lines over the screen rows at column x, row y.
func (m *model) float(rows, lines []string, x, y int) {
	w := 0
	for _, l := range lines {
		w = max(w, lipgloss.Width(l))
	}
	for i, l := range lines {
		l = paint(l, w, surfaceBg)
		r := y + i
		if r >= len(rows)-1 {
			break
		}
		// A wide character cut in half at either edge leaves a gap: pad it.
		left := fit(ansi.Cut(rows[r], 0, x), x)
		right := ansi.Cut(rows[r], x+w, m.width)
		right = strings.Repeat(" ", max(0, m.width-x-w-lipgloss.Width(right))) + right
		rows[r] = left + l + right
	}
}
