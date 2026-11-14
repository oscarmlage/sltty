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

func readBlocks(t *testing.T, js string) (string, []string, bool) {
	t.Helper()
	var bs slackBlocks
	if err := json.Unmarshal([]byte(js), &bs); err != nil {
		t.Fatal(err)
	}
	return blocksMrkdwn(bs)
}

func TestBlockKitShapes(t *testing.T) {
	body, ctx, ok := readBlocks(t, `[
	 {"type":"header","text":{"type":"plain_text","text":"Deploy <ok>"}},
	 {"type":"section","text":{"type":"mrkdwn","text":"*done* in <https://x.test|x>"},
	  "fields":[{"type":"mrkdwn","text":"env"},{"type":"plain_text","text":"a & b"}]},
	 {"type":"divider"},
	 {"type":"actions","elements":[
	   {"type":"button","text":{"type":"plain_text","text":"Open"}},
	   {"type":"button","text":{"type":"plain_text","text":" "}},
	   {"type":"static_select","text":{"type":"plain_text","text":"Assign"}},
	   {"type":"external_select","text":{"type":"plain_text","text":""}},
	   {"type":"image","alt_text":"ignored"}]},
	 {"type":"actions","elements":[{"type":"button","text":""}]},
	 {"type":"context","elements":[{"type":"mrkdwn","text":"by *bot*"},{"type":"plain_text","text":"1 < 2"},{"type":"image","alt_text":"x"}]},
	 {"type":"context","elements":[{"type":"image","alt_text":"only an image"}]},
	 {"type":"header"},
	 {"type":"section"},
	 {"type":"unknown_new_block","elements":[{"type":"text","text":"ignored"}]}]`)
	if !ok {
		t.Fatal("blocks with content must replace the text")
	}
	for _, want := range []string{"*Deploy &lt;ok&gt;*", "*done* in <https://x.test|x>", "env\na &amp; b", "──────", "[ Open ] [ Assign ▾ ]"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	if len(ctx) != 1 || ctx[0] != "by *bot*  1 &lt; 2" {
		t.Fatalf("context %q", ctx)
	}
}

func TestRichTextElements(t *testing.T) {
	body, _, ok := readBlocks(t, `[{"type":"rich_text","elements":[
	 {"type":"rich_text_section","elements":[
	  {"type":"text","text":"hi "},{"type":"user","user_id":"U1"},{"type":"text","text":" "},
	  {"type":"channel","channel_id":"C9"},{"type":"text","text":" "},
	  {"type":"usergroup","usergroup_id":"S1"},{"type":"text","text":" "},
	  {"type":"broadcast","range":"here"},{"type":"text","text":" "},
	  {"type":"date","fallback":"Nov 13 <soon>"},{"type":"text","text":" "},
	  {"type":"color","value":"#ff0000"},{"type":"text","text":" "},
	  {"type":"link","url":"https://a.test/?q=1&r=2"},{"type":"text","text":" "},
	  {"type":"link","url":"https://b.test","text":"B"},{"type":"text","text":" "},
	  {"type":"text","text":"gone","style":{"strike":true,"code":true}},{"type":"text","text":"\n\n"},
	  {"type":"text","text":"two\nlines","style":{"bold":true,"italic":true}}]},
	 {"type":"rich_text_list","style":"ordered","offset":2,"indent":1,"elements":[
	  {"type":"rich_text_section","elements":[{"type":"text","text":"c"}]},
	  {"type":"rich_text_section","elements":[{"type":"text","text":"d"}]}]},
	 {"type":"rich_text_preformatted","elements":[
	  {"type":"text","text":"a < b\n\nwith \u0060 tick\n"},{"type":"link","url":"https://c.test"},{"type":"link","text":"label only"}]},
	 {"type":"rich_text_quote","elements":[{"type":"text","text":"quoted\nbox"}]}]}]`)
	if !ok {
		t.Fatal("no body")
	}
	for _, want := range []string{
		"hi <@U1> <#C9> <!subteam^S1> <!here> Nov 13 &lt;soon&gt; #ff0000 <https://a.test/?q=1&amp;r=2> <https://b.test|B> ~`gone`~",
		"*_two_*\n*_lines_*", "  3. c\n  4. d", "`a &lt; b`", "`https://c.testlabel only`", "▎ quoted\n▎ box",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	// the people a message's blocks mention are looked up
	var bs slackBlocks
	_ = json.Unmarshal([]byte(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"user","user_id":"U7"},{"type":"user"}]}]}]`), &bs)
	if got := richUsers(bs); len(got) != 1 || got[0] != "U7" {
		t.Fatalf("richUsers %v", got)
	}
}

func TestBlocksWithNothingToSayAndBadShapes(t *testing.T) {
	if _, _, ok := readBlocks(t, `[{"type":"divider"},{"type":"rich_text","elements":[]}]`); !ok {
		// a divider alone is still a body
		t.Fatal("a divider is content")
	}
	if _, _, ok := readBlocks(t, `[{"type":"section","text":{"type":"mrkdwn","text":"  "}}]`); ok {
		t.Fatal("blank blocks must keep the message text")
	}
	if _, _, ok := readBlocks(t, `"not a list"`); ok {
		t.Fatal("garbage blocks must keep the message text")
	}
	if _, _, ok := readBlocks(t, `[{"type":"section","text":5}]`); ok {
		t.Fatal("a block that does not decode marks them all bad")
	}
}
