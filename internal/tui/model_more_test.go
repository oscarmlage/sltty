package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"

	"github.com/oscarmlage/sltty/internal/chat"
)

var errBoom = errors.New("boom")

// Every reply of the backend that failed is shown, never swallowed.
func TestBackendErrorsAreShown(t *testing.T) {
	for name, msg := range map[string]any{
		"presence": presenceMsg{err: errBoom},
		"snooze":   snoozeMsg{err: errBoom},
		"convs":    convsMsg{err: errBoom},
		"history":  historyMsg{err: errBoom},
		"replies":  repliesMsg{err: errBoom},
		"threads":  threadsMsg{err: errBoom},
		"activity": activityMsg{err: errBoom},
		"marked":   markedMsg{err: errBoom},
		"changed":  changedMsg{err: errBoom},
		"opened":   openedMsg{err: errBoom},
	} {
		m := newTest(t)
		m.err = ""
		run(m, msg)
		if !strings.Contains(m.err, "boom") {
			t.Errorf("%s: error not shown (%q)", name, m.err)
		}
		if name != "presence" && name != "snooze" && name != "opened" && !strings.Contains(screen(m), "boom") {
			t.Errorf("%s: error not on screen", name)
		}
	}
}

func TestPresenceSnoozeAndConnectionMessages(t *testing.T) {
	m := newTest(t)
	run(m, presenceMsg{away: true, set: true})
	if !m.away || m.status != "you are away" {
		t.Fatalf("away: %v %q", m.away, m.status)
	}
	run(m, presenceMsg{away: false, set: true})
	if m.away || m.status != "you are active" {
		t.Fatalf("active: %v %q", m.away, m.status)
	}
	run(m, snoozeMsg{minutes: 30})
	if m.snoozeTill.Before(time.Now().Add(29 * time.Minute)) {
		t.Fatal("snooze not noted")
	}
	run(m, snoozeMsg{minutes: 0})
	if !m.snoozeTill.IsZero() {
		t.Fatal("snooze not ended")
	}
}

func TestSpinnerStopsWhenIdle(t *testing.T) {
	m := newTest(t)
	m.loading = false
	m.spinning = true
	run(m, spinner.TickMsg{})
	if m.spinning {
		t.Fatal("spinner kept going while idle")
	}
	m.loading = true
	m.spinning = true
	_, cmd := m.Update(spinner.TickMsg{ID: m.spin.ID()})
	if cmd == nil {
		t.Fatal("spinner stopped while busy")
	}
}

func TestRestoreSelectsTheSavedConversationAndMessage(t *testing.T) {
	m := newTest(t)
	hist, _ := m.backend.History(nil, "C2")
	if len(hist) < 3 {
		t.Fatal("demo history too short")
	}
	m.restoreConv, m.restoreTS, m.restoreOff = "C2", hist[1].TS, 2
	convs, _ := m.backend.Conversations(nil)
	run(m, convsMsg{convs: convs})
	if c := m.current(); c == nil || c.ID != "C2" {
		t.Fatalf("restored %v", c)
	}
	if m.selected() != 1 || m.chatOff != 2 {
		t.Fatalf("selected %d offset %d", m.selected(), m.chatOff)
	}
}

func TestRepliesAndPolling(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "random")
	press(m, "l", "g", "g", "enter")
	if m.thread == nil {
		t.Fatal("no thread")
	}
	msgs := m.thread.msgs
	m.jump = &jump{convID: m.thread.convID, ts: msgs[1].TS, root: m.thread.root}
	run(m, repliesMsg{id: m.thread.convID, root: m.thread.root, msgs: msgs})
	if m.thread.sel != 1 || m.jump != nil {
		t.Fatalf("jump to a reply: sel %d jump %v", m.thread.sel, m.jump)
	}
	// polling: in the thread and in the conversation, until the last attempt
	_, cmd := m.Update(replyPollMsg{id: m.thread.convID, root: m.thread.root, attempt: 0})
	if cmd == nil {
		t.Fatal("poll stopped early")
	}
	_, cmd = m.Update(replyPollMsg{id: "C1", attempt: 3})
	if cmd == nil {
		t.Fatal("the last poll still refreshes")
	}
	_, cmd = m.Update(replyPollMsg{id: "C1", attempt: 0})
	if cmd == nil {
		t.Fatal("poll stopped early")
	}
}

func TestEmptyFeedsBecomeEmptyLists(t *testing.T) {
	m := newTest(t)
	run(m, threadsMsg{})
	run(m, activityMsg{})
	if m.threads == nil || m.activity == nil {
		t.Fatal("a nil feed must become an empty list, or the view keeps loading")
	}
	run(m, threadsMsg{threads: []chat.ThreadSummary{{ConvID: "C1"}}})
	if len(m.threads) != 1 {
		t.Fatal("threads not stored")
	}
}

func TestTargetOutOfRangeAndNoConversation(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "random")
	press(m, "l", "g", "g", "enter")
	m.thread.sel = 99
	if m.focus == paneThread && m.target() != nil {
		t.Fatal("a stale thread selection must not be a target")
	}
	empty := newModel(chat.NewFake(), defaultKeymap(), Options{})
	if empty.selected() != -1 || empty.selectedMsg() != nil || empty.target() != nil {
		t.Fatal("no conversation, no target")
	}
}
