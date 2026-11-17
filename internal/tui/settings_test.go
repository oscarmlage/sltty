package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/oscarmlage/sltty/internal/chat"
)

func TestSettingsPanel(t *testing.T) {
	m := newTest(t)
	press(m, ";")
	if m.settings == nil || !strings.Contains(screen(m), "Settings") {
		t.Fatal("; did not open the settings")
	}
	was := m.themeName
	press(m, "l") // theme: next one, live
	if m.themeName == was {
		t.Fatal("l on Theme did not switch it")
	}
	press(m, "h")
	if m.themeName != was {
		t.Fatal("h did not go back")
	}
	press(m, "j", "enter") // layout
	if !boxed() {
		t.Fatal("enter on Layout did not switch it")
	}
	press(m, "j", "j", "j", "j", "j", "j") // past sidebar chats, clock, images, emoji and read-on-enter: composer
	rows := m.inputRows
	press(m, "l", "l")
	if m.inputRows != rows+2 || !strings.Contains(screen(m), `"composer_height": 4`) {
		t.Fatalf("composer %d, want %d", m.inputRows, rows+2)
	}
	press(m, "j", "j", "j", "j", "enter") // sidebar width, cache and TTL, then keybindings
	if !m.help || m.sheet == nil || m.sheet.title != "Keybindings" {
		t.Fatal("enter on Keybindings did not open them")
	}
	press(m, "esc")
	if m.help || m.settings == nil {
		t.Fatal("closing the keys should go back to the settings")
	}
	press(m, "j", "enter")
	if !m.help || m.sheet.title != "Config file" || !strings.Contains(screen(m), "composer_height") {
		t.Fatal("no config reference")
	}
	press(m, "q", "q")
	if m.help || m.settings != nil {
		t.Fatal("q q did not close both")
	}
	command_(m, "settings")
	if m.settings == nil {
		t.Fatal(":settings did not open it")
	}
}

func TestClockSetting(t *testing.T) {
	t.Cleanup(func() { hour12 = false })
	m := newTest(t)
	at := time.Date(2026, 11, 13, 21, 5, 0, 0, time.Local)
	if got := clock(at, at); got != "21:05" {
		t.Fatalf("24h by default: %q", got)
	}
	press(m, ";", "j", "j", "j") // past theme, layout and sidebar chats
	if !strings.Contains(screen(m), "‹ 24h") {
		t.Fatal("no clock in the settings")
	}
	press(m, "l")
	if !hour12 || clock(at, at) != "9:05 PM" || clock(at, at.AddDate(0, 0, 1)) != "Yesterday 9:05 PM" {
		t.Fatalf("l did not switch to 12h: %q", clock(at, at))
	}
	s := screen(m)
	if !strings.Contains(s, "‹ 12h") || !strings.Contains(s, `"clock": "12h"`) || !regexp.MustCompile(`\d:\d\d [AP]M`).MatchString(s) {
		t.Fatalf("the screen behind is not on 12h:\n%s", s)
	}
	press(m, "h")
	if hour12 {
		t.Fatal("h did not switch back")
	}
	command_(m, "clock 12h")
	if !hour12 {
		t.Fatal(":clock 12h")
	}
	if newTest(t); hour12 {
		t.Fatal("a new model takes the config's clock")
	}
	if newModel(chat.NewFake(), m.km, Options{Clock: "12h"}); !hour12 {
		t.Fatal(`"clock": "12h" in the config`)
	}
}

func TestKeysSheetSearch(t *testing.T) {
	m := newTest(t)
	press(m, "?")
	all := len(m.sheet.visible())
	press(m, "/")
	typeText(m, "edit")
	vis := m.sheet.visible()
	if len(vis) == 0 || len(vis) >= all {
		t.Fatalf("filter: %d of %d", len(vis), all)
	}
	for _, r := range vis {
		if !strings.Contains(strings.ToLower(r[0]+r[1]), "edit") {
			t.Fatalf("%v does not match", r)
		}
	}
	press(m, "enter", "j")
	if m.sheet.cursor != 1 {
		t.Fatal("j after the search did not move")
	}
	press(m, "esc") // first clears the filter
	if !m.help || len(m.sheet.visible()) != all {
		t.Fatal("esc did not clear the filter")
	}
	press(m, "esc")
	if m.help {
		t.Fatal("esc did not close")
	}
}
