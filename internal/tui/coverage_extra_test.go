package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Dragging across message text copies it and shows the selection while it
// is in progress.
func TestDragSelectionCopiesAndPaints(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "tools-team")
	m.render()
	rows := []int{}
	for y, i := range m.layout.chatMsg {
		if i >= 0 && strings.TrimSpace(strings.TrimRight(m.layout.chatText[y], " ")) != "" {
			rows = append(rows, y)
		}
	}
	if len(rows) < 2 {
		t.Fatal("no message rows to select")
	}
	y0, y1 := rows[0]+m.layout.top, rows[len(rows)-1]+m.layout.top
	x := m.mainX() + 2
	run(m, tea.MouseClickMsg{X: x, Y: y0, Button: tea.MouseLeft})
	run(m, tea.MouseMotionMsg{X: x + 12, Y: y1, Button: tea.MouseLeft})
	if m.drag != dragText {
		t.Fatal("no drag in progress")
	}
	if out := m.render(); !strings.Contains(out, dragSelBg) {
		t.Fatal("the selection is not painted while dragging")
	}
	_, cmd := m.Update(tea.MouseReleaseMsg{X: x + 12, Y: y1, Button: tea.MouseLeft})
	if m.drag != 0 || m.status != "copied selection" || cmd == nil {
		t.Fatalf("release: drag %v status %q cmd %v", m.drag, m.status, cmd != nil)
	}
	// and in reverse, from the bottom up
	run(m, tea.MouseClickMsg{X: x + 12, Y: y1, Button: tea.MouseLeft})
	run(m, tea.MouseMotionMsg{X: x, Y: y0, Button: tea.MouseLeft})
	if txt := m.draggedText(m.selectDrag); txt == "" {
		t.Fatal("an upward selection copied nothing")
	}
	run(m, tea.MouseReleaseMsg{X: x, Y: y0, Button: tea.MouseLeft})
}

func TestSnoozeAndLogSheets(t *testing.T) {
	m := newTest(t)
	run(m, m.setSnooze(20)())
	if m.snoozeTill.IsZero() {
		t.Fatal("snooze not noted")
	}
	run(m, m.setSnooze(0)())
	m.openLog()
	if !m.help || m.sheet == nil || m.sheet.title != "Logs" {
		t.Fatal("log sheet not open")
	}
	if !strings.Contains(screen(m), "Logs") {
		t.Fatal("log sheet not drawn")
	}
	m.closeSheet()
	if m.help {
		t.Fatal("sheet still open")
	}
}

func TestKeysFromSaysWhereTheyCome(t *testing.T) {
	m := newTest(t)
	m.opts.ConfigFound, m.opts.ConfigPath = true, "/x/config.json"
	if got := m.keysFrom(); !strings.Contains(got, "/x/config.json") {
		t.Fatalf("keysFrom = %q", got)
	}
	m.opts.ConfigFound = false
	if got := m.keysFrom(); !strings.Contains(got, "sltty keys") {
		t.Fatalf("keysFrom = %q", got)
	}
}
