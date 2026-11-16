package tui

// Run: the program's entry point and the persistence of UI preferences.

import (
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/img"
	"github.com/oscarmlage/sltty/internal/uistate"
)

// Account is one chat service in a tab: its backend and its own sidebar
// sections (config patterns; none keeps the service's own sections).
type Account struct {
	Backend  chat.Backend
	Sections []Section
}

// Run shows the TUI for a single account until the user quits.
func Run(b chat.Backend, opts Options) error {
	return RunAccounts([]Account{{Backend: b, Sections: opts.Sections}}, opts)
}

// RunAccounts shows the TUI until the user quits, one tab per account. Bad
// key bindings in opts are reported before the screen is taken over. The
// visual settings are one set for every account.
func RunAccounts(accounts []Account, opts Options) error {
	if len(accounts) == 0 {
		return fmt.Errorf("no account to show")
	}
	// state.json is local UI preference; config.json remains the declarative
	// configuration surface. A malformed state never prevents the app opening.
	state := uistate.State{Workspaces: map[string]uistate.Workspace{}}
	var stateErr error
	if opts.StatePath != "" {
		state, stateErr = uistate.Load()
	}
	if stateErr == nil && opts.StatePath != "" {
		// The visual settings come from the first account that saved them.
		var saved uistate.Workspace
		found := false
		for _, a := range accounts {
			if saved, found = state.Workspaces[a.Backend.Workspace()]; found {
				break
			}
		}
		if found {
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
			if saved.HistoryCache != nil {
				if *saved.HistoryCache {
					opts.HistoryCacheTTL = time.Duration(saved.HistoryCacheTTL) * time.Minute
				} else {
					opts.HistoryCacheTTL = 0
				}
			}
		}
	}
	km, err := newKeymap(opts.Keys)
	if err != nil {
		return err
	}
	for _, a := range accounts {
		if err := validateSections(a.Sections); err != nil {
			return err
		}
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
	models := make([]*model, len(accounts))
	for i, a := range accounts {
		o := opts
		o.Sections = a.Sections
		m := newModel(a.Backend, km, o)
		m.themes = ts
		if stateErr == nil && opts.StatePath != "" {
			if saved, ok := state.Workspaces[a.Backend.Workspace()]; ok {
				m.collapsed = saved.Collapsed
				m.restoreConv, m.restoreTS, m.restoreOff = saved.Conversation, saved.SelectedTS, saved.ChatOffset
			}
		}
		m.imgs.mode = mode
		models[i] = m
	}
	if len(models) == 1 {
		err = runProgram(models[0])
	} else {
		a := newApp(models)
		err = runApp(a)
		for _, m := range models { // the settings as the last tab left them
			if m != a.cur() {
				shareSettings(a.cur(), m)
			}
		}
	}
	// Position is intentionally automatic: a normal quit restores it even if
	// the user never runs :save for their visual preferences.
	if err == nil && opts.StatePath != "" {
		for _, m := range models {
			_ = m.saveUIState()
		}
	}
	return err
}

// runApp takes over the terminal for several accounts; tests replace it.
var runApp = func(a *app) error {
	_, err := tea.NewProgram(a).Run()
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
	cacheOn := m.opts.HistoryCacheTTL > 0
	current, ts := "", ""
	if c := m.current(); c != nil {
		current = c.ID
		if x := m.selectedMsg(); x != nil {
			ts = x.TS
		}
	}
	s.Workspaces[m.backend.Workspace()] = uistate.Workspace{
		Theme: m.themeName, Layout: m.layoutName, Clock: clockName(), ImagesOn: &on, CustomEmoji: &emoji,
		ComposerHeight: m.inputRows, SidebarWidth: m.side, HistoryCache: &cacheOn, HistoryCacheTTL: int(m.opts.HistoryCacheTTL / time.Minute), Collapsed: m.collapsed,
		Conversation: current, SelectedTS: ts, ChatOffset: m.chatOff,
	}
	return uistate.Save(s)
}
