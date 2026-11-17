package tui

// Mentions while writing: "@" and a few letters before the cursor open a
// list of people above the composer, the open conversation's first (those
// of its history at once, the service's members and directory as they
// arrive). tab or enter picks one, ctrl+n / ctrl+p (or the arrows) move,
// esc closes the list. A picked mention reads @name in the composer and is
// sent as <@ID>, which each backend makes a real mention of.

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/oscarmlage/sltty/internal/chat"
)

const (
	maxSuggest   = 6
	suggestDelay = 200 * time.Millisecond // asks the service once typing rests
)

// suggest is the open list of people to mention.
type suggest struct {
	key    string // the composer's attachKey
	conv   string
	query  string // what follows the @
	local  []chat.Person
	remote []chat.Person // the service's answer for remoteQ
	remQ   string
	items  []chat.Person
	sel    int
}

type (
	suggestTick struct{ key, conv, query string }
	peopleMsg   struct {
		key, query string
		people     []chat.Person
	}
)

// mentionMarkup is a mention in a message's markup: <@ID>, or Slack's
// <@ID|name>.
var mentionMarkup = regexp.MustCompile(`<@([A-Za-z0-9]+)(?:\|[^>]*)?>`)

// mentionRef is a mention picked into a composer: its text there and whom.
type mentionRef struct{ text, id string }

// mentionQuery is the "@word" being typed at the cursor: the word without
// the @ and its length in runes with it.
func mentionQuery(in *textarea.Model) (string, int, bool) {
	lines := strings.Split(in.Value(), "\n")
	row := in.Line()
	if row >= len(lines) {
		return "", 0, false
	}
	line := []rune(lines[row])
	col := min(in.Column(), len(line))
	start := col
	for start > 0 && !unicode.IsSpace(line[start-1]) {
		start--
	}
	word := string(line[start:col])
	if !strings.HasPrefix(word, "@") || strings.Count(word, "@") > 1 {
		return "", 0, false
	}
	return word[1:], col - start, true
}

// conversationPeople are the authors of the composer's conversation (or
// thread), newest first: the likeliest to mention, known at once.
func (m *model) conversationPeople(in *textarea.Model) []chat.Person {
	msgs := m.history[m.convID()]
	if t := m.thread; in == &m.threadIn && t != nil {
		msgs = append(slices.Clone(msgs), t.msgs...)
	}
	me := m.backend.Me()
	seen := map[string]bool{}
	var out []chat.Person
	for i := len(msgs) - 1; i >= 0; i-- {
		x := msgs[i]
		// Bots (Slack's B…) and Telegram's channels and groups (c…, g…)
		// are no one to mention.
		if x.UserID == "" || x.User == me || seen[x.UserID] || strings.ContainsAny(x.UserID[:1], "Bcg") {
			continue
		}
		seen[x.UserID] = true
		name := x.User
		if mn, ok := m.backend.(chat.Mentioner); ok {
			if n, ok := mn.PersonName(x.UserID); ok && n != "" {
				name = n
			}
		}
		out = append(out, chat.Person{ID: x.UserID, Name: name, Full: x.User})
	}
	return out
}

// updateSuggest opens, refreshes or closes the list after each key typed.
func (m *model) updateSuggest(in *textarea.Model) tea.Cmd {
	if _, ok := m.backend.(chat.Mentioner); !ok {
		m.suggest = nil
		return nil
	}
	q, _, ok := mentionQuery(in)
	if !ok {
		m.suggest = nil
		return nil
	}
	key, conv := m.attachKey(in), m.convID()
	if s := m.suggest; s == nil || s.key != key {
		m.suggest = &suggest{key: key, conv: conv, local: m.conversationPeople(in)}
	}
	s := m.suggest
	if s.query != q {
		s.query, s.sel = q, 0
	}
	m.rankSuggest()
	return tea.Tick(suggestDelay, func(time.Time) tea.Msg { return suggestTick{key, conv, q} })
}

// rankSuggest lists the people matching the query: the conversation's
// first, then the service's.
func (m *model) rankSuggest() {
	s := m.suggest
	all := slices.Clone(s.local)
	for _, p := range s.remote {
		if !slices.ContainsFunc(all, func(x chat.Person) bool { return x.ID == p.ID }) {
			all = append(all, p)
		}
	}
	q := strings.ToLower(s.query)
	s.items = s.items[:0]
	for _, i := range rank(len(all), func(i int) string { return all[i].Name + " " + all[i].Full }, q) {
		s.items = append(s.items, all[i])
		if len(s.items) == maxSuggest {
			break
		}
	}
	s.sel = min(s.sel, max(0, len(s.items)-1))
}

// askPeople asks the service, once typing rested on the same query.
func (m *model) askPeople(t suggestTick) tea.Cmd {
	s := m.suggest
	if s == nil || s.key != t.key || s.query != t.query || s.remQ == t.query && s.remote != nil {
		return nil
	}
	mn := m.backend.(chat.Mentioner)
	return m.call(func(ctx context.Context) tea.Msg {
		p, _ := mn.People(ctx, t.conv, t.query)
		return peopleMsg{t.key, t.query, p}
	})
}

func (m *model) gotPeople(msg peopleMsg) {
	s := m.suggest
	if s == nil || s.key != msg.key || !strings.HasPrefix(strings.ToLower(s.query), strings.ToLower(msg.query)) {
		return
	}
	s.remote, s.remQ = msg.people, msg.query
	m.rankSuggest()
}

// suggestKey handles the list's keys; ok is false for the rest, which type.
func (m *model) suggestKey(in *textarea.Model, k string) (tea.Cmd, bool) {
	s := m.suggest
	if s == nil || len(s.items) == 0 {
		if s != nil && (k == "esc" || m.km.is(scInsert, k, actNormalMode)) {
			m.suggest = nil
			return nil, true
		}
		return nil, false
	}
	switch k {
	case "tab", "enter":
		m.pickMention(in, s.items[s.sel])
	case "down", "ctrl+n":
		s.sel = (s.sel + 1) % len(s.items)
	case "up", "ctrl+p":
		s.sel = (s.sel - 1 + len(s.items)) % len(s.items)
	case "esc":
		m.suggest = nil
	default:
		return nil, false
	}
	return nil, true
}

// pickMention replaces the "@word" at the cursor with the person's mention.
func (m *model) pickMention(in *textarea.Model, p chat.Person) {
	_, n, ok := mentionQuery(in)
	if !ok {
		m.suggest = nil
		return
	}
	for range n {
		*in, _ = in.Update(backspace())
	}
	text := "@" + p.Name
	in.InsertString(text + " ")
	key := m.attachKey(in)
	refs := slices.DeleteFunc(m.mentions[key], func(r mentionRef) bool { return r.text == text })
	m.mentions[key] = append(refs, mentionRef{text, p.ID})
	m.suggest = nil
}

// encodeMentions writes the mentions picked into composer key as <@ID>.
func (m *model) encodeMentions(key, text string) string {
	refs := slices.Clone(m.mentions[key])
	delete(m.mentions, key)
	// The longest first: "@ana maría" before "@ana".
	slices.SortFunc(refs, func(a, b mentionRef) int { return len(b.text) - len(a.text) })
	for _, r := range refs {
		text = replaceMention(text, r.text, "<@"+r.id+">")
	}
	return text
}

// replaceMention replaces mention in text where it is a whole word (not
// "@ana" inside "@anabel").
func replaceMention(text, mention, with string) string {
	var b strings.Builder
	for {
		i := strings.Index(text, mention)
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		end := i + len(mention)
		b.WriteString(text[:i])
		if r := []rune(text[end:]); len(r) == 0 || !unicode.IsLetter(r[0]) && !unicode.IsDigit(r[0]) && r[0] != '_' && r[0] != '.' {
			b.WriteString(with)
		} else {
			b.WriteString(mention)
		}
		text = text[end:]
	}
}

// decodeMentions is the other way, for an edit: <@ID> (and Slack's
// <@ID|name>) read @name again, remembered for when it is saved.
func (m *model) decodeMentions(key, raw string) string {
	mn, ok := m.backend.(chat.Mentioner)
	if !ok || !strings.Contains(raw, "<@") {
		return raw
	}
	return mentionMarkup.ReplaceAllStringFunc(raw, func(tok string) string {
		id := mentionMarkup.FindStringSubmatch(tok)[1]
		name, ok := mn.PersonName(id)
		if !ok || name == "" {
			return tok
		}
		text := "@" + name
		m.mentions[key] = append(m.mentions[key], mentionRef{text, id})
		return text
	})
}

// suggestLines is the list, to draw above the composer w columns wide.
func (m *model) suggestLines(in *textarea.Model, w int) []string {
	s := m.suggest
	if s == nil || m.mode != insert || in != m.activeInput() || s.key != m.attachKey(in) {
		return nil
	}
	head := " " + dimStyle.Render("mention · tab pick · ctrl+n/p move · esc close")
	if len(s.items) == 0 {
		msg := "no one matches"
		if s.query == "" {
			msg = "type a name"
		}
		return []string{paint(head, w, surfaceBg), paint(" "+modalEmpty.Render(msg), w, surfaceBg)}
	}
	out := []string{paint(head, w, surfaceBg)}
	for i, p := range s.items {
		mark, name := "  ", textStyle.Render("@"+p.Name)
		if i == s.sel {
			mark, name = modalMark.Render("▌")+" ", modalSel.Render("@"+p.Name)
		}
		line := mark + name
		if p.Full != "" && !strings.EqualFold(p.Full, p.Name) {
			line += dimStyle.Render("  " + p.Full)
		}
		out = append(out, paint(line, w, surfaceBg))
	}
	return out
}
