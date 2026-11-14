package tui

import (
	"strings"
	"testing"
)

// In the Threads and Activity lists every movement key walks the list.
func TestListViewKeys(t *testing.T) {
	for _, header := range []int{3, 2} { // Threads, Activity: k steps up from #general
		m := newTest(t)
		for range header {
			press(m, "k")
		}
		press(m, "l") // focus the list
		if m.view == sideConv {
			t.Fatalf("not in a list view (%d)", header)
		}
		press(m, "j", "k", "ctrl+d", "ctrl+u", "G", "g", "g")
		press(m, "r", "e") // react and edit mean nothing here
		if m.picker != nil {
			t.Fatal("react opened a picker in a list view")
		}
		press(m, "i") // reply: a thread in Threads, nothing in Activity
		press(m, "esc")
		press(m, "enter")
	}
}

func TestThreadPaneKeys(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "random")
	press(m, "l", "g", "g", "enter") // the thread of the pizza message, focused
	if m.thread == nil || m.focus != paneThread {
		t.Fatal("no thread focus")
	}
	press(m, "j", "j", "k", "G", "g", "g", "ctrl+d", "ctrl+u")
	first := m.thread.sel
	press(m, "G")
	if m.thread.sel != len(m.thread.msgs)-1 {
		t.Fatalf("G: %d of %d", m.thread.sel, len(m.thread.msgs))
	}
	press(m, "g", "g")
	if m.thread.sel != 0 {
		t.Fatalf("gg: %d (was %d)", m.thread.sel, first)
	}
	w := m.threadW
	press(m, ">")
	press(m, "<", "<")
	if m.threadW == w {
		t.Fatal("the thread divider did not move")
	}
	press(m, "=")
	press(m, "h", "h") // thread -> chat -> sidebar
	if m.focus != paneSide {
		t.Fatalf("focus %v", m.focus)
	}
	press(m, "tab")
	if m.focus != paneChat {
		t.Fatalf("tab from the sidebar: %v", m.focus)
	}
	press(m, "tab")
	if m.focus != paneSide {
		t.Fatalf("tab from the chat: %v", m.focus)
	}
}

func TestChatPagingAndSidebarDivider(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "deploys")
	press(m, "l", "ctrl+d", "ctrl+d", "ctrl+u", "G", "g", "g")
	side := m.side
	press(m, "h", ">")
	if m.side == side {
		t.Fatal("the sidebar divider did not move")
	}
	press(m, "=")
	if m.side != defaultSide {
		t.Fatalf("= did not reset: %d", m.side)
	}
	press(m, "ctrl+s")
	if m.picker == nil {
		t.Fatal("ctrl+s did not open the status picker")
	}
	press(m, "esc", "esc")
}

func TestEditLastWhenThereIsNothingOfYours(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "general") // none of its messages is yours
	press(m, "i", "up")
	if !strings.Contains(m.status, "nothing of yours") {
		t.Fatalf("status %q", m.status)
	}
	// in a thread: your reply is found and selected
	press(m, "esc")
	m.goTo(t, "random")
	press(m, "l", "g", "g", "enter")
	press(m, "i", "up")
	if m.editing == nil {
		t.Fatalf("up did not start editing your last reply (status %q)", m.status)
	}
}

func TestQuitThroughTheMenuKeysAndMisc(t *testing.T) {
	m := newTest(t)
	press(m, "q")
	press(m, "y")
	m = newTest(t)
	press(m, "J", "K")
	press(m, "ctrl+k", "esc", "esc")
	m.goTo(t, "help")
	press(m, "l", "esc")
	press(m, "n") // next match with no search: nothing happens
}
