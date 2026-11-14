package chat

import (
	"cmp"
	"encoding/json"
	"strconv"
	"strings"
)

// Block Kit, as Slack's clients and apps send it next to a message's text.
// The text is a one-line fallback (no lists, no line breaks); the blocks
// are what Slack shows, so they are read the way slk reads them: back into
// mrkdwn, the wire form message() already turns into what people read.
//
// Blocks come in many shapes (buttons, images, inputs…) and new ones
// appear, so they are read leniently: one that does not decode is
// skipped, and a message whose blocks are not all read keeps its text.
type slackBlocks struct {
	list []slackBlock
	bad  bool
}

func (bs *slackBlocks) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		bs.bad = true
		return nil
	}
	for _, r := range raw {
		var b slackBlock
		if json.Unmarshal(r, &b) != nil {
			bs.bad = true
			continue
		}
		bs.list = append(bs.list, b)
	}
	return nil
}

type slackBlock struct {
	Type     string        `json:"type"`
	Text     *slackText    `json:"text"`   // section, header
	Fields   []slackText   `json:"fields"` // section
	Elements []richElement `json:"elements"`
}

type slackText struct {
	Type string `json:"type"` // mrkdwn, plain_text
	Text string `json:"text"`
}

// richElement is any element of a rich_text block (sections, lists,
// quotes, code, and their text, links, emoji and mentions) or of a
// context block (mrkdwn, plain_text, image).
type richElement struct {
	Type        string          `json:"type"`
	Text        flexText        `json:"text"`
	URL         string          `json:"url"`
	Name        string          `json:"name"`
	SkinTone    int             `json:"skin_tone"`
	UserID      string          `json:"user_id"`
	ChannelID   string          `json:"channel_id"`
	UsergroupID string          `json:"usergroup_id"`
	Range       string          `json:"range"`
	Fallback    string          `json:"fallback"`
	Value       string          `json:"value"`
	AltText     string          `json:"alt_text"`
	Style       json.RawMessage `json:"style"` // {bold…} on text, "bullet"/"ordered" on a list
	Indent      int             `json:"indent"`
	Offset      int             `json:"offset"`
	Elements    []richElement   `json:"elements"`
}

// flexText is text given as a string, or as a text object (a button's).
type flexText string

func (t *flexText) UnmarshalJSON(data []byte) error {
	var s string
	if json.Unmarshal(data, &s) != nil {
		var o slackText
		_ = json.Unmarshal(data, &o)
		s = o.Text
	}
	*t = flexText(s)
	return nil
}

type textStyle struct{ Bold, Italic, Strike, Code bool }

// blocksMrkdwn is the message's body read from its blocks, and its
// context blocks (the small print under it), both as mrkdwn. ok is false
// when the blocks say nothing the text should be replaced with.
func blocksMrkdwn(blocks slackBlocks) (body string, context []string, ok bool) {
	if blocks.bad {
		return "", nil, false
	}
	var parts []string
	for _, b := range blocks.list {
		switch b.Type {
		case "rich_text":
			parts = append(parts, richText(b.Elements))
		case "section":
			var p []string
			if b.Text != nil {
				p = append(p, b.Text.mrkdwn())
			}
			for _, f := range b.Fields {
				p = append(p, f.mrkdwn())
			}
			parts = append(parts, strings.Join(p, "\n"))
		case "header":
			if b.Text != nil {
				parts = append(parts, "*"+b.Text.mrkdwn()+"*")
			}
		case "divider":
			parts = append(parts, "──────")
		case "actions":
			// Block Kit actions cannot be invoked from the terminal yet, but
			// they are important context in app messages. Show their labels in
			// Slack's familiar bracket form rather than silently dropping them.
			var actions []string
			for _, e := range b.Elements {
				switch e.Type {
				case "button":
					if label := strings.TrimSpace(string(e.Text)); label != "" {
						actions = append(actions, "[ "+label+" ]")
					}
				case "static_select", "external_select", "users_select", "conversations_select", "channels_select":
					if label := strings.TrimSpace(string(e.Text)); label != "" {
						actions = append(actions, "[ "+label+" ▾ ]")
					}
				}
			}
			if len(actions) > 0 {
				parts = append(parts, strings.Join(actions, " "))
			}
		case "context":
			var p []string
			for _, e := range b.Elements {
				switch e.Type {
				case "mrkdwn":
					p = append(p, string(e.Text))
				case "plain_text":
					p = append(p, escape(string(e.Text)))
				}
			}
			if line := strings.Join(p, "  "); strings.TrimSpace(line) != "" {
				context = append(context, line)
			}
		}
	}
	body = strings.Trim(strings.Join(parts, "\n"), "\n")
	return body, context, strings.TrimSpace(body) != ""
}

func (t slackText) mrkdwn() string {
	if t.Type == "plain_text" {
		return escape(t.Text)
	}
	return t.Text
}

func richText(elements []richElement) string {
	var parts []string
	for _, e := range elements {
		switch e.Type {
		case "rich_text_section":
			parts = append(parts, inline(e.Elements))
		case "rich_text_list":
			parts = append(parts, list(e))
		case "rich_text_preformatted":
			parts = append(parts, preformatted(e.Elements))
		case "rich_text_quote":
			lines := strings.Split(strings.Trim(inline(e.Elements), "\n"), "\n")
			for i, l := range lines {
				lines[i] = "▎ " + l
			}
			parts = append(parts, strings.Join(lines, "\n"))
		}
	}
	return strings.Join(parts, "\n")
}

func inline(elements []richElement) string {
	var b strings.Builder
	for _, e := range elements {
		switch e.Type {
		case "text":
			b.WriteString(styled(escape(string(e.Text)), e.Style))
		case "link":
			label := string(e.Text)
			if label == "" || label == e.URL {
				b.WriteString("<" + escape(e.URL) + ">")
			} else {
				b.WriteString("<" + escape(e.URL) + "|" + escape(label) + ">")
			}
		case "emoji":
			b.WriteString(":" + e.Name + ":")
			if e.SkinTone > 0 {
				b.WriteString(":skin-tone-" + strconv.Itoa(e.SkinTone) + ":")
			}
		case "user":
			b.WriteString("<@" + e.UserID + ">")
		case "channel":
			b.WriteString("<#" + e.ChannelID + ">")
		case "usergroup":
			b.WriteString("<!subteam^" + e.UsergroupID + ">")
		case "broadcast":
			b.WriteString("<!" + e.Range + ">")
		case "date":
			b.WriteString(escape(e.Fallback))
		case "color":
			b.WriteString(e.Value)
		}
	}
	return b.String()
}

// styled marks a run of text the way mrkdwn does, line by line (marks do
// not cross lines) and around the words only, leaving the spaces out.
func styled(text string, raw json.RawMessage) string {
	var st textStyle
	if len(raw) == 0 || json.Unmarshal(raw, &st) != nil || st == (textStyle{}) {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		core := strings.TrimSpace(line)
		if core == "" {
			continue
		}
		at, n := strings.Index(line, core), len(core)
		if st.Code {
			core = "`" + core + "`"
		}
		if st.Strike {
			core = "~" + core + "~"
		}
		if st.Italic {
			core = "_" + core + "_"
		}
		if st.Bold {
			core = "*" + core + "*"
		}
		lines[i] = line[:at] + core + line[at+n:]
	}
	return strings.Join(lines, "\n")
}

func list(l richElement) string {
	var ordered string
	_ = json.Unmarshal(l.Style, &ordered)
	pad := strings.Repeat("  ", l.Indent)
	lines := make([]string, 0, len(l.Elements))
	for i, item := range l.Elements {
		marker := "• "
		if ordered == "ordered" {
			marker = strconv.Itoa(l.Offset+i+1) + ". "
		}
		lines = append(lines, pad+marker+inline(item.Elements))
	}
	return strings.Join(lines, "\n")
}

// preformatted is a code block: each line as code, the closest mrkdwn
// gets without fences.
func preformatted(elements []richElement) string {
	var b strings.Builder
	for _, e := range elements {
		switch e.Type {
		case "link":
			b.WriteString(cmp.Or(e.URL, string(e.Text)))
		default:
			b.WriteString(string(e.Text))
		}
	}
	lines := strings.Split(strings.Trim(b.String(), "\n"), "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "" && !strings.Contains(l, "`") {
			lines[i] = "`" + l + "`"
		}
		lines[i] = escape(lines[i])
	}
	return strings.Join(lines, "\n")
}

// richUsers are the people a message's blocks mention.
func richUsers(blocks slackBlocks) []string {
	var out []string
	var walk func([]richElement)
	walk = func(es []richElement) {
		for _, e := range es {
			if e.Type == "user" && e.UserID != "" {
				out = append(out, e.UserID)
			}
			walk(e.Elements)
		}
	}
	for _, b := range blocks.list {
		walk(b.Elements)
	}
	return out
}

var mrkdwnEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// escape writes literal text in mrkdwn, where <, > and & are markup.
func escape(s string) string { return mrkdwnEscaper.Replace(s) }
