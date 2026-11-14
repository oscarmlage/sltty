package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := quick(cmd).(tea.QuitMsg)
	return ok
}

func TestCtrlCStepsBackThenAsks(t *testing.T) {
	m := newTest(t)
	press(m, "l", "enter") // thread open, focus in it
	press(m, "ctrl+c")
	if m.thread != nil || m.focus != paneChat || m.quitting {
		t.Fatalf("1st ctrl+c: thread %v focus %v quitting %v", m.thread != nil, m.focus, m.quitting)
	}
	press(m, "ctrl+c")
	if m.focus != paneSide || m.quitting {
		t.Fatalf("2nd ctrl+c: focus %v quitting %v", m.focus, m.quitting)
	}
	press(m, "ctrl+c")
	if !m.quitting || !contains(screen(m), "Leave sltty?") {
		t.Fatal("3rd ctrl+c: no confirmation")
	}
	press(m, "n")
	if m.quitting {
		t.Fatal("n did not dismiss the confirmation")
	}
	press(m, "q")
	if !m.quitting {
		t.Fatal("q did not ask")
	}
	if _, cmd := m.Update(named["ctrl+c"]); !isQuit(cmd) {
		t.Fatal("ctrl+c on the confirmation did not quit")
	}
}

func TestCtrlCClosesPickerFirst(t *testing.T) {
	m := newTest(t)
	press(m, "ctrl+k", "ctrl+c") // filter → list
	if m.picker == nil || m.picker.typing {
		t.Fatal("ctrl+c in the filter did not go to the list")
	}
	press(m, "ctrl+c")
	if m.picker != nil || m.quitting {
		t.Fatal("ctrl+c in the list did not just close it")
	}
}

func TestShiftEnterNewLine(t *testing.T) {
	m := newTest(t)
	press(m, "i")
	typeText(m, "uno")
	run(m, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	typeText(m, "dos")
	if m.chatIn.Value() != "uno\ndos" {
		t.Fatalf("value %q", m.chatIn.Value())
	}
}

func clickMsg(m *model, i int) {
	for y, mi := range m.layout.chatMsg {
		if mi == i {
			run(m, tea.MouseClickMsg{X: m.mainX() + 3, Y: y + m.layout.top, Button: tea.MouseLeft})
			run(m, tea.MouseReleaseMsg{X: m.mainX() + 3, Y: y + m.layout.top, Button: tea.MouseLeft})
			return
		}
	}
}

func TestDoubleClickOpensThread(t *testing.T) {
	m := newTest(t)
	clock := time.Now()
	m.now = func() time.Time { return clock }
	m.render()
	i := m.selected() - 1
	clickMsg(m, i)
	if m.thread != nil || m.selected() != i {
		t.Fatal("a single click opened the thread")
	}
	clock = clock.Add(time.Second) // too slow: two single clicks
	m.render()
	clickMsg(m, i)
	if m.thread != nil {
		t.Fatal("slow clicks opened the thread")
	}
	clock = clock.Add(200 * time.Millisecond)
	m.render()
	clickMsg(m, i)
	if m.thread == nil || m.thread.root != m.history["C1"][i].TS {
		t.Fatal("double click did not open the thread")
	}
}

func TestDragComposerTaller(t *testing.T) {
	m := newTest(t)
	y := m.layout.composerTop + m.layout.top
	run(m, tea.MouseClickMsg{X: m.mainX() + 5, Y: y, Button: tea.MouseLeft})
	if m.drag != dragComposer {
		t.Fatal("press on the composer's top edge did not start a drag")
	}
	run(m, tea.MouseMotionMsg{X: m.mainX() + 5, Y: y - 4, Button: tea.MouseLeft})
	run(m, tea.MouseReleaseMsg{X: m.mainX() + 5, Y: y - 4, Button: tea.MouseLeft})
	if m.inputRows != defaultInput+4 {
		t.Fatalf("input rows %d, want %d", m.inputRows, defaultInput+4)
	}
	press(m, "-", "-")
	if m.inputRows != defaultInput+2 {
		t.Fatalf("after - -: %d", m.inputRows)
	}
	for range 50 {
		press(m, "+")
	}
	if m.inputRows != m.contentRows()/2 {
		t.Fatalf("not capped: %d", m.inputRows)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
