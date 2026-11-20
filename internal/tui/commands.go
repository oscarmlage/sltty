package tui

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/oscarmlage/sltty/internal/chat"
)

// COMMAND mode, slk style: ":" opens a command line in the status bar;
// enter runs it, tab completes the command, up/down walk the ones run
// before, and the normal_mode keys (esc, ctrl+c, jk) leave it.

type command struct {
	name, args, help string
	run              func(m *model, arg string) tea.Cmd
}

// commands is set in init: they reach the help, which lists them.
var commands []command

func init() {
	commands = []command{
		{"help", "", "the keys and these commands", func(m *model, _ string) tea.Cmd {
			m.openKeys()
			return nil
		}},
		{"settings", "", "theme, layout, clock, images, sizes", func(m *model, _ string) tea.Cmd {
			m.openSettings()
			return nil
		}},
		{"config", "", "what the config file takes", func(m *model, _ string) tea.Cmd {
			m.openConfigRef()
			return nil
		}},
		{"log", "", "open application and connection logs", func(m *model, _ string) tea.Cmd {
			m.openLog()
			return nil
		}},
		{"save", "", "save local theme, sizes and collapsed sections", func(m *model, _ string) tea.Cmd {
			if err := m.saveUIState(); err != nil {
				m.err = "saving settings: " + err.Error()
			} else {
				m.status = "settings saved"
			}
			return nil
		}},
		{"savequit", "", "save local UI state and quit now", func(m *model, _ string) tea.Cmd {
			if err := m.saveUIState(); err != nil {
				m.err = "saving settings: " + err.Error()
				return nil
			}
			return tea.Quit
		}},
		{"attach", "[path]", "attach a file to the next message (none: the clipboard's picture)", func(m *model, arg string) tea.Cmd {
			if m.view != sideConv || m.current() == nil {
				m.err = "open a conversation first"
				return nil
			}
			if arg == "" {
				return m.pasteImage()
			}
			path, ok := pastedPath(arg)
			if !ok {
				m.err = "no such file: " + arg
				return nil
			}
			if err := m.attachFile(path); err != nil {
				m.err = err.Error()
			}
			return nil
		}},
		{"detach", "", "drop the file attached to the next message", func(m *model, _ string) tea.Cmd {
			m.detach()
			return nil
		}},
		{"version", "", "sltty's version", func(m *model, _ string) tea.Cmd {
			m.status = "sltty " + cmpOr(m.opts.Version, "dev") + " · " + m.backend.Workspace()
			return nil
		}},
		{"who", "", "list members of the open channel or group", func(m *model, _ string) tea.Cmd { return m.who() }},
		{"find", "[name]", "jump to a conversation (#channels, @people)", func(m *model, arg string) tea.Cmd {
			cmd := m.openPicker(pickSwitch, nil)
			if arg == "" {
				return cmd
			}
			m.picker.input.SetValue(arg)
			m.refreshPicker()
			if len(m.picker.items) == 1 { // one match: go
				return m.pick()
			}
			return cmd
		}},
		{"search", "[words]", "search messages everywhere (in:, from: work)", func(m *model, arg string) tea.Cmd {
			cmd := m.openSearch(false)
			if arg == "" {
				return cmd
			}
			m.picker.input.SetValue(arg)
			return tea.Batch(cmd, m.refreshPicker())
		}},
		{"theme", "[name]", "switch theme (no name: the picker)", func(m *model, arg string) tea.Cmd {
			if arg == "" {
				return m.openPicker(pickTheme, nil)
			}
			if _, err := m.themes.get(arg, ""); err != nil {
				m.err = err.Error()
				return nil
			}
			m.setTheme(arg, m.layoutName)
			m.status = "theme " + arg
			return nil
		}},
		{"layout", "[slack|panels]", "switch layout", func(m *model, arg string) tea.Cmd {
			switch arg {
			case "":
				return m.paneAction(actLayout)
			case layoutSlack, layoutPanels:
				m.setTheme(m.themeName, arg)
				m.status = "layout " + arg
			default:
				m.err = "layout: slack or panels"
			}
			return nil
		}},
		{"clock", "[12h|24h]", "times as 3:04 PM or 15:04", func(m *model, arg string) tea.Cmd {
			switch arg {
			case "":
				hour12 = !hour12
			case "12h", "24h":
				hour12 = arg == "12h"
			default:
				m.err = "clock: 12h or 24h"
				return nil
			}
			m.status = "clock " + clockName() + ` · "clock": "` + clockName() + `" in the config keeps it`
			return nil
		}},
		{"images", "[on|off]", "pictures and custom emoji", func(m *model, arg string) tea.Cmd {
			switch {
			case arg == "" || arg == "on" && !m.imgs.on || arg == "off" && m.imgs.on:
				m.toggleImages()
			case arg != "on" && arg != "off":
				m.err = "images: on or off"
			default:
				m.status = "images already " + arg
			}
			return nil
		}},
		{"status", "", "set active, away, or Do Not Disturb", func(m *model, _ string) tea.Cmd { return m.openStatus() }},
		{"away", "", "show yourself away", func(m *model, _ string) tea.Cmd { return m.setPresence(true) }},
		{"active", "", "show yourself active", func(m *model, _ string) tea.Cmd { return m.setPresence(false) }},
		{"read", "", "mark the conversation read", func(m *model, _ string) tea.Cmd {
			if c := m.current(); c != nil && c.Unread == 0 {
				m.status = "nothing unread here"
				return nil
			}
			return m.toggleRead()
		}},
		{"read_on_enter", "[on|off]", "mark conversations read when opening them", func(m *model, arg string) tea.Cmd {
			switch arg {
			case "":
				m.readOnEnter = !m.readOnEnter
			case "on", "yes":
				m.readOnEnter = true
			case "off", "no":
				m.readOnEnter = false
			default:
				m.err = "read_on_enter: on or off"
				return nil
			}
			m.status = "mark read on enter " + onOff(m.readOnEnter) + ` · "read_on_enter": ` + onOff(m.readOnEnter) + " in the config keeps it"
			return nil
		}},
		{"fold", "[section]", "fold / unfold a sidebar section", func(m *model, arg string) tea.Cmd {
			if arg == "" {
				m.toggleSection("")
				return nil
			}
			for _, s := range m.sections() {
				if strings.EqualFold(s.name, arg) {
					m.toggleSection(s.name)
					return nil
				}
			}
			m.err = "no section " + arg
			return nil
		}},
		{"quit", "", "leave (asks first); also :q", func(m *model, _ string) tea.Cmd {
			m.quitting = true
			return nil
		}},
	}
}

var aliases = map[string]string{"q": "quit", "w": "save", "wq": "savequit", "h": "help", "f": "find", "s": "search"}

func findCommand(name string) (command, bool) {
	name = strings.ToLower(name)
	if a, ok := aliases[name]; ok {
		name = a
	}
	i := slices.IndexFunc(commands, func(c command) bool { return c.name == name })
	if i < 0 {
		return command{}, false
	}
	return commands[i], true
}

func (m *model) openCommand() tea.Cmd {
	in := textinput.New()
	in.Prompt = ""
	in.SetWidth(max(10, m.width-40))
	m.cmdline = &in
	m.cmdHist = -1
	return m.cmdline.Focus()
}

func (m *model) commandKey(msg tea.KeyPressMsg, k string) tea.Cmd {
	in := m.cmdline
	undo := func() { *in, _ = in.Update(backspace()) }
	switch {
	case m.typedSequence(scInsert, k, actNormalMode, undo) || m.km.is(scInsert, k, actNormalMode):
		m.cmdline, m.lastInsert = nil, ""
		return nil
	case k == "backspace" && in.Value() == "":
		m.cmdline = nil // like vim: backspace on an empty line leaves
		return nil
	case k == "enter":
		line := strings.TrimSpace(in.Value())
		m.cmdline = nil
		return m.runCommand(line)
	case k == "tab":
		m.complete()
		return nil
	case k == "up" || k == "down":
		m.walkHistory(k == "up")
		return nil
	}
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	return cmd
}

// who loads the membership list for the open Slack channel or Telegram group.
func (m *model) who() tea.Cmd {
	c := m.current()
	members, ok := m.backend.(chat.ChannelMembers)
	if !ok {
		m.err = "who is available on Slack and Telegram groups"
		return nil
	}
	if c == nil || c.Kind != chat.Channel {
		m.err = "open a channel or group first"
		return nil
	}
	return m.call(func(ctx context.Context) tea.Msg {
		people, err := members.Members(ctx, c.ID)
		return membersMsg{id: c.ID, members: people, err: err}
	})
}

func (m *model) runCommand(line string) tea.Cmd {
	if line == "" {
		return nil
	}
	m.cmdHistory = append(slices.DeleteFunc(m.cmdHistory, func(s string) bool { return s == line }), line)
	name, arg, _ := strings.Cut(line, " ")
	c, ok := findCommand(name)
	if !ok {
		m.err = fmt.Sprintf("not a command: %s (:help lists them)", name)
		return nil
	}
	return c.run(m, strings.TrimSpace(arg))
}

// complete finishes the command name, or lists what it could be.
func (m *model) complete() {
	v := m.cmdline.Value()
	if strings.Contains(v, " ") {
		return
	}
	var match []string
	for _, c := range commands {
		if strings.HasPrefix(c.name, strings.ToLower(v)) {
			match = append(match, c.name)
		}
	}
	switch len(match) {
	case 0:
		m.status = "no command starts with " + v
	case 1:
		m.cmdline.SetValue(match[0] + " ")
		m.cmdline.CursorEnd()
	default:
		m.status = strings.Join(match, "  ")
	}
}

func (m *model) walkHistory(older bool) {
	n := len(m.cmdHistory)
	if n == 0 {
		return
	}
	switch {
	case older && m.cmdHist < 0:
		m.cmdHist = n - 1
	case older:
		m.cmdHist = max(0, m.cmdHist-1)
	case m.cmdHist >= 0:
		m.cmdHist++
	}
	if m.cmdHist < 0 || m.cmdHist >= n {
		m.cmdHist = -1
		m.cmdline.SetValue("")
		return
	}
	m.cmdline.SetValue(m.cmdHistory[m.cmdHist])
	m.cmdline.CursorEnd()
}

// Presence and connection, for the status bar.

type presenceMsg struct {
	away bool
	set  bool
	err  error
}

func (m *model) loadPresence() tea.Cmd {
	return m.call(func(ctx context.Context) tea.Msg {
		away, err := m.backend.Presence(ctx)
		return presenceMsg{away: away, err: err}
	})
}

func (m *model) setPresence(away bool) tea.Cmd {
	return m.call(func(ctx context.Context) tea.Msg {
		return presenceMsg{away: away, set: true, err: m.backend.SetPresence(ctx, away)}
	})
}

type snoozeMsg struct {
	minutes int
	err     error
}

func (m *model) setSnooze(minutes int) tea.Cmd {
	return m.call(func(ctx context.Context) tea.Msg {
		return snoozeMsg{minutes, m.backend.SetSnooze(ctx, minutes)}
	})
}

// noteConnection follows the backend replies: any error marks the
// connection down until the next reply that worked.
func (m *model) noteConnection(msg tea.Msg) {
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Struct {
		return
	}
	f := v.FieldByName("err")
	if !f.IsValid() || f.Type() != reflect.TypeFor[error]() {
		return
	}
	m.replied = true
	// Rate limited is still connected (the field is unexported: its type
	// is all reflection may look at).
	m.connected = f.IsNil() || f.Elem().Type() == reflect.TypeFor[*chat.WaitError]()
}

// indicators is "● Active ● Connected", slk's corner of the status bar.
func (m *model) indicators() string {
	dot := func(on bool, yes, no string, bad lipgloss.Style) string {
		if on {
			return onDot.Render("●") + " " + dimStyle.Render(yes)
		}
		return bad.Render("●") + " " + dimStyle.Render(no)
	}
	conn := dot(m.connected, "Connected", "Disconnected", errStyle)
	if !m.replied {
		conn = dimStyle.Render("○ Connecting…")
	}
	// Away is a deliberate, non-error state: give its dot the warning yellow
	// rather than the muted grey used for an unavailable value.
	presence := dot(!m.away, "Active", "Away", dragStyle)
	if m.snoozeTill.After(time.Now()) {
		presence = dimStyle.Render("◐ Snoozed")
	}
	return presence + "  " + conn + " "
}
