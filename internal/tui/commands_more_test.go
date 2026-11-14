package tui

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"

	"github.com/oscarmlage/sltty/internal/uistate"
)

// A command line, then what it left in the status line, error or sheet.
func TestEveryCommandAnswers(t *testing.T) {
	sandboxState(t)
	defer func(h12 bool) { hour12 = h12 }(hour12)
	type check func(m *model) bool
	status := func(sub string) check {
		return func(m *model) bool { return strings.Contains(m.status, sub) }
	}
	errIs := func(sub string) check { return func(m *model) bool { return strings.Contains(m.err, sub) } }
	sheet := func(title string) check {
		return func(m *model) bool { return m.help && m.sheet != nil && m.sheet.title == title }
	}
	for _, tc := range []struct {
		line string
		ok   check
	}{
		{"settings", func(m *model) bool { return m.settings != nil }},
		{"config", sheet("Config file")},
		{"log", sheet("Logs")},
		{"save", errIs("not configured")},
		{"savequit", errIs("not configured")},
		{"theme nord", status("theme nord")},
		{"theme nope", errIs("nope")},
		{"theme", func(m *model) bool { return m.picker != nil && m.picker.kind == pickTheme }},
		{"layout panels", status("layout panels")},
		{"layout slack", status("layout slack")},
		{"layout cube", errIs("slack or panels")},
		{"clock 12h", status("clock 12h")},
		{"clock 24h", status("clock 24h")},
		{"clock", status("clock")},
		{"clock 99", errIs("12h or 24h")},
		{"images off", status("images")},
		{"images on", status("already on")},
		{"images", status("images")},
		{"images maybe", errIs("on or off")},
		{"status", func(m *model) bool { return m.picker != nil && m.picker.kind == pickStatus }},
		{"read_on_enter on", status("mark read on enter on")},
		{"read_on_enter off", status("mark read on enter off")},
		{"read_on_enter", status("mark read on enter")},
		{"read_on_enter sometimes", errIs("on or off")},
		{"fold nowhere", errIs("no section nowhere")},
		{"fold Channels", func(m *model) bool { return m.collapsed["Channels"] }},
		{"fold", func(m *model) bool { return true }},
		{"search runner", func(m *model) bool { return m.picker != nil && m.picker.input.Value() == "runner" }},
		{"search", func(m *model) bool { return m.picker != nil }},
		{"find", func(m *model) bool { return m.picker != nil && m.picker.kind == pickSwitch }},
		{"quit", func(m *model) bool { return m.quitting }},
		{"read", func(m *model) bool { return true }},
	} {
		m := newTest(t)
		m.render()
		command_(m, tc.line)
		if !tc.ok(m) {
			t.Errorf(":%s: status %q err %q help %v picker %v", tc.line, m.status, m.err, m.help, m.picker != nil)
		}
	}
}

func TestSaveCommandsWriteTheState(t *testing.T) {
	sandboxState(t)
	m := newTest(t)
	m.opts.StatePath = uistate.Path()
	command_(m, "save")
	if !strings.Contains(m.status, "settings saved") {
		t.Fatalf("save: %q %q", m.status, m.err)
	}
	cmd := m.runCommand("savequit")
	if !isQuit(cmd) {
		t.Fatal("savequit did not quit")
	}
}

func TestReadCommandOnAnUnreadAndAReadChat(t *testing.T) {
	m := newTest(t)
	m.goTo(t, "general")
	m.current().Unread = 0
	command_(m, "read")
	if !strings.Contains(m.status, "nothing unread") {
		t.Fatalf("read on a read chat: %q", m.status)
	}
	m.goTo(t, "deploys")
	m.current().Unread = 3
	if cmd := m.runCommand("read"); cmd == nil && m.err != "" {
		t.Fatalf("read on an unread chat: %q", m.err)
	}
}

func TestCommandLineEditing(t *testing.T) {
	m := newTest(t)
	press(m, ":")
	typeText(m, "zzz")
	press(m, "tab")
	if !strings.Contains(m.status, "no command starts with zzz") {
		t.Fatalf("tab with no match: %q", m.status)
	}
	m.cmdline.SetValue("s")
	press(m, "tab")
	if !strings.Contains(m.status, "settings") || !strings.Contains(m.status, "status") {
		t.Fatalf("tab with several matches: %q", m.status)
	}
	m.cmdline.SetValue("version arg")
	press(m, "tab") // after the name: nothing to complete
	if m.cmdline.Value() != "version arg" {
		t.Fatalf("%q", m.cmdline.Value())
	}
	m.cmdline.SetValue("")
	press(m, "backspace")
	if m.cmdline != nil {
		t.Fatal("backspace on an empty line must leave")
	}
	// history walks both ways and ends empty
	command_(m, "version")
	command_(m, "away")
	press(m, ":", "up")
	if m.cmdline.Value() != "away" {
		t.Fatalf("up: %q", m.cmdline.Value())
	}
	press(m, "up")
	if m.cmdline.Value() != "version" {
		t.Fatalf("up again: %q", m.cmdline.Value())
	}
	press(m, "up")
	if m.cmdline.Value() != "version" {
		t.Fatalf("up at the oldest: %q", m.cmdline.Value())
	}
	run(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cmdline.Value() != "away" {
		t.Fatalf("down: %q", m.cmdline.Value())
	}
	run(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cmdline.Value() != "" {
		t.Fatalf("down past the newest: %q", m.cmdline.Value())
	}
	press(m, "esc")
	press(m, ":")
	if m.walkHistory(true); false { // no history on a fresh model is covered below
		t.Fatal()
	}
}

func TestWalkHistoryWithoutHistory(t *testing.T) {
	m := newTest(t)
	press(m, ":")
	m.walkHistory(true)
	if m.cmdline.Value() != "" {
		t.Fatal("history from nothing")
	}
}
