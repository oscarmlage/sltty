package tui

// Run: the program's entry point and the persistence of UI preferences.

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/img"
	"github.com/oscarmlage/sltty/internal/uistate"
)

// Run shows the TUI until the user quits. Bad key bindings in opts are
// reported before the screen is taken over.
func Run(b chat.Backend, opts Options) error {
	// state.json is local UI preference; config.json remains the declarative
	// configuration surface. A malformed state never prevents the app opening.
	state := uistate.State{Workspaces: map[string]uistate.Workspace{}}
	var stateErr error
	if opts.StatePath != "" {
		state, stateErr = uistate.Load()
	}
	if stateErr == nil && opts.StatePath != "" {
		if saved, ok := state.Workspaces[b.Workspace()]; ok {
			if saved.Theme != "" {
				opts.Theme = saved.Theme
			}
			if saved.Layout != "" {
				opts.Layout = saved.Layout
			}
			if saved.Clock != "" {
				opts.Clock = saved.Clock
			}
			if saved.ImagesOn != nil {
				if *saved.ImagesOn {
					opts.Images = "auto"
				} else {
					opts.Images = "off"
				}
			}
			if saved.CustomEmoji != nil {
				opts.CustomEmoji = *saved.CustomEmoji
			}
			if saved.ComposerHeight != 0 {
				opts.ComposerHeight = saved.ComposerHeight
			}
			if saved.SidebarWidth != 0 {
				opts.SidebarWidth = saved.SidebarWidth
			}
		}
	}
	km, err := newKeymap(opts.Keys)
	if err != nil {
		return err
	}
	if err := validateSections(opts.Sections); err != nil {
		return err
	}
	if c := opts.Clock; c != "" && c != "12h" && c != "24h" {
		return fmt.Errorf(`clock %q: "24h" or "12h"`, c)
	}
	if w := lipgloss.Width(opts.PrivateIcon); w > 2 {
		return fmt.Errorf("private_icon %q: at most two columns wide", opts.PrivateIcon)
	} else if w > 0 {
		privateIcon = opts.PrivateIcon
	}
	ts, err := loadThemes(opts.Themes)
	if err != nil {
		return err
	}
	t, err := ts.get(opts.Theme, opts.Layout)
	if err != nil {
		return err
	}
	applyTheme(t)
	mode, err := img.Detect(opts.Images, os.Getenv)
	if err != nil {
		return err
	}
	if w := opts.ImageWidth; w != 0 && (w < 4 || w > img.MaxCells) || opts.ImageHeight != 0 && (opts.ImageHeight < 2 || opts.ImageHeight > img.MaxCells) {
		return fmt.Errorf("image_width / image_height: 4 to %d columns, 2 to %d rows", img.MaxCells, img.MaxCells)
	}
	m := newModel(b, km, opts)
	m.themes = ts
	if stateErr == nil && opts.StatePath != "" {
		if saved, ok := state.Workspaces[b.Workspace()]; ok {
			m.collapsed = saved.Collapsed
			m.restoreConv, m.restoreTS, m.restoreOff = saved.Conversation, saved.SelectedTS, saved.ChatOffset
		}
	}
	m.imgs.mode = mode
	err = runProgram(m)
	// Position is intentionally automatic: a normal quit restores it even if
	// the user never runs :save for their visual preferences.
	if err == nil && opts.StatePath != "" {
		_ = m.saveUIState()
	}
	return err
}

// runProgram takes over the terminal until the model quits; tests replace it.
var runProgram = func(m *model) error {
	_, err := tea.NewProgram(m).Run()
	return err
}

// setTheme applies a theme and layout by name; the names are known good.
// saveUIState writes the current workspace's local visual preferences.
func (m *model) saveUIState() error {
	if m.opts.StatePath == "" {
		return fmt.Errorf("local state is not configured")
	}
	s, err := uistate.Load()
	if err != nil {
		return err
	}
	on, emoji := m.imgs.on, m.imgs.emoji
	current, ts := "", ""
	if c := m.current(); c != nil {
		current = c.ID
		if x := m.selectedMsg(); x != nil {
			ts = x.TS
		}
	}
	s.Workspaces[m.backend.Workspace()] = uistate.Workspace{
		Theme: m.themeName, Layout: m.layoutName, Clock: clockName(), ImagesOn: &on, CustomEmoji: &emoji,
		ComposerHeight: m.inputRows, SidebarWidth: m.side, Collapsed: m.collapsed,
		Conversation: current, SelectedTS: ts, ChatOffset: m.chatOff,
	}
	return uistate.Save(s)
}
