package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// The help panel floats over the screen and is built from the keymap, so it
// shows the keys as configured. It scrolls with the NORMAL keys when it is
// taller than the screen.

var keyGlyphs = map[string]string{"down": "↓", "up": "↑", "left": "←", "right": "→"}

// chip renders a key as a key cap: "g g" as two of them.
func chip(k string) string {
	var caps []string
	for _, part := range strings.Fields(k) {
		if g, ok := keyGlyphs[part]; ok {
			part = g
		}
		caps = append(caps, keyCap.Render(part))
	}
	return strings.Join(caps, " ")
}

func (m *model) overlayQuit(rows []string) {
	w := min(46, m.width-4)
	if w < 24 {
		return
	}
	body := []string{textStyle.Render("Leave sltty? Unsent drafts are lost.")}
	footer := chip("y") + " " + chip("enter") + dimStyle.Render(" quit     ") + chip("n") + " " + chip("esc") + dimStyle.Render(" stay")
	box := modal("Quit", body, footer, w)
	m.float(rows, box, (m.width-w)/2, max(0, (m.height-1-len(box))/2))
}

// overlayLoading is the box in the middle of the screen while the
// conversations load: until they come there is nothing else to show.
func (m *model) overlayLoading(rows []string) {
	if !m.loading {
		return
	}
	m.spin.Style = spinStyle // the theme's, as it is now
	body := []string{
		m.spin.View() + " " + textStyle.Render("Loading conversations…"),
		"  " + dimStyle.Render(m.backend.Workspace()),
	}
	w := 0
	for _, l := range body {
		w = max(w, lipgloss.Width(l))
	}
	w = min(w+4, m.width)
	box := modal("Loading", body, "", w)
	m.float(rows, box, max(0, (m.width-w)/2), max(0, (m.height-1-len(box))/2))
}
