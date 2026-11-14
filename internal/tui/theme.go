package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// A Theme is a palette plus a layout. Layouts:
//   - "slack": a filled sidebar, the conversation and thread on the terminal
//     (or theme) background, composers in rounded boxes (msga's look);
//   - "panels": every pane in its own box, a "▌" cursor and "•" unread
//     marks in the sidebar, filled composers (slk's look).
//
// Colors are "#rrggbb"; an empty Background keeps the terminal's own. Empty
// optional colors are derived from the others in complete.
type Theme struct {
	Base   string `json:"base,omitempty"` // custom themes: start from this one
	Layout string `json:"layout,omitempty"`

	Background string `json:"background,omitempty"`
	Surface    string `json:"surface,omitempty"`
	Text       string `json:"text,omitempty"`
	TextMuted  string `json:"text_muted,omitempty"`
	Primary    string `json:"primary,omitempty"`
	Accent     string `json:"accent,omitempty"`
	Warning    string `json:"warning,omitempty"`
	Error      string `json:"error,omitempty"`
	Border     string `json:"border,omitempty"`

	SidebarBg       string `json:"sidebar_background,omitempty"`
	SidebarText     string `json:"sidebar_text,omitempty"`
	SidebarMuted    string `json:"sidebar_text_muted,omitempty"`
	SidebarSelBg    string `json:"sidebar_selected_bg,omitempty"` // empty: a "▌" cursor instead of a filled row
	SidebarSelFg    string `json:"sidebar_selected_fg,omitempty"`
	SidebarCursorBg string `json:"sidebar_cursor_bg,omitempty"` // the selected chat while the sidebar has no focus

	Selection string `json:"selection,omitempty"` // the selected message
	Username  string `json:"username,omitempty"`
	Me        string `json:"me,omitempty"`
	Badge     string `json:"badge,omitempty"`
	Link      string `json:"link,omitempty"`
	ReactBg   string `json:"reaction_bg,omitempty"`
	ReactMine string `json:"reaction_mine_bg,omitempty"`
}

const (
	layoutSlack  = "slack"
	layoutPanels = "panels"
)

// The built-in themes, in the picker's order. The palettes are the
// published ones of each scheme.
var builtinThemes = []struct {
	name  string
	theme Theme
}{
	{"aubergine", Theme{Layout: layoutSlack,
		Text: "#d1d2d3", TextMuted: "#8a8a8a", Primary: "#1d9bd1", Accent: "#2bac76", Warning: "#ffaf00", Error: "#e01e5a",
		Border: "#444444", Surface: "#262626", Selection: "#303030", Username: "#ffffff",
		SidebarBg: "#3f0e40", SidebarText: "#ffffff", SidebarMuted: "#c9b6c9",
		SidebarSelBg: "#ece6ed", SidebarSelFg: "#1d1c1d", SidebarCursorBg: "#5a2b5c",
		Badge: "#cd2553", ReactBg: "#444444", ReactMine: "#005f87"}},
	{"terminal", Theme{Layout: layoutPanels,
		Text: "#d0d0d0", TextMuted: "#808080", Primary: "#5f5fd7", Accent: "#00d787", Warning: "#ffaf00", Error: "#ff005f",
		Border: "#444444", Surface: "#262626", Selection: "#303030"}},
	{"dracula", Theme{Layout: layoutPanels,
		Background: "#282a36", Surface: "#343746", Text: "#f8f8f2", TextMuted: "#6272a4",
		Primary: "#bd93f9", Accent: "#50fa7b", Warning: "#ffb86c", Error: "#ff5555", Border: "#44475a", SidebarBg: "#21222c"}},
	{"nord", Theme{Layout: layoutPanels,
		Background: "#2e3440", Surface: "#3b4252", Text: "#eceff4", TextMuted: "#8892a6",
		Primary: "#88c0d0", Accent: "#a3be8c", Warning: "#ebcb8b", Error: "#bf616a", Border: "#4c566a", SidebarBg: "#272c36"}},
	// slk mirrors the compact, cool-grey panel palette of the original slk:
	// a near-black sidebar, blue-grey workspace, cyan frame and cyan names.
	{"slk", Theme{Layout: layoutPanels,
		Background: "#2b313c", Surface: "#343b47", Text: "#eceff4", TextMuted: "#8793a6",
		Primary: "#88c0d0", Accent: "#a3be8c", Warning: "#ebcb8b", Error: "#bf616a", Border: "#88c0d0",
		SidebarBg: "#1d222c", SidebarText: "#d8dee9", SidebarMuted: "#7f8b9d", SidebarCursorBg: "#3d4656",
		Selection: "#4a555a", Username: "#88c0d0", Me: "#d08770", Badge: "#88c0d0", Link: "#88c0d0",
		ReactBg: "#3d4656", ReactMine: "#36566a"}},
	{"tokyo night", Theme{Layout: layoutPanels,
		Background: "#1a1b26", Surface: "#24283b", Text: "#c0caf5", TextMuted: "#565f89",
		Primary: "#7aa2f7", Accent: "#9ece6a", Warning: "#e0af68", Error: "#f7768e", Border: "#3b4261", SidebarBg: "#16161e"}},
	{"gruvbox", Theme{Layout: layoutPanels,
		Background: "#282828", Surface: "#3c3836", Text: "#ebdbb2", TextMuted: "#928374",
		Primary: "#83a598", Accent: "#b8bb26", Warning: "#fabd2f", Error: "#fb4934", Border: "#504945", SidebarBg: "#1d2021"}},
	{"catppuccin", Theme{Layout: layoutPanels,
		Background: "#1e1e2e", Surface: "#313244", Text: "#cdd6f4", TextMuted: "#7f849c",
		Primary: "#89b4fa", Accent: "#a6e3a1", Warning: "#f9e2af", Error: "#f38ba8", Border: "#45475a", SidebarBg: "#181825"}},
	{"solarized", Theme{Layout: layoutPanels,
		Background: "#002b36", Surface: "#073642", Text: "#93a1a1", TextMuted: "#657b83",
		Primary: "#268bd2", Accent: "#859900", Warning: "#b58900", Error: "#dc322f", Border: "#0b4f5f", SidebarBg: "#00212b"}},
	{"light", Theme{Layout: layoutPanels,
		Background: "#ffffff", Surface: "#f6f8fa", Text: "#24292e", TextMuted: "#6a737d",
		Primary: "#0366d6", Accent: "#28a745", Warning: "#d9840d", Error: "#cb2431", Border: "#d1d5da",
		SidebarBg: "#24292e", SidebarText: "#f6f8fa", SidebarMuted: "#8c959f", Username: "#0366d6"}},
}

const defaultTheme = "aubergine"

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// themes is the built-ins plus the custom themes of the config file, each
// a JSON object of Theme fields, optionally on top of a "base" theme.
type themeSet struct {
	names  []string
	themes map[string]Theme
}

func loadThemes(custom map[string]json.RawMessage) (*themeSet, error) {
	ts := &themeSet{themes: map[string]Theme{}}
	for _, b := range builtinThemes {
		ts.names = append(ts.names, b.name)
		ts.themes[b.name] = b.theme
	}
	var names []string
	for n := range custom {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		var t Theme
		dec := json.NewDecoder(bytes.NewReader(custom[n]))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&t); err != nil {
			return nil, fmt.Errorf("themes: %s: %w", n, err)
		}
		if t.Base != "" {
			base, ok := ts.themes[strings.ToLower(t.Base)]
			if !ok {
				return nil, fmt.Errorf("themes: %s: unknown base %q", n, t.Base)
			}
			t = overlay(base, t)
		}
		if err := validate(t); err != nil {
			return nil, fmt.Errorf("themes: %s: %w", n, err)
		}
		key := strings.ToLower(n)
		if _, dup := ts.themes[key]; !dup {
			ts.names = append(ts.names, key)
		}
		ts.themes[key] = t
	}
	return ts, nil
}

// get returns a theme by name, its layout replaced when layout is set.
func (ts *themeSet) get(name, layout string) (Theme, error) {
	t, ok := ts.themes[strings.ToLower(cmpOr(name, defaultTheme))]
	if !ok {
		return t, fmt.Errorf("theme %q not found (built in: %s)", name, strings.Join(ts.names, ", "))
	}
	if layout != "" {
		t.Layout = layout
	}
	if err := validate(t); err != nil {
		return t, err
	}
	return complete(t), nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// overlay is base with every field t sets.
func overlay(base, t Theme) Theme {
	b, _ := json.Marshal(t)
	_ = json.Unmarshal(b, &base) // omitempty: only the fields t sets
	return base
}

func validate(t Theme) error {
	if t.Layout != "" && t.Layout != layoutSlack && t.Layout != layoutPanels {
		return fmt.Errorf("layout %q: want %q or %q", t.Layout, layoutSlack, layoutPanels)
	}
	b, _ := json.Marshal(t)
	var fields map[string]string
	_ = json.Unmarshal(b, &fields)
	for k, v := range fields {
		if k != "base" && k != "layout" && !hexColor.MatchString(v) {
			return fmt.Errorf("%s: %q is not a #rrggbb color", k, v)
		}
	}
	return nil
}

// complete fills the optional colors from the main ones.
func complete(t Theme) Theme {
	bg := cmpOr(t.Background, "#1c1c1c") // for mixing only
	set := func(f *string, v string) {
		if *f == "" {
			*f = v
		}
	}
	set(&t.Layout, layoutPanels)
	set(&t.Surface, mix(bg, t.Text, 0.08))
	set(&t.Selection, mix(bg, t.Primary, 0.18))
	set(&t.SidebarText, t.Text)
	set(&t.SidebarMuted, t.TextMuted)
	set(&t.SidebarSelFg, t.SidebarText)
	set(&t.SidebarCursorBg, t.Selection)
	set(&t.Username, t.Primary)
	set(&t.Me, t.Accent)
	set(&t.Badge, t.Error)
	set(&t.Link, t.Primary)
	set(&t.ReactBg, t.Surface)
	set(&t.ReactMine, mix(bg, t.Primary, 0.4))
	return t
}

func rgb(hex string) (r, g, b int) {
	v, _ := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	return int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff)
}

// mix blends b into a by f (0..1).
func mix(a, b string, f float64) string {
	ar, ag, ab := rgb(a)
	br, bg, bb := rgb(b)
	c := func(x, y int) int { return x + int(float64(y-x)*f+0.5) }
	return fmt.Sprintf("#%02x%02x%02x", c(ar, br), c(ag, bg), c(ab, bb))
}

// bgSeq is the escape sequence for a background color; empty keeps the
// terminal's.
func bgSeq(hex string) string {
	if hex == "" {
		return ""
	}
	r, g, b := rgb(hex)
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}
