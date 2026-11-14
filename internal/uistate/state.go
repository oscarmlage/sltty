// Package uistate persists local, non-secret TUI preferences.
package uistate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Workspace struct {
	Theme          string          `json:"theme,omitempty"`
	Layout         string          `json:"layout,omitempty"`
	Clock          string          `json:"clock,omitempty"`
	ImagesOn       *bool           `json:"images_on,omitempty"`
	CustomEmoji    *bool           `json:"custom_emoji,omitempty"`
	ComposerHeight int             `json:"composer_height,omitempty"`
	SidebarWidth   int             `json:"sidebar_width,omitempty"`
	Collapsed      map[string]bool `json:"collapsed,omitempty"`
	Conversation   string          `json:"conversation,omitempty"`
	SelectedTS     string          `json:"selected_ts,omitempty"`
	ChatOffset     int             `json:"chat_offset,omitempty"`
}
type State struct {
	Workspaces map[string]Workspace `json:"workspaces"`
}

func Path() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "sltty", "state.json")
}
func Load() (State, error) {
	var s State
	b, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return State{Workspaces: map[string]Workspace{}}, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	if s.Workspaces == nil {
		s.Workspaces = map[string]Workspace{}
	}
	return s, nil
}
func Save(s State) error {
	if s.Workspaces == nil {
		s.Workspaces = map[string]Workspace{}
	}
	if err := os.MkdirAll(filepath.Dir(Path()), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path() + ".tmp"
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}
