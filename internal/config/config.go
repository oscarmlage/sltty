// Package config reads sltty's config file: key bindings and themes.
// It never holds credentials.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config is ~/.config/sltty/config.json.
type Config struct {
	// Keys rebinds actions: action name to its keys, replacing the default
	// keys of that action. Actions left out keep their defaults.
	Keys map[string][]string `json:"keys,omitempty"`
	// Theme is a built-in or custom theme name; Layout ("slack" or
	// "panels") replaces the theme's own.
	Theme  string `json:"theme,omitempty"`
	Layout string `json:"layout,omitempty"`
	// Clock shows the times as "24h" (15:04, the default) or "12h" (3:04 PM).
	Clock string `json:"clock,omitempty"`
	// Themes are custom themes: name to colors, optionally on a "base".
	Themes map[string]json.RawMessage `json:"themes,omitempty"`
	// Sections are custom sidebar groups, shown above Channels in this
	// order. Patterns are globs ("tools-*"); "@name" matches DMs.
	Sections []Section `json:"sections,omitempty"`
	// PrivateIcon replaces the 🔒 of private channels, e.g. "◆".
	PrivateIcon string `json:"private_icon,omitempty"`
	// Images: "auto" (the default: kitty graphics where the terminal has
	// them, half blocks elsewhere), "kitty", "halfblock" or "off".
	Images string `json:"images,omitempty"`
	// CustomEmoji draws the workspace's :emoji: as pictures (kitty only);
	// on unless false.
	CustomEmoji *bool `json:"custom_emoji,omitempty"`
	// ImageWidth and ImageHeight bound a picture, in cells (40 × 14).
	ImageWidth  int `json:"image_width,omitempty"`
	ImageHeight int `json:"image_height,omitempty"`
	// ComposerHeight is the text rows of a composer (2); SidebarWidth the
	// sidebar's columns (26).
	ComposerHeight int `json:"composer_height,omitempty"`
	SidebarWidth   int `json:"sidebar_width,omitempty"`
	// HistoryCacheTTL is the local history cache lifetime in minutes; 0 disables it.
	HistoryCacheTTL int `json:"history_cache_ttl,omitempty"`
	// PollSeconds is how often Slack is asked for new messages (it has no
	// live connection here): 0 for the default (20), -1 for never.
	PollSeconds int `json:"poll_seconds,omitempty"`
	// Accounts are the services to open, one tab each, in this order:
	// "slack", "telegram". Empty: every service with a saved session.
	Accounts []string `json:"accounts,omitempty"`
	// ReadOnEnter marks a conversation read when it is opened; on by default.
	ReadOnEnter *bool `json:"read_on_enter,omitempty"`
}

type Section struct {
	Name     string   `json:"name"`
	Channels []string `json:"channels"`
	// Collapsed starts the section folded: only the open and the unread
	// conversations show.
	Collapsed bool `json:"collapsed,omitempty"`
}

func Dir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "sltty")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sltty")
}

func Path() string { return filepath.Join(Dir(), "config.json") }

// Load reads the config file. A missing file is no error: it means the
// defaults. Unknown fields are, so a typo does not go unnoticed.
func Load(path string) (Config, bool, error) {
	var c Config
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, true, fmt.Errorf("%s: %w", path, err)
	}
	return c, true, nil
}

// Field documents one key of the config file, for `sltty config` and the
// settings panel.
type Field struct {
	Key, Type, Default, Help string
}

// Reference is every key the config file takes.
var Reference = []Field{
	{"accounts", "array", "every signed-in service", `the tabs, in order: "slack", "telegram"`},
	{"theme", "string", "aubergine", "theme name: a built-in one or one of themes"},
	{"layout", "string", "the theme's", `"slack" (msga look) or "panels" (slk look)`},
	{"clock", "string", "24h", `"24h" (15:04) or "12h" (3:04 PM)`},
	{"keys", "object", "see `sltty keys`", "action → list of keys; a list replaces the action's defaults"},
	{"themes", "object", "", `custom themes: name → colors, optionally on a "base" theme`},
	{"sections", "array", "", `custom Slack sidebar groups: {"name", "channels": globs, "@name" for DMs, "collapsed"}`},
	{"private_icon", "string", "🔒", "the mark of private channels, e.g. ◆"},
	{"images", "string", "auto", `"auto", "kitty", "halfblock" or "off"`},
	{"custom_emoji", "bool", "true", "the workspace's :emoji: as pictures (kitty)"},
	{"read_on_enter", "bool", "true", "mark a conversation read when opening it"},
	{"image_width", "int", "40", "the widest picture, in cells"},
	{"image_height", "int", "14", "the tallest picture, in cells"},
	{"composer_height", "int", "2", "text rows of a composer (+/- and dragging change it)"},
	{"sidebar_width", "int", "26", "sidebar columns (</> and dragging change it)"},
	{"poll_seconds", "int", "20", "how often Slack is checked for new messages and unread counts; -1 never"},
	{"history_cache_ttl", "int", "0", "local history cache lifetime in minutes; 0 disables it"},
}
