package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/emoji"
)

// block is where one message sits in a rendered list: lines [from, to),
// and where its pictures are.
type block struct {
	from, to int
	pics     []picSpot
}

// renderMsgs lays out msgs for a pane of width columns, Slack style: a day
// separator when the date changes, a header (name and time) for each run of
// messages by the same person within five minutes, then the text, the
// reactions and, outside threads, the thread summary. In a thread the root
// is followed by a "N replies" rule.
// newTS, when set, draws Slack's red New line above that message.
// art draws what is more than text: attachments, custom emoji, the words
// of the last search, avatars.
type art interface {
	fileLines(f chat.File, width int) (lines []string, picture bool)
	richText(line string, words []string) string
	reactionEmoji(e string) string
	avatars() bool
	avatarLines(userID string) []string
}

// words, when set, are marked in the text (the last search).
func renderMsgs(msgs []chat.Message, width int, inThread bool, me string, now time.Time, newTS string, words []string, a art) ([]string, []block) {
	inner := max(1, width-2) // one column of margin on each side
	// With pictures on, each message sits right of its author's avatar in
	// both the conversation and its thread. Threads are narrower, so use a
	// lower threshold there but still leave enough text width to read.
	gutter := 0
	if a.avatars() && inner > avatarCols+18 {
		gutter = avatarCols + 1
	}
	body := inner - gutter
	var lines []string
	var blocks []block
	var prev chat.Message
	for i, msg := range msgs {
		newDay := i == 0 || !sameDay(msg.At, prev.At)
		if newDay && !inThread {
			lines = append(lines, daySeparator(msg.At, now, width))
		}
		isNew := newTS != "" && msg.TS == newTS
		header := newDay || isNew || msg.User != prev.User || msg.At.Sub(prev.At) > 5*time.Minute || (inThread && i == 1)
		if inThread && i == 1 {
			lines = append(lines, repliesRule(len(msgs)-1, width))
		}
		if header && i > 0 && !newDay && !isNew && !(inThread && i == 1) {
			lines = append(lines, "")
		}
		if isNew {
			lines = append(lines, newLine(width))
		}
		from := len(lines)
		if header {
			us := nameStyle
			if msg.User == me {
				us = meStyle
			}
			lines = append(lines, us.Render(msg.User)+"  "+dimStyle.Render(clock(msg.At, now)))
		}
		if msg.Broadcast && !inThread {
			lines = append(lines, broadcastThreadLine(msg, msgs, body))
		}
		// Interpret Slack mrkdwn before wrapping: a labelled link should take
		// the width of its label, not of its hidden URL.
		// Text, not Raw: Text has the mentions by name, the links as
		// [label](url) (clickable below) and, for Block Kit messages, the
		// blocks; Raw is only the markup an edit starts from (for apps, a
		// one-line fallback).
		if q := msg.Quote; q != nil {
			lines = append(lines, quoteLines(*q, me, body)...)
		}
		rendered := a.richText(emoji.Expand(msg.Text), words)
		text := strings.Split(ansi.Wrap(rendered, body, " -"), "\n")
		lines = append(lines, text...)
		for _, attachment := range msg.Attachments {
			lines = append(lines, attachmentLines(attachment, body, a, words)...)
		}
		if msg.Edited {
			const mark = " (edited)"
			if last := len(lines) - 1; lipgloss.Width(text[len(text)-1])+len(mark) <= body {
				lines[last] += dimStyle.Render(mark)
			} else {
				lines = append(lines, dimStyle.Render(mark[1:]))
			}
		}
		for _, c := range msg.Context {
			for _, l := range strings.Split(ansi.Wrap(a.richText(emoji.Expand(c), words), body, " -"), "\n") {
				lines = append(lines, dimStyle.Render(l))
			}
		}
		files, pics := gallery(msg.Files, body, a)
		for k := range pics {
			pics[k].line += len(lines)
			pics[k].x += 1 + gutter // the margin, the avatar
			pics[k].msg = i
		}
		lines = append(lines, files...)
		if len(msg.Reactions) > 0 {
			var chips []string
			for _, r := range msg.Reactions {
				st := reactStyle
				if r.Mine {
					st = reactMine
				}
				chips = append(chips, st.Render(fmt.Sprintf("%s %d", a.reactionEmoji(r.Emoji), r.Count)))
			}
			lines = append(lines, strings.Join(chips, " "))
		}
		if msg.Replies > 0 && !inThread {
			n := "1 reply"
			if msg.Replies > 1 {
				n = fmt.Sprintf("%d replies", msg.Replies)
			}
			if boxed() {
				lines = append(lines, linkStyle.Render("["+n+" ->]"))
			} else if msg.LastReply.IsZero() {
				lines = append(lines, linkStyle.Render(n))
			} else {
				lines = append(lines, linkStyle.Render(n)+dimStyle.Render(" · last reply "+clock(msg.LastReply, now)))
			}
		}
		if gutter > 0 {
			// On every message, not only a run's first: who said what
			// stays plain in a busy chat.
			av := a.avatarLines(msg.UserID)
			for len(lines)-from < len(av) {
				lines = append(lines, "")
			}
			for k := from; k < len(lines); k++ {
				pad := strings.Repeat(" ", gutter)
				if k-from < len(av) {
					pad = av[k-from] + " "
				}
				lines[k] = pad + lines[k]
			}
		}
		blocks = append(blocks, block{from, len(lines), pics})
		prev = msg
	}
	for i, l := range lines {
		lines[i] = " " + ansi.Truncate(l, inner, "…")
	}
	return lines, blocks
}

// broadcastThreadLine gives a channel-broadcast thread reply the context
// Slack shows above it, rather than making it read like an unrelated message.
func broadcastThreadLine(msg chat.Message, msgs []chat.Message, width int) string {
	root := msg.ThreadContext
	for _, candidate := range msgs {
		if root == "" && candidate.TS == msg.ThreadRoot {
			root = candidate.Text
			if root == "" && len(candidate.Attachments) > 0 {
				root = candidate.Attachments[0].Title
			}
			break
		}
	}
	label := "↳ replied to a thread"
	if root == "" {
		return dimStyle.Render(label)
	}
	return dimStyle.Render(label+": ") + linkStyle.Render(ansi.Truncate(strings.Join(strings.Fields(root), " "), max(1, width-lipgloss.Width(label)-2), "…"))
}

// attachmentLines renders Slack's app cards without degrading them into one
// fallback sentence. Fields deliberately remain vertical on a terminal: they
// stay readable at narrow widths and preserve title/value meaning.
func attachmentLines(x chat.Attachment, width int, a art, words []string) []string {
	var out []string
	bar := "▌"
	if x.Color != "" {
		bar = lipgloss.NewStyle().Foreground(lipgloss.Color(x.Color)).Render(bar)
	}
	add := func(s string, style lipgloss.Style) {
		if s == "" {
			return
		}
		for _, line := range strings.Split(ansi.Wrap(a.richText(s, words), max(8, width-3), " -"), "\n") {
			out = append(out, bar+" "+style.Render(line))
		}
	}
	add(x.Pretext, dimStyle)
	title := x.Title
	if title != "" && x.TitleURL != "" {
		title = "[" + title + "](" + x.TitleURL + ")"
	}
	add(title, nameStyle)
	add(x.Text, textStyle)
	for _, f := range x.Fields {
		label := f.Title
		if label != "" {
			label += ": "
		}
		add(label+f.Value, textStyle)
	}
	add(x.Footer, dimStyle)
	return out
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func dayName(t, now time.Time) string {
	switch {
	case sameDay(t, now):
		return "Today"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday"
	case t.Year() == now.Year():
		return t.Format("Monday, January 2")
	}
	return t.Format("Monday, January 2, 2006")
}

// hour12 shows the times as 3:04 PM rather than 15:04 (the clock setting).
var hour12 bool

// hourOf is the time of day, on the 12 or the 24 hour clock.
func hourOf(t time.Time) string {
	if hour12 {
		return t.Format("3:04 PM")
	}
	return t.Format("15:04")
}

// clock is the time of day, with the day when it is not today.
func clock(t, now time.Time) string {
	if sameDay(t, now) {
		return hourOf(t)
	}
	return dayName(t, now) + " " + hourOf(t)
}

// daySeparator is "──── Today ────" across the pane (the leading margin
// column is added by the caller).
func daySeparator(t, now time.Time, width int) string {
	return centered(dayStyle.Render(" "+dayName(t, now)+" "), width-2, ruleStyle)
}

// newLine is the red rule with "New" on the right above the first unread
// message.
func newLine(width int) string {
	label := newStyle.Render(" New")
	return newRule.Render(strings.Repeat("─", max(0, width-2-lipgloss.Width(label)))) + label
}

func repliesRule(n int, width int) string {
	label := "1 reply"
	if n != 1 {
		label = fmt.Sprintf("%d replies", n)
	}
	s := dimStyle.Render(label + " ")
	return s + ruleStyle.Render(strings.Repeat("─", max(0, width-2-lipgloss.Width(s))))
}

func centered(label string, width int, rule lipgloss.Style) string {
	side := max(0, width-lipgloss.Width(label))
	return rule.Render(strings.Repeat("─", side/2)) + label + rule.Render(strings.Repeat("─", side-side/2))
}

// window picks the visible lines of a list scrolled off lines above its
// bottom, for rows screen lines. It returns them and the index of the first.
func window(lines []string, rows, off int) ([]string, int) {
	end := max(0, len(lines)-off)
	start := max(0, end-rows)
	return lines[start:end], start
}

// follow adjusts off so lines [from, to) are on screen, with the least
// scrolling; a block taller than the screen shows its top.
func follow(off, total, rows int, b block) int {
	top := total - off - rows
	switch {
	case b.from < top || b.to-b.from > rows:
		off = total - b.from - rows
	case b.to > total-off:
		off = total - b.to
	}
	return max(0, min(off, max(0, total-rows)))
}

// quoteLines is the message a reply answers, above it as Telegram draws it:
// a bar, who wrote it, and its beginning on one line.
func quoteLines(q chat.Quote, me string, w int) []string {
	bar := linkStyle.Render("▎")
	room := max(1, w-2)
	text := ansi.Truncate(strings.Join(strings.Fields(q.Text), " "), room, "…")
	if q.Who == "" {
		return []string{bar + " " + dimStyle.Render("in reply to "+text)}
	}
	who := nameStyle
	if q.Who == me {
		who = meStyle
	}
	return []string{bar + " " + who.Render(ansi.Truncate(q.Who, room, "…")), bar + " " + dimStyle.Render(text)}
}
