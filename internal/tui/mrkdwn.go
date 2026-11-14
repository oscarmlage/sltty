package tui

import (
	"regexp"
	"strconv"
	"strings"
)

// Slack sends link and mention tokens in its mrkdwn text. Turn those into what
// people read before wrapping: printing the URL itself is both noisy and makes
// terminal wrapping almost useless. OSC-8 is ignored by terminals that do not
// support hyperlinks and opens the URL on click in ones that do.
var slackToken = regexp.MustCompile(`<([^>|]+)(?:\|([^>]+))?>`)
var markdownLink = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)[:space:]]+)\)`)
var mentionToken = regexp.MustCompile(`(^|[^[:alnum:]_])(@[[:alnum:]_.-]+)`)
var (
	boldToken = regexp.MustCompile(`\*([^*\n]+)\*`)
	// As in Slack, _ marks italics at a word's edge only: snake_case
	// stays as typed.
	italicToken = regexp.MustCompile(`(^|[^[:alnum:]_])_([^_\n]+)_($|[^[:alnum:]_])`)
	keptToken   = regexp.MustCompile("\x00([0-9]+)\x00")
	strikeToken = regexp.MustCompile(`~([^~\n]+)~`)
	codeToken   = regexp.MustCompile("`([^`\\n]+)`")
	fenceToken  = regexp.MustCompile("(?s)```\\n?(.+?)\\n?```")
)

// unmark is a link's label without the marks around it: a link is drawn
// bold already, and the marks would show inside it.
func unmark(label string) string {
	for len(label) > 2 && strings.ContainsRune("*_~`", rune(label[0])) && label[len(label)-1] == label[0] {
		label = label[1 : len(label)-1]
	}
	return label
}

func osc8(url, label string) string {
	if url == "" {
		return label
	}
	return "\x1b]8;;" + url + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

func slackMrkdwn(s string) string {
	// NUL delimits the rendered pieces kept aside below; text must not forge it.
	s = strings.ReplaceAll(s, "\x00", "")
	// What is rendered already (links, code) is kept aside until the end,
	// so the marks below never reach into a URL or into code.
	var kept []string
	keep := func(v string) string {
		kept = append(kept, v)
		return "\x00" + strconv.Itoa(len(kept)-1) + "\x00"
	}
	// A code block, line by line: nothing inside is a link or a mark.
	s = fenceToken.ReplaceAllStringFunc(s, func(v string) string {
		lines := strings.Split(fenceToken.FindStringSubmatch(v)[1], "\n")
		for i, l := range lines {
			lines[i] = keyStyle.Render(l)
		}
		return keep(strings.Join(lines, "\n"))
	})
	// Some apps send Markdown-style links inside Slack mrkdwn rather than
	// Slack's <url|label> form. Handle both before wrapping.
	s = markdownLink.ReplaceAllStringFunc(s, func(token string) string {
		p := markdownLink.FindStringSubmatch(token)
		return keep(linkStyle.Render(osc8(p[2], unmark(p[1]))))
	})
	s = slackToken.ReplaceAllStringFunc(s, func(token string) string {
		parts := slackToken.FindStringSubmatch(token)
		target, label := parts[1], parts[2]
		if label == "" {
			label = target
		}
		switch {
		case strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://"), strings.HasPrefix(target, "mailto:"):
			return keep(linkStyle.Render(osc8(target, unmark(label))))
		case strings.HasPrefix(target, "#"):
			return "#" + strings.TrimPrefix(label, "#")
		case strings.HasPrefix(target, "@"):
			return "@" + strings.TrimPrefix(label, "@")
		case strings.HasPrefix(target, "!"):
			// <!here>, <!channel>, date tokens, and special Slack entities.
			return label
		default:
			return label
		}
	})
	// Slack's inline formatting is intentionally applied after links. This is
	// small rather than a full rich_text implementation, but covers ordinary
	// bot/human messages and keeps the raw form available in Message.Raw.
	// The backend resolves <@U…> into @handles. Make those handles visible
	// again in the terminal, like Slack's highlighted mentions.
	s = codeToken.ReplaceAllStringFunc(s, func(v string) string { return keep(keyStyle.Render(v[1 : len(v)-1])) })
	s = mentionToken.ReplaceAllString(s, "$1"+linkStyle.Render("$2"))
	s = boldToken.ReplaceAllString(s, "\x1b[1m$1\x1b[22m")
	s = italicToken.ReplaceAllString(s, "$1\x1b[3m$2\x1b[23m$3")
	s = strikeToken.ReplaceAllString(s, "\x1b[9m$1\x1b[29m")
	return keptToken.ReplaceAllStringFunc(s, func(v string) string {
		n, _ := strconv.Atoi(v[1 : len(v)-1])
		return kept[n]
	})
}
