package chat

// Messages: Slack's wire types, their conversion to Message, and sending.

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/oscarmlage/sltty/internal/emoji"
)

type slackReaction struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	Users []string `json:"users"`
}
type slackFile struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Mimetype   string `json:"mimetype"`
	URLPrivate string `json:"url_private"`
	OriginalW  int    `json:"original_w"`
	OriginalH  int    `json:"original_h"`
	Thumb720   string `json:"thumb_720"`
	Thumb480   string `json:"thumb_480"`
	Thumb360   string `json:"thumb_360"`
}

// picture is the file's best thumbnail: big enough for a terminal, far
// smaller than the original.
func (f slackFile) picture() string {
	for _, u := range []string{f.Thumb720, f.Thumb480, f.Thumb360, f.URLPrivate} {
		if u != "" {
			return u
		}
	}
	return ""
}

type slackMessage struct {
	TS          string            `json:"ts"`
	Channel     string            `json:"channel"`
	User        string            `json:"user"`
	Text        string            `json:"text"`
	ThreadTS    string            `json:"thread_ts"`
	ReplyCount  int               `json:"reply_count"`
	LatestReply string            `json:"latest_reply"`
	Edited      *struct{}         `json:"edited"`
	Reactions   []slackReaction   `json:"reactions"`
	Files       []slackFile       `json:"files"`
	Attachments []slackAttachment `json:"attachments"`
	Blocks      slackBlocks       `json:"blocks"`
	// A bot's message (bot_message) has no user: it is the bot's, under
	// the name it posted with.
	BotID      string      `json:"bot_id"`
	Username   string      `json:"username"`
	Icons      *slackIcons `json:"icons"`
	BotProfile *struct {
		Name  string     `json:"name"`
		Icons slackIcons `json:"icons"`
	} `json:"bot_profile"`
}
type slackIcons struct {
	Image48 string `json:"image_48"`
	Image72 string `json:"image_72"`
	Emoji   string `json:"emoji"` // an incoming webhook's: its avatar is an emoji
}

// best is where the icon's picture is; s.mu held.
func (s *Slack) best(i slackIcons) string {
	if u := cmp.Or(i.Image72, i.Image48); u != "" || i.Emoji == "" {
		return u
	}
	name := strings.Trim(i.Emoji, ":")
	if u := s.urls["emoji:"+name]; u != "" {
		return u
	}
	g, ok := emoji.Glyph(name)
	if !ok {
		return ""
	}
	// Slack's own pictures of the standard emoji, by code point.
	var code []string
	for _, r := range g {
		code = append(code, strconv.FormatInt(int64(r), 16))
	}
	return "https://a.slack-edge.com/production-standard-emoji-assets/14.0/apple-large/" + strings.Join(code, "-") + ".png"
}

type slackAttachment struct {
	Color    string `json:"color"`
	Pretext  string `json:"pretext"`
	Title    string `json:"title"`
	TitleURL string `json:"title_link"`
	Text     string `json:"text"`
	Fallback string `json:"fallback"`
	Footer   string `json:"footer"`
	Fields   []struct {
		Title string `json:"title"`
		Value string `json:"value"`
		Short bool   `json:"short"`
	} `json:"fields"`
}

func (s *Slack) message(m slackMessage) Message {
	at, _ := parseSlackTS(m.TS)
	x := Message{TS: m.TS, User: s.user(m.User), UserID: m.User, At: at, Replies: m.ReplyCount, Edited: m.Edited != nil}
	if m.User == "" && m.BotID != "" {
		x.UserID = m.BotID
		// username is often the app's technical handle (for example
		// "sentry2"). Slack displays the bot profile's human-facing name.
		profileName := ""
		if m.BotProfile != nil {
			profileName = m.BotProfile.Name
		}
		x.User = cmp.Or(profileName, m.Username, s.user(m.BotID))
		s.botIcon(m)
	}
	x.Text, x.Raw = s.mrkdwn(m.Text), unescape(m.Text)
	if body, context, ok := blocksMrkdwn(m.Blocks); ok {
		x.Text = s.mrkdwn(body)
		for _, c := range context {
			x.Context = append(x.Context, s.mrkdwn(c))
		}
	}
	if x.Raw == x.Text {
		x.Raw = ""
	}
	if m.LatestReply != "" {
		x.LastReply, _ = parseSlackTS(m.LatestReply)
	}
	for _, r := range m.Reactions {
		mine := false
		for _, u := range r.Users {
			if u == s.me {
				mine = true
			}
		}
		e := ":" + r.Name + ":"
		if g, ok := emoji.Glyph(r.Name); ok {
			e = g
		}
		users := make([]string, 0, len(r.Users))
		for _, u := range r.Users {
			users = append(users, s.user(u))
		}
		x.Reactions = append(x.Reactions, Reaction{Emoji: e, Count: r.Count, Mine: mine, Users: users})
	}
	for _, a := range m.Attachments {
		out := Attachment{Color: a.Color, Pretext: unescape(a.Pretext), Title: unescape(a.Title), TitleURL: safeLink(a.TitleURL), Text: unescape(a.Text), Footer: unescape(a.Footer)}
		for _, f := range a.Fields {
			out.Fields = append(out.Fields, AttachmentField{Title: unescape(f.Title), Value: unescape(f.Value), Short: f.Short})
		}
		if out.Text == "" && out.Title == "" {
			out.Text = unescape(a.Fallback)
		}
		x.Attachments = append(x.Attachments, out)
	}
	for _, f := range m.Files {
		x.Files = append(x.Files, File{ID: f.ID, Name: f.Name, Mime: f.Mimetype, W: f.OriginalW, H: f.OriginalH})
		if u := f.picture(); u != "" {
			s.mu.Lock()
			s.urls[f.ID] = u
			s.urls["full:"+f.ID] = cmp.Or(f.URLPrivate, u)
			s.mu.Unlock()
		}
	}
	return x
}

// mention finds Slack's <…> markup: <@U1>, <#C1|name>, <!here>, <url|label>.
var mention = regexp.MustCompile(`<([^<>]+)>`)
var markdownLinkText = regexp.MustCompile(`\[([^\[\]]+)\]\((https?://[^)[:space:]]+)\)`)

// mrkdwn turns Slack's markup into what a person reads: names for user
// mentions, labels for links, and &, < and > for their entities.
func (s *Slack) mrkdwn(text string) string {
	text = markdownLinkText.ReplaceAllString(text, "$1")
	text = mention.ReplaceAllStringFunc(text, func(m string) string {
		inner := m[1 : len(m)-1]
		ref, label, _ := strings.Cut(inner, "|")
		switch {
		case strings.HasPrefix(ref, "@"):
			return "@" + cmp.Or(label, s.user(ref[1:]))
		case strings.HasPrefix(ref, "#"):
			return "#" + cmp.Or(label, ref[1:])
		case strings.HasPrefix(ref, "!subteam^"):
			return cmp.Or(label, "@group")
		case strings.HasPrefix(ref, "!"):
			name, _, _ := strings.Cut(ref[1:], "^")
			return "@" + cmp.Or(label, name)
		}
		if web := strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://"); web && label != "" && label != ref &&
			!strings.ContainsAny(label, "[]") && !strings.ContainsAny(ref, "() ") {
			return "[" + label + "](" + ref + ")" // a link the terminal can open, read by its label
		}
		return cmp.Or(label, strings.TrimPrefix(ref, "mailto:"))
	})
	return unescape(text)
}
func parseSlackTS(v string) (time.Time, error) {
	var sec float64
	_, err := fmt.Sscanf(v, "%f", &sec)
	return time.Unix(int64(sec), int64((sec-float64(int64(sec)))*1e9)), err
}
func (s *Slack) user(id string) string {
	if id == s.me {
		return s.Me()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := s.users[id]; n != "" {
		return n
	}
	return id
}

func (s *Slack) History(ctx context.Context, id string) ([]Message, error) {
	var r struct {
		Messages []slackMessage `json:"messages"`
	}
	if err := s.call(ctx, "conversations.history", url.Values{"channel": {id}, "limit": {"100"}}, &r); err != nil {
		return nil, err
	}
	s.resolveUsers(ctx, r.Messages)
	out := make([]Message, len(r.Messages))
	for i := range r.Messages {
		out[len(r.Messages)-1-i] = s.message(r.Messages[i])
	}
	return out, nil
}
func (s *Slack) Replies(ctx context.Context, id, root string) ([]Message, error) {
	var r struct {
		Messages []slackMessage `json:"messages"`
	}
	if err := s.call(ctx, "conversations.replies", url.Values{"channel": {id}, "ts": {root}, "limit": {"100"}}, &r); err != nil {
		return nil, err
	}
	s.resolveUsers(ctx, r.Messages)
	out := make([]Message, len(r.Messages))
	for i := range r.Messages {
		out[i] = s.message(r.Messages[i])
	}
	return out, nil
}
func (s *Slack) Send(ctx context.Context, id, root, text string) (Message, error) {
	f := url.Values{"channel": {id}, "text": {text}}
	if root != "" {
		f.Set("thread_ts", root)
	}
	var r struct {
		Message slackMessage `json:"message"`
	}
	err := s.call(ctx, "chat.postMessage", f, &r)
	return s.message(r.Message), err
}
func (s *Slack) Edit(ctx context.Context, id, ts, text string) (Message, error) {
	var r struct {
		Message slackMessage `json:"message"`
	}
	err := s.call(ctx, "chat.update", url.Values{"channel": {id}, "ts": {ts}, "text": {text}}, &r)
	m := s.message(r.Message)
	// The answer's message has no ts (it is beside it): without one the
	// edit would match no message on screen.
	m.TS = cmp.Or(m.TS, ts)
	return m, err
}
func (s *Slack) React(ctx context.Context, id, ts, emoji string) (Message, error) {
	// reactions.get is canonical and lets this action truly toggle.
	var before struct {
		Message slackMessage `json:"message"`
	}
	if err := s.call(ctx, "reactions.get", url.Values{"channel": {id}, "timestamp": {ts}, "full": {"true"}}, &before); err != nil {
		return Message{}, err
	}
	name := strings.Trim(emoji, ":")
	if n, ok := emojiName(emoji); ok {
		name = n
	}
	mine := false
	for _, r := range before.Message.Reactions {
		if r.Name == name {
			for _, u := range r.Users {
				mine = mine || u == s.me
			}
		}
	}
	method := "reactions.add"
	if mine {
		method = "reactions.remove"
	}
	var result struct{}
	if err := s.call(ctx, method, url.Values{"channel": {id}, "timestamp": {ts}, "name": {name}}, &result); err != nil {
		return Message{}, err
	}
	var after struct {
		Message slackMessage `json:"message"`
	}
	if err := s.call(ctx, "reactions.get", url.Values{"channel": {id}, "timestamp": {ts}, "full": {"true"}}, &after); err != nil {
		return Message{}, err
	}
	s.resolveUser(ctx, after.Message.User)
	return s.message(after.Message), nil
}
func (s *Slack) Search(ctx context.Context, q string) ([]Hit, error) {
	var r struct {
		Messages struct {
			Matches []struct {
				Channel struct {
					ID string `json:"id"`
				} `json:"channel"`
				slackMessage
			} `json:"matches"`
		} `json:"messages"`
	}
	if err := s.call(ctx, "search.messages", url.Values{"query": {q}, "count": {"50"}}, &r); err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(r.Messages.Matches))
	for _, m := range r.Messages.Matches {
		out = append(out, Hit{ConvID: m.Channel.ID, RootTS: m.ThreadTS, Msg: s.message(m.slackMessage)})
	}
	return out, nil
}

func (s *Slack) Mark(ctx context.Context, id, ts string) error {
	var r struct{}
	return s.call(ctx, "conversations.mark", url.Values{"channel": {id}, "ts": {ts}}, &r)
}
