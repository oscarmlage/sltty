package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Every key binding of the app in one table: the dispatch, the status line
// hints and the help panel read it, and the config file overrides it, so
// they never drift apart.

type scope int

const (
	scNormal scope = iota // NORMAL mode, panes and the lists of the pickers
	scInsert              // typing in a composer
	scPicker              // typing in a picker's filter
)

type action string

const (
	actQuit        action = "quit"
	actHelp        action = "help"
	actSwitcher    action = "switcher"
	actSearch      action = "search"
	actFilter      action = "filter"
	actNextMatch   action = "next_match"
	actPrevMatch   action = "prev_match"
	actPickScope   action = "search_scope"
	actImages      action = "images_toggle"
	actCommand     action = "command"
	actSettings    action = "settings"
	actStatus      action = "status"
	actInsert      action = "insert"
	actFocusLeft   action = "focus_left"
	actFocusRight  action = "focus_right"
	actToggleFocus action = "toggle_sidebar_chat"
	actDown        action = "down"
	actUp          action = "up"
	actPageDown    action = "page_down"
	actPageUp      action = "page_up"
	actTop         action = "top"
	actBottom      action = "bottom"
	actNextChat    action = "next_chat"
	actPrevChat    action = "prev_chat"
	actOpen        action = "open"
	actReply       action = "reply"
	actReact       action = "react"
	actReactions   action = "reactions"
	actEdit        action = "edit"
	actBack        action = "back"
	actDivLeft     action = "divider_left"
	actDivRight    action = "divider_right"
	actDivReset    action = "divider_reset"
	actSend        action = "send"
	actNewline     action = "newline"
	actNormalMode  action = "normal_mode"
	actEditLast    action = "edit_last"
	actPickDown    action = "picker_down"
	actPickUp      action = "picker_up"
	actPickSelect  action = "picker_select"
	actTheme       action = "theme"
	actLayout      action = "layout"
	actBackOrQuit  action = "back_or_quit"
	actGrow        action = "composer_taller"
	actShrink      action = "composer_shorter"
	actFold        action = "toggle_section"
	actToggleRead  action = "toggle_read"
	actView        action = "view_picture"
	actLinks       action = "links"
	actNextTab     action = "next_account"
	actPrevTab     action = "previous_account"
)

type binding struct {
	act   action
	scope scope
	group string
	help  string
	keys  []string // "g g" is g then g
}

// defaults, in the help panel's order.
var defaults = []binding{
	{actFocusLeft, scNormal, "Move around", "pane on the left", []string{"h", "left", "shift+tab"}},
	{actFocusRight, scNormal, "Move around", "pane on the right", []string{"l", "right"}},
	{actToggleFocus, scNormal, "Move around", "sidebar ⇄ chat", []string{"tab"}},
	{actDown, scNormal, "Move around", "down", []string{"j", "down"}},
	{actUp, scNormal, "Move around", "up", []string{"k", "up"}},
	{actPageDown, scNormal, "Move around", "five down", []string{"ctrl+d"}},
	{actPageUp, scNormal, "Move around", "five up", []string{"ctrl+u"}},
	{actTop, scNormal, "Move around", "first", []string{"g g", "home"}},
	{actBottom, scNormal, "Move around", "last", []string{"G", "end"}},
	{actNextChat, scNormal, "Move around", "next chat, from any pane", []string{"J"}},
	{actPrevChat, scNormal, "Move around", "previous chat", []string{"K"}},
	{actSwitcher, scNormal, "Move around", "jump to a conversation", []string{"ctrl+k", "ctrl+p"}},
	{actSearch, scNormal, "Move around", "search messages", []string{"ctrl+f"}},
	{actFilter, scNormal, "Move around", "find: chats in the sidebar, messages here elsewhere", []string{"/"}},
	{actNextMatch, scNormal, "Move around", "next search result (older)", []string{"n"}},
	{actPrevMatch, scNormal, "Move around", "previous search result", []string{"N"}},
	{actFold, scNormal, "Move around", "fold / unfold the sidebar section", []string{"space", "z a"}},

	{actOpen, scNormal, "Messages", "open the chat / the thread", []string{"enter", "t"}},
	{actReply, scNormal, "Messages", "reply in the thread", nil},
	{actReact, scNormal, "Messages", "add or remove a reaction", []string{"r"}},
	{actReactions, scNormal, "Messages", "show who reacted", []string{"R"}},
	{actEdit, scNormal, "Messages", "edit your message", []string{"e"}},
	{actToggleRead, scNormal, "Messages", "mark read / unread (from here)", []string{"u"}},
	{actLinks, scNormal, "Messages", "list the message's links; a key opens one (a s d f… or 1-9)", []string{"o"}},
	{actView, scNormal, "Messages", "the message's picture, full size (or a click on it)", []string{"v"}},
	{actInsert, scNormal, "Messages", "write (INSERT mode)", []string{"i", "a"}},
	{actBack, scNormal, "Messages", "back, close", []string{"esc", "x"}},

	{actSend, scInsert, "INSERT mode", "send / save the edit", []string{"enter"}},
	{actNewline, scInsert, "INSERT mode", "new line", []string{"shift+enter", "alt+enter", "ctrl+j"}},
	{actNormalMode, scInsert, "INSERT mode", "back to NORMAL", []string{"esc", "ctrl+c", "j k"}},
	{actEditLast, scInsert, "INSERT mode", "edit your last message", []string{"up"}},

	{actPickDown, scPicker, "Lists (ctrl+k, ctrl+f, r, T)", "next while filtering", []string{"ctrl+n", "ctrl+j", "down"}},
	{actPickUp, scPicker, "Lists (ctrl+k, ctrl+f, r, T)", "previous while filtering", []string{"ctrl+p", "ctrl+k", "up"}},
	{actPickSelect, scPicker, "Lists (ctrl+k, ctrl+f, r, T)", "pick", []string{"enter"}},
	{actPickScope, scPicker, "Lists (ctrl+k, ctrl+f, r, T)", "search here ⇄ everywhere", []string{"tab"}},

	{actDivLeft, scNormal, "Layout", "move the divider left", []string{"<"}},
	{actDivRight, scNormal, "Layout", "move the divider right", []string{">"}},
	{actDivReset, scNormal, "Layout", "reset the widths", []string{"="}},
	{actTheme, scNormal, "Layout", "pick a theme", []string{"T"}},
	{actLayout, scNormal, "Layout", "slack / panels layout", []string{"L"}},
	{actImages, scNormal, "Layout", "images and custom emoji on / off", []string{"I"}},
	{actHelp, scNormal, "Layout", "this help", []string{"?"}},
	{actSettings, scNormal, "Layout", "settings: theme, layout, clock, images, sizes", []string{";"}},
	{actStatus, scNormal, "Layout", "set active, away, or Do Not Disturb", []string{"ctrl+s"}},
	{actCommand, scNormal, "Layout", "command line (:help, :find, :version…)", []string{":"}},
	{actGrow, scNormal, "Layout", "taller composer", []string{"+"}},
	{actShrink, scNormal, "Layout", "shorter composer", []string{"-"}},
	{actBackOrQuit, scNormal, "Layout", "back step by step, then quit", []string{"q", "ctrl+c"}},
	{actQuit, scNormal, "Layout", "quit from anywhere (asks first)", []string{}},

	{actNextTab, scNormal, "Accounts", "next account tab", []string{"g t"}},
	{actPrevTab, scNormal, "Accounts", "previous account tab", []string{"g T"}},
}

// actAccount is the action that opens tab n (1-9): account_1…account_9.
func actAccount(n int) action { return action(fmt.Sprintf("account_%d", n)) }

// accountOf is the tab an action opens, or 0.
func accountOf(a action) int {
	var n int
	if _, err := fmt.Sscanf(string(a), "account_%d", &n); err != nil || n < 1 || n > 9 {
		return 0
	}
	return n
}

func init() {
	// One action per tab: n in NORMAL mode, alt+n anywhere (no text is
	// typed with alt).
	for n := 1; n <= 9; n++ {
		defaults = append(defaults, binding{actAccount(n), scNormal, "Accounts",
			fmt.Sprintf("account tab %d", n), []string{fmt.Sprint(n), fmt.Sprintf("alt+%d", n)}})
	}
}

// keymap resolves keys to actions, per scope.
type keymap struct {
	keys   map[action][]string
	lookup [3]map[string]action // scope → key or "k1 k2" → action
	prefix [3]map[string]bool   // first keys of two-key bindings
}

func defaultKeymap() *keymap {
	km, err := newKeymap(nil)
	if err != nil {
		panic(err) // the defaults are wrong: a test catches it
	}
	return km
}

// newKeymap is the defaults with overrides applied: each overridden action
// gets exactly the keys given. It rejects unknown actions, empty or
// malformed keys, and one key bound to two actions of the same scope.
func newKeymap(overrides map[string][]string) (*keymap, error) {
	km := &keymap{keys: map[action][]string{}}
	known := map[action]binding{}
	for _, b := range defaults {
		known[b.act] = b
		km.keys[b.act] = b.keys
	}
	names := make([]string, 0, len(overrides))
	for n := range overrides {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, ok := known[action(n)]; !ok {
			return nil, fmt.Errorf("keys: unknown action %q (run `sltty keys` for the list)", n)
		}
		for _, k := range overrides[n] {
			if f := strings.Fields(k); len(f) == 0 || len(f) > 2 {
				return nil, fmt.Errorf("keys: %s: %q must be one key or two separated by a space", n, k)
			}
		}
		km.keys[action(n)] = overrides[n]
	}
	for s := range km.lookup {
		km.lookup[s], km.prefix[s] = map[string]action{}, map[string]bool{}
	}
	for _, b := range defaults {
		for _, k := range km.keys[b.act] {
			k = strings.Join(strings.Fields(k), " ")
			if other, dup := km.lookup[b.scope][k]; dup {
				return nil, fmt.Errorf("keys: %q is bound to both %s and %s", k, other, b.act)
			}
			km.lookup[b.scope][k] = b.act
			if first, _, two := strings.Cut(k, " "); two {
				km.prefix[b.scope][first] = true
			}
		}
	}
	for s := range km.lookup {
		for k, a := range km.lookup[s] {
			if km.prefix[s][k] {
				return nil, fmt.Errorf("keys: %q (%s) is also the start of a two-key binding", k, a)
			}
		}
	}
	return km, nil
}

func (km *keymap) is(s scope, key string, a action) bool { return km.lookup[s][key] == a }

// first is the key the status line advertises for an action.
func (km *keymap) first(a action) string {
	if ks := km.keys[a]; len(ks) > 0 {
		return ks[0]
	}
	return ""
}

// DefaultsJSON is a config file with every default binding, for
// `sltty keys`.
func DefaultsJSON() string {
	keys := map[string][]string{}
	for _, b := range defaults {
		keys[string(b.act)] = b.keys
	}
	out, _ := json.MarshalIndent(map[string]any{"keys": keys}, "", "  ")
	return string(out)
}

// helpGroups lists the bindings by help group, in table order.
func (km *keymap) helpGroups() (groups []string, rows map[string][]binding) {
	rows = map[string][]binding{}
	for _, b := range defaults {
		if !slices.Contains(groups, b.group) {
			groups = append(groups, b.group)
		}
		b.keys = km.keys[b.act]
		rows[b.group] = append(rows[b.group], b)
	}
	return groups, rows
}
