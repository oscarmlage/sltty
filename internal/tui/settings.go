package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/oscarmlage/sltty/internal/img"
)

// The settings panel (; or :settings): theme, layout, the clock, images, the
// composer and sidebar sizes, and the way to the keybindings and the config
// reference. j/k move, h/l (or ←/→) change, enter opens; the screen behind
// shows each change at once. Changes last the session; the panel tells the
// config lines that keep them.

type settings struct{ cursor int }

const (
	setTheme = iota
	setLayout
	setClock
	setImages
	setEmoji
	setReadOnEnter
	setComposer
	setSidebar
	setHistoryCache
	setHistoryCacheTTL
	setKeys
	setConfig
	setCount
)

func (m *model) openSettings() { m.settings = &settings{} }

func (m *model) settingsAction(a action) tea.Cmd {
	s := m.settings
	switch a {
	case actDown:
		s.cursor = min(s.cursor+1, setCount-1)
	case actUp:
		s.cursor = max(s.cursor-1, 0)
	case actTop:
		s.cursor = 0
	case actBottom:
		s.cursor = setCount - 1
	case actFocusRight, actGrow:
		m.changeSetting(1)
	case actFocusLeft, actShrink:
		m.changeSetting(-1)
	case actOpen:
		switch s.cursor {
		case setTheme:
			return m.openPicker(pickTheme, nil)
		case setKeys:
			m.openKeys()
		case setConfig:
			m.openConfigRef()
		default:
			m.changeSetting(1)
		}
	case actBack, actBackOrQuit, actQuit, actSettings:
		m.settings = nil
	case actHelp:
		m.openKeys()
	}
	return nil
}

func (m *model) changeSetting(d int) {
	switch m.settings.cursor {
	case setTheme:
		names := m.themes.names
		i := slices.Index(names, m.themeName)
		m.setTheme(names[(i+d+len(names))%len(names)], m.layoutName)
	case setLayout:
		l := layoutPanels
		if boxed() {
			l = layoutSlack
		}
		m.setTheme(m.themeName, l)
	case setClock:
		hour12 = !hour12
	case setImages:
		m.toggleImages()
	case setEmoji:
		m.imgs.emoji = !m.imgs.emoji
	case setReadOnEnter:
		m.readOnEnter = !m.readOnEnter
	case setComposer:
		m.setInputRows(m.inputRows + d)
	case setSidebar:
		m.setSide(m.side + d*2)
	case setHistoryCache:
		if m.opts.HistoryCacheTTL > 0 {
			m.opts.HistoryCacheTTL, m.cache = 0, nil
		} else {
			m.opts.HistoryCacheTTL = 30 * time.Minute
			m.cache = newHistoryCache(m.backend.Workspace(), m.opts.HistoryCacheTTL)
		}
	case setHistoryCacheTTL:
		choices := []time.Duration{5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour, 60 * 24 * time.Hour}
		i := slices.Index(choices, m.opts.HistoryCacheTTL)
		if i < 0 {
			i = 1
		}
		m.opts.HistoryCacheTTL = choices[(i+d+len(choices))%len(choices)]
		if m.opts.HistoryCacheTTL > 0 {
			m.cache = newHistoryCache(m.backend.Workspace(), m.opts.HistoryCacheTTL)
		}
	}
}

// clockName is the clock setting as the config writes it.
func clockName() string {
	if hour12 {
		return "12h"
	}
	return "24h"
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func cacheTTLName(d time.Duration) string {
	switch d {
	case 7 * 24 * time.Hour:
		return "7 days"
	case 30 * 24 * time.Hour:
		return "30 days"
	case 60 * 24 * time.Hour:
		return "60 days"
	}
	return d.String()
}

// slider is a bar of w cells filled for v in lo..hi.
func slider(v, lo, hi, w int) string {
	f := w
	if hi > lo {
		f = max(0, min(w, (v-lo)*w/(hi-lo)))
	}
	return keyStyle.Render(strings.Repeat("█", f)) + ruleStyle.Render(strings.Repeat("░", w-f))
}

func (m *model) overlaySettings(rows []string) {
	w := min(64, m.width-4)
	if w < 34 {
		return
	}
	inner := w - 4
	layout := m.layoutName
	if layout == "" {
		layout = theme.Layout
	}
	images := onOff(m.imgs.on) + " (" + m.imgs.mode.String() + ")"
	if m.imgs.mode == img.Off {
		images = "off in the config"
	}
	emoji := onOff(m.imgs.emoji)
	if m.imgs.mode != img.Kitty {
		emoji += dimStyle.Render(" (needs kitty)")
	}
	maxSide := m.width - 1 - minMain
	type row struct{ label, value string }
	items := []row{
		{"Theme", "‹ " + m.themeName + " ›"},
		{"Layout", "‹ " + layout + " ›"},
		{"Clock", "‹ " + clockName() + dimStyle.Render(" · "+hourOf(m.now())) + " ›"},
		{"Images", "‹ " + images + " ›"},
		{"Custom emoji", "‹ " + emoji + " ›"},
		{"Read on enter", "‹ " + onOff(m.readOnEnter) + " ›"},
		{"Composer", slider(m.inputRows, 1, max(2, m.contentRows()/2), 16) + fmt.Sprintf(" %d rows", m.inputRows)},
		{"Sidebar", slider(m.side, minSide, max(minSide+1, maxSide), 16) + fmt.Sprintf(" %d cols", m.side)},
		{"History cache", "‹ " + onOff(m.opts.HistoryCacheTTL > 0) + " ›"},
		{"Cache TTL", "‹ " + cacheTTLName(m.opts.HistoryCacheTTL) + " ›"},
		{"Keybindings", dimStyle.Render("enter ›")},
		{"Config file", dimStyle.Render("enter ›")},
	}
	body := []string{dimStyle.Render("changes show at once and last this session"), ""}
	for i, it := range items {
		mark, label := "  ", textStyle.Render(it.label)
		if i == m.settings.cursor {
			mark, label = modalMark.Render("▌")+" ", modalSel.Render(it.label)
		}
		if i == setKeys {
			body = append(body, "")
		}
		body = append(body, mark+fit(label, 16)+it.value)
	}
	k := m.km.first
	keep := fmt.Sprintf(`"theme": %q, "layout": %q, "clock": %q, "images": %q, "read_on_enter": %t, "composer_height": %d, "sidebar_width": %d`,
		m.themeName, layout, clockName(), map[bool]string{true: cmpOr(m.opts.Images, "auto"), false: "off"}[m.imgs.on], m.readOnEnter, m.inputRows, m.side)
	keep += fmt.Sprintf(`, "history_cache_ttl": %d`, int(m.opts.HistoryCacheTTL/time.Minute))
	body = append(body, "", dimStyle.Render("to keep it, in "+m.opts.ConfigPath+":"))
	for _, l := range wrapPlain(keep, inner) {
		body = append(body, keyStyle.Render(l))
	}
	box := modal("Settings", body, hints([][2]string{{k(actDown) + "/" + k(actUp), "move"}, {k(actFocusLeft) + "/" + k(actFocusRight), "change"}, {k(actOpen), "open"}, {k(actBack), "close"}}), w)
	m.float(rows, box, (m.width-w)/2, max(0, (m.height-1-len(box))/2))
}

// wrapPlain breaks s at ", " to lines of at most w columns.
func wrapPlain(s string, w int) []string {
	var out []string
	line := ""
	for i, part := range strings.Split(s, ", ") {
		if i > 0 {
			part = ", " + part
		}
		if line != "" && lipgloss.Width(line+part) > w {
			out = append(out, line+",")
			line = strings.TrimPrefix(part, ", ")
			continue
		}
		line += part
	}
	return append(out, line)
}
