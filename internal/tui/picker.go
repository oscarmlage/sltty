package tui

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/emoji"
)

// A picker is a floating list with a filter, telescope style: it opens
// typing in the filter; normal_mode switches to the list, where the NORMAL
// keys move (j/k, gg, G), open picks, insert goes back to the filter and
// back closes. The quick switcher, the message search and the reaction
// picker are pickers.

type pickKind int

const (
	pickSwitch pickKind = iota
	pickSearch
	pickReact
	pickTheme
	pickStatus
	pickEmoji
)

type pickItem struct {
	label   string
	right   string      // muted, flush right: unread count, time
	conv    int         // pickSwitch
	hit     chat.Hit    // pickSearch
	emoji   string      // pickReact
	theme   string      // pickTheme
	minutes int         // pickStatus: -1 active, -2 away, 0 ends DND
	person  chat.Person // pickSwitch: start a new DM
}

type picker struct {
	kind    pickKind
	title   string
	input   textinput.Model
	typing  bool
	items   []pickItem
	cursor  int
	first   int // first item on screen
	target  chat.Message
	convID  string          // pickReact: the conversation of target
	was     string          // pickTheme: the theme to go back to
	insert  *textarea.Model // pickEmoji: composer to insert into
	waiting bool            // pickSearch: a query is on its way
	here    string          // pickSearch: the conversation it was opened in
	scope   string          // pickSearch: here, or "" for everywhere
	people  []chat.Person
	peopleQ string
}

// found is a picked search: its results, the one shown and the words.
type found struct {
	query string
	words []string
	hits  []chat.Hit
	i     int
}

const (
	pickerRows     = 10
	searchDebounce = 250 * time.Millisecond
)

type (
	dmLookupTick struct{ query string }
	dmPeopleMsg  struct {
		query  string
		people []chat.Person
	}
	dmOpenedMsg struct {
		conv chat.Conversation
		err  error
	}
	searchTick struct{ query string }
	searchMsg  struct {
		query string
		hits  []chat.Hit
		err   error
	}
)

// reactions are the picker's emoji and the names that find them. Each is a
// single wide character, so it measures the same in every terminal.
var reactions = []struct{ emoji, names string }{
	{"👍", "+1 thumbsup yes"}, {"🙏", "pray thanks"}, {"😂", "joy laugh"}, {"🎉", "tada party"},
	{"👀", "eyes looking"}, {"🚀", "rocket ship"}, {"✅", "white_check_mark done"}, {"🔥", "fire"},
	{"👏", "clap"}, {"💯", "100 hundred"}, {"🤔", "thinking"}, {"😮", "open_mouth wow"},
	{"😢", "cry sad"}, {"☕", "coffee"}, {"💜", "purple_heart love"},
}

func (m *model) openPicker(kind pickKind, target *chat.Message) tea.Cmd {
	in := textinput.New()
	in.Prompt = "› "
	p := &picker{kind: kind, input: in, typing: true}
	var load tea.Cmd
	switch kind {
	case pickSwitch:
		p.title, in.Placeholder = "Switch conversation", "name · #channels · @people / start a DM"
	case pickSearch:
		p.title, in.Placeholder = "Search messages", "words · in:#channel · from:@person"
	case pickReact:
		whose := target.User + "'s"
		if target.User == "you" {
			whose = "your"
		}
		p.title, in.Placeholder = "React to "+whose+" message", m.reactHint()
		p.target, p.convID = *target, m.convID()
		load = m.loadCustomEmoji() // for their names
	case pickTheme:
		p.title, in.Placeholder, p.was = "Theme", "name", m.themeName
	case pickStatus:
		p.title, in.Placeholder = "Status", "type to filter…"
	case pickEmoji:
		p.title, in.Placeholder = "Insert emoji", "type an emoji name…"
		load = m.loadCustomEmoji()
	}
	p.input = in
	m.picker = p
	m.pending, m.lastInsert = "", ""
	m.refreshPicker()
	return tea.Batch(p.input.Focus(), load)
}

// closePicker closes without picking: a previewed theme goes back.
func (m *model) closePicker() {
	if p := m.picker; p != nil {
		if p.kind == pickTheme {
			m.setTheme(p.was, m.layoutName)
		}
		if p.insert != nil {
			m.mode = insert
			p.insert.Focus()
		}
	}
	m.picker = nil
}

// openEmoji pauses INSERT mode for a searchable picker, then resumes it at
// the same composer after choosing or cancelling.
func (m *model) openEmoji(in *textarea.Model) tea.Cmd {
	m.mode = normal
	cmd := m.openPicker(pickEmoji, nil)
	m.picker.insert = in
	return cmd
}

func (m *model) pickerTypingKey(msg tea.KeyPressMsg, k string) tea.Cmd {
	p := m.picker
	undo := func() { p.input, _ = p.input.Update(backspace()); m.refreshPicker() }
	if m.typedSequence(scInsert, k, actNormalMode, undo) || m.km.is(scInsert, k, actNormalMode) {
		m.lastInsert = ""
		p.typing = false
		p.input.Blur()
		return nil
	}
	if p.kind == pickSearch && p.scope != "" && k == "backspace" && p.input.Value() == "" {
		p.scope = "" // backspace on the empty filter drops the chip
		return m.refreshPicker()
	}
	switch m.km.lookup[scPicker][k] {
	case actPickScope:
		if p.kind != pickSearch {
			return nil
		}
		if p.here == "" {
			m.status = "no conversation open to search in"
			return nil
		}
		if p.scope == "" {
			p.scope = p.here
		} else {
			p.scope = ""
		}
		return m.refreshPicker()
	case actPickDown:
		m.pickerMove(1)
		return nil
	case actPickUp:
		m.pickerMove(-1)
		return nil
	case actPickSelect:
		return m.pick()
	}
	before := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != before {
		cmd = tea.Batch(cmd, m.refreshPicker())
	}
	return cmd
}

func (m *model) pickerListAction(a action) tea.Cmd {
	p := m.picker
	switch a {
	case actDown:
		m.pickerMove(1)
	case actUp:
		m.pickerMove(-1)
	case actPageDown:
		m.pickerMove(pageMsgs)
	case actPageUp:
		m.pickerMove(-pageMsgs)
	case actTop:
		m.pickerMove(-len(p.items))
	case actBottom:
		m.pickerMove(len(p.items))
	case actOpen:
		return m.pick()
	case actInsert:
		p.typing = true
		return p.input.Focus()
	case actBack, actQuit, actBackOrQuit:
		m.closePicker()
	}
	return nil
}

func (m *model) pickerMove(n int) {
	p := m.picker
	p.cursor = max(0, min(p.cursor+n, len(p.items)-1))
	if p.cursor < p.first {
		p.first = p.cursor
	}
	if p.cursor >= p.first+pickerRows {
		p.first = p.cursor - pickerRows + 1
	}
	m.previewTheme()
}

// previewTheme shows the highlighted theme while the theme picker is open.
func (m *model) previewTheme() {
	if p := m.picker; p != nil && p.kind == pickTheme && p.cursor < len(p.items) {
		m.setTheme(p.items[p.cursor].theme, m.layoutName)
	}
}

// refreshPicker filters the items again after the query changed. A search
// waits until the typing stops.
func (m *model) refreshPicker() tea.Cmd {
	p := m.picker
	q := strings.ToLower(strings.TrimSpace(p.input.Value()))
	p.cursor, p.first = 0, 0
	switch p.kind {
	case pickSwitch:
		p.items = nil
		for _, v := range []struct {
			name string
			row  int
		}{{"💬 Threads", rowThreads}, {"🔔 Activity", rowActivity}} {
			if v.row == rowThreads && m.noThreads {
				continue
			}
			if q != "" && strings.HasPrefix(strings.ToLower(v.name[strings.Index(v.name, " ")+1:]), q) {
				p.items = append(p.items, pickItem{label: v.name, conv: v.row})
			}
		}
		// "#" keeps the channels, "@" the people.
		only := -1
		switch {
		case strings.HasPrefix(q, "#"):
			only, q = int(chat.Channel), q[1:]
		case strings.HasPrefix(q, "@"):
			only, q = int(chat.DM), q[1:]
		}
		for _, i := range m.switchRank(q) {
			c := m.convs[i]
			if only >= 0 && int(c.Kind) != only {
				continue
			}
			it := pickItem{label: convLabel(c), conv: i}
			if c.Unread > 0 {
				it.right = fmt.Sprint(c.Unread)
			}
			p.items = append(p.items, it)
		}
		if only == int(chat.DM) && q != "" {
			for _, person := range p.people {
				if !strings.HasPrefix(strings.ToLower(p.peopleQ), q) || strings.EqualFold(person.Name, m.backend.Me()) {
					continue
				}
				p.items = append(p.items, pickItem{label: "Start chat with " + person.Full, person: person})
			}
			if _, ok := m.backend.(chat.DMOpener); ok {
				return tea.Tick(searchDebounce, func(time.Time) tea.Msg { return dmLookupTick{q} })
			}
		}
	case pickReact:
		p.items = m.reactItems(q)
	case pickTheme:
		p.items = nil
		for _, i := range rank(len(m.themes.names), func(i int) string { return m.themes.names[i] }, q) {
			n := m.themes.names[i]
			t, _ := m.themes.get(n, "")
			p.items = append(p.items, pickItem{label: n + dimStyle.Render("  "+t.Layout), theme: n})
			if q == "" && n == p.was {
				p.cursor = len(p.items) - 1
			}
		}
		p.first = max(0, p.cursor-pickerRows+1)
		m.previewTheme()
	case pickEmoji:
		p.items = m.emojiItems(q)
	case pickStatus:
		choices := []struct {
			label   string
			minutes int
		}{
			{"Active", -1}, {"Away", -2}, {"Snooze for 20 minutes", 20},
			{"Snooze for 1 hour", 60}, {"Snooze for 2 hours", 120}, {"Snooze for 4 hours", 240},
			{"Snooze for 8 hours", 480}, {"Snooze for 24 hours", 1440}, {"End snooze / DND", 0},
		}
		for _, i := range rank(len(choices), func(i int) string { return choices[i].label }, q) {
			c := choices[i]
			p.items = append(p.items, pickItem{label: c.label, minutes: c.minutes})
		}
	case pickSearch:
		if q == "" {
			p.items, p.waiting = nil, false
			return nil
		}
		p.waiting = true
		return tea.Tick(searchDebounce, func(time.Time) tea.Msg { return searchTick{q} })
	}
	return nil
}

// switchRank filters the conversations as the switcher always has, then
// orders equal matches by the user's local most-recently-opened list.
func (m *model) switchRank(q string) []int {
	out := rank(len(m.convs), func(i int) string { return m.convs[i].Name }, q)
	at := make(map[string]int, len(m.recent))
	for i, id := range m.recent {
		at[id] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, aok := at[m.convs[out[i]].ID]
		b, bok := at[m.convs[out[j]].ID]
		if aok != bok {
			return aok
		}
		return a < b
	})
	return out
}

// maxReactItems caps the reactions a few letters find.
const maxReactItems = 200

// reactItems is what the reaction picker offers for q: the usual few, and
// once something is typed, every emoji Slack knows and the workspace's own,
// as in slk. The usual come first among equal matches.
func (m *model) emojiItems(q string) []pickItem {
	var items []pickItem
	for _, n := range slices.Sorted(maps.Keys(m.imgs.custom)) {
		if q == "" || strings.Contains(n, q) || subsequence(q, n) {
			items = append(items, pickItem{label: ":" + n + ":  " + n, emoji: ":" + n + ":"})
		}
	}
	for _, n := range emoji.Names() {
		if q != "" && !strings.Contains(n, q) && !subsequence(q, n) {
			continue
		}
		g, _ := emoji.Glyph(n)
		items = append(items, pickItem{label: g + "  " + n, emoji: g})
		if len(items) == maxReactItems {
			break
		}
	}
	return items
}

func (m *model) reactItems(q string) []pickItem {
	type choice struct{ emoji, names string }
	cs := make([]choice, 0, len(reactions))
	for _, r := range reactions {
		cs = append(cs, choice{r.emoji, r.names})
	}
	if q != "" {
		for _, n := range emoji.Names() {
			g, _ := emoji.Glyph(n)
			cs = append(cs, choice{g, n})
		}
		for _, n := range slices.Sorted(maps.Keys(m.imgs.custom)) {
			cs = append(cs, choice{":" + n + ":", n})
		}
	}
	var items []pickItem
	seen := map[string]bool{}
	for _, i := range rank(len(cs), func(i int) string { return cs[i].names }, q) {
		c := cs[i]
		if seen[c.emoji] {
			continue // an alias of one already there
		}
		seen[c.emoji] = true
		label := strings.Fields(c.names)[0]
		if !strings.HasPrefix(c.emoji, ":") {
			label = c.emoji + "  " + label
		}
		if slices.ContainsFunc(m.picker.target.Reactions, func(x chat.Reaction) bool { return x.Emoji == c.emoji && x.Mine }) {
			label += dimStyle.Render("  (yours: picking removes it)")
		}
		items = append(items, pickItem{label: label, emoji: c.emoji})
		if len(items) == maxReactItems {
			break
		}
	}
	return items
}

// rank orders n names against q: those starting with it, then containing
// it, then holding its letters in order.
func rank(n int, name func(int) string, q string) []int {
	var prefix, sub, fuzzy []int
	for i := range n {
		s := strings.ToLower(name(i))
		switch {
		case strings.HasPrefix(s, q):
			prefix = append(prefix, i)
		case strings.Contains(s, q):
			sub = append(sub, i)
		case subsequence(q, s):
			fuzzy = append(fuzzy, i)
		}
	}
	return append(append(prefix, sub...), fuzzy...)
}

func subsequence(q, s string) bool {
	for _, r := range q {
		i := strings.IndexRune(s, r)
		if i < 0 {
			return false
		}
		s = s[i+len(string(r)):]
	}
	return true
}

// pick acts on the highlighted item and closes the picker.
func (m *model) pick() tea.Cmd {
	p := m.picker
	if p.cursor >= len(p.items) {
		return nil
	}
	it := p.items[p.cursor]
	m.picker = nil
	switch p.kind {
	case pickTheme:
		m.setTheme(it.theme, m.layoutName)
		m.status = `theme ` + it.theme + ` · "theme": "` + it.theme + `" in the config keeps it`
	case pickSwitch:
		if it.person.ID != "" {
			opener := m.backend.(chat.DMOpener)
			return m.call(func(ctx context.Context) tea.Msg {
				c, err := opener.OpenDM(ctx, it.person.ID)
				return dmOpenedMsg{c, err}
			})
		}
		var cmd tea.Cmd
		switch it.conv {
		case rowThreads:
			cmd = m.openThreadsView()
		case rowActivity:
			cmd = m.openActivityView()
		default:
			cmd = m.selectConv(it.conv)
		}
		m.focus = paneChat
		return cmd
	case pickReact:
		id, ts := p.convID, p.target.TS
		return m.call(func(ctx context.Context) tea.Msg {
			msg, err := m.backend.React(ctx, id, ts, it.emoji)
			return changedMsg{id, msg, err}
		})
	case pickEmoji:
		if p.insert != nil {
			p.insert.InsertString(it.emoji)
			m.mode = insert
			return p.insert.Focus()
		}
	case pickStatus:
		switch {
		case it.minutes == -1:
			return m.setPresence(false)
		case it.minutes == -2:
			return m.setPresence(true)
		default:
			return m.setSnooze(it.minutes)
		}
	case pickSearch:
		q := strings.TrimSpace(p.input.Value())
		f := &found{query: q, words: chat.ParseQuery(q).Words, i: p.cursor}
		for _, x := range p.items {
			f.hits = append(f.hits, x.hit)
		}
		m.found = f
		return m.jumpToHit(it.hit)
	}
	return nil
}

// lookupDMPeople finds a person outside the already-open DM list.
func (m *model) lookupDMPeople(query string) tea.Cmd {
	p := m.picker
	if p == nil || p.kind != pickSwitch || p.peopleQ == query {
		return nil
	}
	mn, ok := m.backend.(chat.Mentioner)
	if !ok {
		return nil
	}
	return m.call(func(ctx context.Context) tea.Msg {
		people, _ := mn.People(ctx, "", query)
		return dmPeopleMsg{query, people}
	})
}
