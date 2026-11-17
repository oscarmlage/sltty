package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestBuiltinThemesAreValid(t *testing.T) {
	ts, err := loadThemes(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range ts.names {
		th, err := ts.get(n, "")
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		if err := validate(th); err != nil {
			t.Fatalf("%s completed: %v", n, err)
		}
	}
}

func TestCustomThemes(t *testing.T) {
	ts, err := loadThemes(map[string]json.RawMessage{
		"Mine": json.RawMessage(`{"base": "nord", "primary": "#ff00ff", "layout": "slack"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	th, err := ts.get("mine", "")
	if err != nil || th.Primary != "#ff00ff" || th.Background != "#2e3440" || th.Layout != layoutSlack {
		t.Fatalf("mine: %+v %v", th, err)
	}
	for name, raw := range map[string]string{
		"bad color":   `{"primary": "pink"}`,
		"bad base":    `{"base": "nope"}`,
		"bad layout":  `{"layout": "tabs"}`,
		"unknown key": `{"primry": "#ffffff"}`,
	} {
		if _, err := loadThemes(map[string]json.RawMessage{"x": json.RawMessage(raw)}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := ts.get("nope", ""); err == nil {
		t.Error("unknown theme: no error")
	}
}

// eachLayout runs f with every built-in theme in both layouts.
func eachLayout(t *testing.T, f func(t *testing.T, m *model)) {
	t.Cleanup(func() { applyTheme(mustTheme(defaultTheme, "")) })
	for _, b := range builtinThemes {
		for _, l := range []string{layoutSlack, layoutPanels} {
			t.Run(b.name+"/"+l, func(t *testing.T) {
				applyTheme(mustTheme(b.name, l))
				f(t, newTest(t))
			})
		}
	}
}

func mustTheme(name, layout string) Theme {
	ts, _ := loadThemes(nil)
	th, err := ts.get(name, layout)
	if err != nil {
		panic(err)
	}
	return th
}

func TestEveryThemeFitsTheScreen(t *testing.T) {
	eachLayout(t, func(t *testing.T, m *model) {
		m.goTo(t, "tools-team")
		press(m, "l", "enter", "?")
		out := m.render()
		for i, l := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(l); w != m.width {
				t.Fatalf("line %d is %d wide, want %d: %q", i, w, m.width, ansi.Strip(l))
			}
		}
		if n := strings.Count(out, "\n") + 1; n != m.height {
			t.Fatalf("%d lines, want %d", n, m.height)
		}
	})
}

func TestPanelsMouse(t *testing.T) {
	t.Cleanup(func() { applyTheme(mustTheme(defaultTheme, "")) })
	applyTheme(mustTheme("nord", ""))
	m := newTest(t)
	m.render()
	for y, ci := range m.layout.side {
		if ci >= 0 && m.convs[ci].Name == "random" {
			run(m, tea.MouseClickMsg{X: 4, Y: y + m.layout.top, Button: tea.MouseLeft})
			break
		}
	}
	if m.current().Name != "random" {
		t.Fatalf("click in the boxed sidebar opened %q", m.current().Name)
	}
	run(m, tea.MouseClickMsg{X: m.side + 1, Y: 5, Button: tea.MouseLeft}) // the box border next to the gap
	run(m, tea.MouseMotionMsg{X: 36, Y: 5, Button: tea.MouseLeft})
	run(m, tea.MouseReleaseMsg{X: 36, Y: 5, Button: tea.MouseLeft})
	if m.side != 36 {
		t.Fatalf("side %d after dragging the border", m.side)
	}
}

func TestThemePickerPreviewsAndReverts(t *testing.T) {
	t.Cleanup(func() { applyTheme(mustTheme(defaultTheme, "")) })
	m := newTest(t)
	press(m, "T", "esc", "j", "j")
	if theme.Background == "" {
		t.Fatalf("no preview: theme %q", m.themeName)
	}
	press(m, "esc") // close: back to aubergine
	if m.themeName != defaultTheme || theme.SidebarBg != "#3f0e40" {
		t.Fatalf("not reverted: %q", m.themeName)
	}
	press(m, "T")
	typeText(m, "nord")
	press(m, "enter")
	if m.themeName != "nord" || theme.Primary != "#88c0d0" || !boxed() {
		t.Fatalf("picked %q", m.themeName)
	}
	press(m, "L")
	if boxed() || m.themeName != "nord" {
		t.Fatal("L did not switch to the slack layout")
	}
}

// A theme's background is the terminal's own while sltty runs, not painted
// cell by cell: the renderer's scrolls and erases fill with the terminal's
// background, and a painted one left repeated rows behind.
func TestThemeBackgroundIsTheTerminals(t *testing.T) {
	m := newTest(t)
	t.Cleanup(func() { m.setTheme(defaultTheme, "") })
	m.setTheme("nord", "")
	v := m.View()
	if v.BackgroundColor == nil || v.ForegroundColor == nil {
		t.Fatal("nord: the view does not set the terminal's colors")
	}
	if r, g, b, _ := v.BackgroundColor.RGBA(); r>>8 != 0x2e || g>>8 != 0x34 || b>>8 != 0x40 {
		t.Fatalf("background %v", v.BackgroundColor)
	}
	if bg := bgSeq(theme.Background); strings.Contains(v.Content, bg) {
		t.Fatal("nord's background is still painted cell by cell")
	}
	if !strings.Contains(v.Content, bgSeq(theme.SidebarBg)) {
		t.Fatal("the sidebar keeps its own color")
	}
	// On the terminal's background (aubergine), the terminal is left alone.
	m.setTheme(defaultTheme, "")
	if v := m.View(); v.BackgroundColor != nil || v.ForegroundColor != nil {
		t.Fatal("aubergine must not change the terminal's colors")
	}
}
