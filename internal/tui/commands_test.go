package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oscarmlage/sltty/internal/chat"
)

func command_(m *model, line string) {
	press(m, ":")
	typeText(m, line)
	press(m, "enter")
}

func TestCommandMode(t *testing.T) {
	m := newTest(t)
	m.opts.Version = "v1.2.3"
	press(m, ":")
	if m.cmdline == nil || !strings.Contains(screen(m), "COMMAND") {
		t.Fatal(": did not open the command line")
	}
	typeText(m, "ver")
	press(m, "tab")
	if m.cmdline.Value() != "version " {
		t.Fatalf("tab: %q", m.cmdline.Value())
	}
	press(m, "enter")
	if m.cmdline != nil || !strings.Contains(screen(m), "sltty v1.2.3") {
		t.Fatalf("version: %q", m.status)
	}

	command_(m, "find leads") // one match: straight there
	if m.picker != nil || m.current().Name != "leads" {
		t.Fatalf("find leads: at %q", m.current().Name)
	}
	command_(m, "find tools")
	if m.picker == nil || m.picker.kind != pickSwitch || len(m.picker.items) < 2 {
		t.Fatal("find with several matches should leave the switcher open")
	}
	press(m, "esc", "esc")

	command_(m, "help")
	if !m.help || !slices.ContainsFunc(m.sheet.rows, func(r [2]string) bool { return r[0] == ":version" }) {
		t.Fatal(":help does not list the commands")
	}
	press(m, "q")

	command_(m, "nope")
	if !strings.Contains(screen(m), "not a command: nope") {
		t.Fatal("unknown command said nothing")
	}

	press(m, ":")
	press(m, "up") // history: the last one run
	if m.cmdline.Value() != "nope" {
		t.Fatalf("up: %q", m.cmdline.Value())
	}
	press(m, "esc")
	if m.cmdline != nil {
		t.Fatal("esc did not leave COMMAND")
	}
	press(m, ":")
	typeText(m, "xjk") // no esc on an iPad
	if m.cmdline != nil {
		t.Fatal("jk did not leave COMMAND")
	}

	command_(m, "q")
	if !m.quitting {
		t.Fatal(":q did not ask to quit")
	}
}

func TestPresenceAndConnection(t *testing.T) {
	m := newTest(t)
	if s := screen(m); !strings.Contains(s, "● Active") || !strings.Contains(s, "● Connected") {
		t.Fatalf("no indicators:\n%s", s)
	}
	command_(m, "away")
	if !m.away || !strings.Contains(screen(m), "● Away") {
		t.Fatal(":away did not show away")
	}
	command_(m, "active")
	if m.away {
		t.Fatal(":active did not come back")
	}
	m.backend = failing{m.backend}
	m.goTo(t, "random")
	if m.connected || !strings.Contains(screen(m), "Disconnected") {
		t.Fatal("a failed call did not show Disconnected")
	}
}

type failing struct{ chat.Backend }

func (failing) History(context.Context, string) ([]chat.Message, error) {
	return nil, errors.New("network down")
}
