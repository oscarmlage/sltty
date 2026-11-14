package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// botMessages are CBOT's history, newest first as Slack sends it: an
// app's message under its own name and with blocks, and one of a bot
// that only bots.info names.
var botMessages = func() []any {
	var v []any
	err := json.Unmarshal([]byte(`[
	{"type": "message", "ts": "2.0", "bot_id": "B2", "text": "plain"},
	{"type": "message", "subtype": "bot_message", "ts": "1.0", "bot_id": "B1",
	 "username": "Claude [daily run]",
	 "bot_profile": {"name": "Claude", "icons": {"image_72": "https://avatars.test/claude_72.png"}},
	 "text": ":orbit: *Daily* • one • two (fallback, one line)",
	 "blocks": [
	  {"type": "rich_text", "elements": [
	   {"type": "rich_text_section", "elements": [
	    {"type": "emoji", "name": "orbit"}, {"type": "text", "text": " "},
	    {"type": "text", "text": "Daily · Nov 13", "style": {"italic": true}},
	    {"type": "text", "text": "\n"},
	    {"type": "emoji", "name": "+1", "skin_tone": 3},
	    {"type": "text", "text": " Health ", "style": {"bold": true}}]},
	   {"type": "rich_text_list", "style": "bullet", "elements": [
	    {"type": "rich_text_section", "elements": [
	     {"type": "text", "text": "on "}, {"type": "text", "text": "6d2f", "style": {"code": true}},
	     {"type": "text", "text": " ("}, {"type": "link", "url": "https://git.test/mr/1379?a=1&copy=2", "text": "!1379"},
	     {"type": "text", "text": ") for "}, {"type": "user", "user_id": "U2"}]},
	    {"type": "rich_text_section", "elements": [{"type": "text", "text": "a < b & c"}]}]},
	   {"type": "rich_text_list", "style": "ordered", "indent": 1, "elements": [
	    {"type": "rich_text_section", "elements": [{"type": "text", "text": "nested"}]}]},
	   {"type": "rich_text_quote", "elements": [{"type": "text", "text": "quoted"}]},
	   {"type": "rich_text_preformatted", "elements": [{"type": "text", "text": "go test\nmake"}]}]},
	  {"type": "context", "elements": [
	   {"type": "mrkdwn", "text": "sonnet · <https://claude.test/configure|Configure>"}]}]}
	]`), &v)
	if err != nil {
		panic(err)
	}
	return v
}()

func TestBotMessagesReadAsInSlack(t *testing.T) {
	s, _ := fakeSlack(t)
	ctx := context.Background()
	msgs, err := s.History(ctx, "CBOT")
	if err != nil || len(msgs) != 2 {
		t.Fatalf("history: %v %+v", err, msgs)
	}
	app, bot := msgs[0], msgs[1]
	if app.User != "Claude" || app.UserID != "B1" {
		t.Fatalf("an app's message is under its profile name: %q / %q", app.User, app.UserID)
	}
	want := strings.Join([]string{
		":orbit: _Daily · Nov 13_",
		":+1::skin-tone-3: *Health* ",
		"• on `6d2f` ([!1379](https://git.test/mr/1379?a=1&copy=2)) for @u2.name",
		"• a < b & c",
		"  1. nested",
		"▎ quoted",
		"`go test`",
		"`make`",
	}, "\n")
	if app.Text != want {
		t.Fatalf("blocks, not the one-line fallback:\n got %q\nwant %q", app.Text, want)
	}
	if len(app.Context) != 1 || app.Context[0] != "sonnet · [Configure](https://claude.test/configure)" {
		t.Fatalf("context %q", app.Context)
	}
	if app.Raw != ":orbit: *Daily* • one • two (fallback, one line)" {
		t.Fatalf("raw %q", app.Raw)
	}
	if bot.User != "deploy-bot" || bot.UserID != "B2" || bot.Text != "plain" {
		t.Fatalf("a bot named by bots.info: %+v", bot)
	}
	if b, err := s.File(ctx, "avatar:B2"); err != nil || !strings.HasSuffix(string(b), "/bot-B2.png") {
		t.Fatalf("bots.info's avatar: %q %v", b, err)
	}
	s.resolveAuthor(ctx, "BHOOK")
	s.mu.Lock()
	u, hook := s.urls["avatar:B1"], s.urls["avatar:BHOOK"]
	s.mu.Unlock()
	if u != "https://avatars.test/claude_72.png" {
		t.Fatalf("the message's own avatar: %q", u)
	}
	if hook != "https://a.slack-edge.com/production-standard-emoji-assets/14.0/apple-large/1f407.png" {
		t.Fatalf("a webhook's emoji is its avatar: %q", hook)
	}
}

func TestBrokenBlocksKeepTheText(t *testing.T) {
	var m slackMessage
	err := json.Unmarshal([]byte(`{"ts": "1.0", "user": "U1", "text": "the *text*", "blocks": [
	 {"type": "actions", "elements": [{"type": "button", "text": {"type": "plain_text", "text": "Open"}, "style": "primary"}]},
	 {"type": "rich_text", "elements": {"not": "a list"}}]}`), &m)
	if err != nil {
		t.Fatal(err)
	}
	s := &Slack{users: map[string]string{"U1": "ana"}, urls: map[string]string{}}
	if x := s.message(m); x.Text != "the *text*" {
		t.Fatalf("a block that does not read leaves the text: %q", x.Text)
	}
}
