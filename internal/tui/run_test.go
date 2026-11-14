package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/uistate"
)

// capture replaces the terminal takeover: the model is handed to f instead.
func capture(t *testing.T, f func(*model)) {
	t.Helper()
	old := runProgram
	runProgram = func(m *model) error {
		if f != nil {
			f(m)
		}
		return nil
	}
	t.Cleanup(func() { runProgram = old })
}

func sandboxState(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

func TestRunRejectsBadOptions(t *testing.T) {
	capture(t, nil)
	fake := chat.NewFake()
	for name, o := range map[string]Options{
		"keys":       {Keys: map[string][]string{"fly": {"f"}}},
		"sections":   {Sections: []Section{{Name: "", Channels: nil}}},
		"clock":      {Clock: "13h"},
		"icon":       {PrivateIcon: "abc"},
		"theme":      {Theme: "nope"},
		"images":     {Images: "sixel"},
		"image size": {ImageWidth: 1},
		"image high": {ImageHeight: 1},
	} {
		if err := Run(fake, o); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestRunAppliesOptionsAndRestoresState(t *testing.T) {
	sandboxState(t)
	old := privateIcon
	defer func() { privateIcon = old }()
	on, emoji := false, false
	fake := chat.NewFake()
	if err := uistate.Save(uistate.State{Workspaces: map[string]uistate.Workspace{fake.Workspace(): {
		Theme: "dracula", Layout: "panels", Clock: "12h", ImagesOn: &on, CustomEmoji: &emoji,
		ComposerHeight: 3, SidebarWidth: 30, Collapsed: map[string]bool{"Channels": true},
		Conversation: "C2", SelectedTS: "9.0", ChatOffset: 4,
	}}}); err != nil {
		t.Fatal(err)
	}
	var got *model
	capture(t, func(m *model) { got = m })
	if err := Run(fake, Options{Theme: "nord", PrivateIcon: "◆", StatePath: uistate.Path(), Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("the program never started")
	}
	if got.opts.Theme != "dracula" || got.opts.Layout != "panels" || got.opts.Clock != "12h" || got.opts.Images != "off" || got.opts.ComposerHeight != 3 || got.opts.SidebarWidth != 30 || got.opts.CustomEmoji {
		t.Fatalf("saved preferences not applied: %+v", got.opts)
	}
	if got.restoreConv != "C2" || got.restoreTS != "9.0" || got.restoreOff != 4 || !got.collapsed["Channels"] || privateIcon != "◆" {
		t.Fatalf("restore: %q %q %d %v %q", got.restoreConv, got.restoreTS, got.restoreOff, got.collapsed, privateIcon)
	}
	// a normal quit writes the state back
	s, err := uistate.Load()
	if err != nil || s.Workspaces[fake.Workspace()].Theme == "" {
		t.Fatalf("state not saved on quit: %+v %v", s, err)
	}
}

func TestRunSurvivesACorruptState(t *testing.T) {
	sandboxState(t)
	if err := uistate.Save(uistate.State{}); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(uistate.Path(), "{corrupt"); err != nil {
		t.Fatal(err)
	}
	capture(t, nil)
	if err := Run(chat.NewFake(), Options{StatePath: uistate.Path()}); err != nil {
		t.Fatalf("a malformed state must not stop the app: %v", err)
	}
}

func TestSaveUIStateNeedsAPathAndRecordsTheView(t *testing.T) {
	sandboxState(t)
	m := newTest(t)
	if err := m.saveUIState(); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("no state path: %v", err)
	}
	m.opts.StatePath = uistate.Path()
	m.goTo(t, "tools-team")
	press(m, "l", "k")
	if err := m.saveUIState(); err != nil {
		t.Fatal(err)
	}
	s, _ := uistate.Load()
	w := s.Workspaces[m.backend.Workspace()]
	if w.Conversation == "" || w.ImagesOn == nil || w.SidebarWidth == 0 {
		t.Fatalf("saved workspace: %+v", w)
	}
}

func TestViewDeclaresAltScreenAndMouse(t *testing.T) {
	m := newTest(t)
	v := m.View()
	if !v.AltScreen || v.MouseMode == 0 {
		t.Fatalf("view: %+v", v)
	}
}

func writeFile(path, body string) error { return os.WriteFile(path, []byte(body), 0o600) }
