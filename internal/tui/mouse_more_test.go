package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/img"
)

func TestWheelScrollsWithoutStealingTheFocus(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "deploys")
	m.render()
	before := m.selected()
	run(m, tea.MouseWheelMsg{X: m.mainX() + 5, Y: 5, Button: tea.MouseWheelUp})
	if m.selected() >= before {
		t.Fatalf("wheel up did not move up: %d -> %d", before, m.selected())
	}
	run(m, tea.MouseWheelMsg{X: m.mainX() + 5, Y: 5, Button: tea.MouseWheelDown})
	if m.focus == paneChat && m.selected() != before {
		t.Fatalf("wheel down: %d", m.selected())
	}
	prev := m.focus
	run(m, tea.MouseWheelMsg{X: m.mainX() + 5, Y: 5, Button: tea.MouseWheelLeft}) // not a vertical wheel
	if m.focus != prev {
		t.Fatal("a sideways wheel changed the focus")
	}
	run(m, tea.MouseWheelMsg{X: 2, Y: 5, Button: tea.MouseWheelDown}) // over the sidebar: it moves the chats
	press(m, "i")
	run(m, tea.MouseWheelMsg{X: m.mainX() + 5, Y: 5, Button: tea.MouseWheelUp}) // typing: ignored
	press(m, "esc")
}

func TestClicksOnTheSidebarHeadersAndThreadMessages(t *testing.T) {
	m := newTest(t)
	m.render()
	click := func(x, y int) {
		run(m, tea.MouseClickMsg{X: x, Y: y + m.layout.top, Button: tea.MouseLeft})
		run(m, tea.MouseReleaseMsg{X: x, Y: y + m.layout.top, Button: tea.MouseLeft})
		m.render()
	}
	for y, r := range m.layout.side {
		switch {
		case r == rowThreads:
			click(3, y)
			if m.view != sideThreads {
				t.Fatalf("Threads row: view %v", m.view)
			}
		case r == rowActivity:
			click(3, y)
			if m.view != sideActivity {
				t.Fatalf("Activity row: view %v", m.view)
			}
		}
	}
	for y, r := range m.layout.side { // a section header folds
		if r <= rowSection {
			click(3, y)
			break
		}
	}
	m.view = sideConv
	m.goTo(t, "random")
	press(m, "l", "g", "g", "enter")
	m.render()
	for y, i := range m.layout.threadMsg {
		if i >= 0 {
			click(m.threadX()+3, y)
			if m.thread.sel != i {
				t.Fatalf("click on thread row %d selected %d", i, m.thread.sel)
			}
		}
	}
	// a drag in the thread pane copies from it
	ys := []int{}
	for y, i := range m.layout.threadMsg {
		if i >= 0 && strings.TrimSpace(m.layout.threadText[y]) != "" {
			ys = append(ys, y)
		}
	}
	if len(ys) > 1 {
		x := m.threadX() + 2
		run(m, tea.MouseClickMsg{X: x, Y: ys[0] + m.layout.top, Button: tea.MouseLeft})
		run(m, tea.MouseMotionMsg{X: x + 10, Y: ys[len(ys)-1] + m.layout.top, Button: tea.MouseLeft})
		m.render()
		run(m, tea.MouseMotionMsg{X: x, Y: ys[0] + m.layout.top, Button: tea.MouseLeft}) // back above the start
		if out := m.render(); !strings.Contains(out, dragSelBg) && m.drag != dragText {
			t.Fatal("no selection in the thread")
		}
		run(m, tea.MouseReleaseMsg{X: x + 10, Y: ys[len(ys)-1] + m.layout.top, Button: tea.MouseLeft})
	}
	// a right click, or a click below the panes, does nothing
	run(m, tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseRight})
	run(m, tea.MouseClickMsg{X: 5, Y: m.height + 5, Button: tea.MouseLeft})
}

func TestPickerMouse(t *testing.T) {
	m := newTest(t)
	press(m, "ctrl+k")
	m.render()
	run(m, tea.MouseWheelMsg{X: m.layout.pickX + 3, Y: m.layout.pickTop + 1, Button: tea.MouseWheelDown})
	if m.picker.cursor != 1 {
		t.Fatalf("wheel down: cursor %d", m.picker.cursor)
	}
	run(m, tea.MouseWheelMsg{X: m.layout.pickX + 3, Y: m.layout.pickTop + 1, Button: tea.MouseWheelUp})
	if m.picker.cursor != 0 {
		t.Fatalf("wheel up: cursor %d", m.picker.cursor)
	}
	run(m, tea.MouseClickMsg{X: m.layout.pickX + 3, Y: m.layout.pickTop + 1, Button: tea.MouseRight}) // not the left button
	if m.picker == nil {
		t.Fatal("a right click closed the picker")
	}
	run(m, tea.MouseClickMsg{X: m.layout.pickX + 3, Y: 0, Button: tea.MouseLeft}) // inside, on no entry
	if m.picker == nil {
		t.Fatal("a click on the title closed the picker")
	}
	run(m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft}) // outside
	if m.picker != nil {
		t.Fatal("a click outside must close it")
	}
}

func TestViewerMouse(t *testing.T) {
	m := withImages(t, img.HalfBlock)
	twoPictures(t, m)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	fetch(m, cmd)
	_, cmd = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	fetch(m, cmd)
	if m.viewer == nil || m.viewer.at != 1 {
		t.Fatalf("wheel down: %+v", m.viewer)
	}
	_, cmd = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	fetch(m, cmd)
	if m.viewer.at != 0 {
		t.Fatalf("wheel up: %d", m.viewer.at)
	}
	run(m, tea.MouseWheelMsg{Button: tea.MouseWheelLeft}) // nothing
	run(m, tea.MouseMotionMsg{X: 1, Y: 1})                // nothing
	run(m, tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	if m.viewer != nil {
		t.Fatal("a click must close the viewer")
	}
}
