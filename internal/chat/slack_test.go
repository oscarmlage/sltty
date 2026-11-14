package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// fakeSlack answers the Web API methods a conversation's pictures and
// text need, as Slack shapes them.
func fakeSlack(t *testing.T) (*Slack, *[]string) {
	t.Helper()
	var reacted []string
	png := []byte("\x89PNG fake")
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			if r.URL.Path == "/missing.png" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(append(png, r.URL.Path...))
			return
		}
		_ = r.ParseForm()
		var out any
		switch strings.TrimPrefix(r.URL.Path, "/api/") {
		case "users.info":
			id := r.Form.Get("user")
			out = map[string]any{"ok": true, "user": map[string]any{"name": strings.ToLower(id) + ".name",
				"profile": map[string]any{"image_72": srv.URL + "/avatar-" + id + ".png"}}}
		case "bots.info":
			icons := map[string]any{"image_72": srv.URL + "/bot-" + r.Form.Get("bot") + ".png"}
			if r.Form.Get("bot") == "BHOOK" {
				icons = map[string]any{"emoji": ":rabbit2:"} // an incoming webhook
			}
			out = map[string]any{"ok": true, "bot": map[string]any{"name": "deploy-bot", "icons": icons}}
		case "conversations.history":
			if r.Form.Get("channel") == "CBOT" {
				out = map[string]any{"ok": true, "messages": botMessages}
				break
			}
			out = map[string]any{"ok": true, "messages": []any{map[string]any{
				"ts": "1.0", "user": "U1",
				"text": "hi <@U2>, see <https://x.test/mr/1|the MR> &amp; <https://x.test/raw> <!here> :tada:",
				"reactions": []any{
					map[string]any{"name": "+1::skin-tone-3", "count": 2, "users": []string{"UME"}},
					map[string]any{"name": "party-parrot", "count": 1, "users": []string{"U2"}},
				},
				"files": []any{map[string]any{"id": "F1", "name": "shot.png", "mimetype": "image/png",
					"url_private": srv.URL + "/original.png", "thumb_720": srv.URL + "/thumb720.png",
					"original_w": 1600, "original_h": 900}},
			}}}
		case "emoji.list":
			out = map[string]any{"ok": true, "emoji": map[string]string{
				"party-parrot": srv.URL + "/parrot.gif", "parrot": "alias:party-parrot",
				"white_square": "alias:white_large_square", // a standard one: not a picture here
			}}
		case "reactions.get":
			out = map[string]any{"ok": true, "message": map[string]any{"ts": "1.0"}}
		case "reactions.add", "reactions.remove":
			reacted = append(reacted, r.URL.Path[5:]+" "+r.Form.Get("name"))
			out = map[string]any{"ok": true}
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			out = map[string]any{"ok": false, "error": "unknown_method"}
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	s := &Slack{creds: Credentials{Token: "xoxc-test", Cookie: "xoxd-test"}, me: "UME", hc: srv.Client(),
		apiBase: srv.URL + "/api/", users: map[string]string{}, urls: map[string]string{}}
	return s, &reacted
}

func TestSlackMessagesReadAsInSlack(t *testing.T) {
	s, _ := fakeSlack(t)
	msgs, err := s.History(context.Background(), "C1")
	if err != nil || len(msgs) != 1 {
		t.Fatalf("history: %v %+v", err, msgs)
	}
	m := msgs[0]
	if want := "hi @u2.name, see [the MR](https://x.test/mr/1) & https://x.test/raw @here :tada:"; m.Text != want {
		t.Fatalf("text\n got %q\nwant %q", m.Text, want)
	}
	if !strings.Contains(m.Raw, "<@U2>") || strings.Contains(m.Raw, "&amp;") {
		t.Fatalf("raw (what an edit starts from) %q", m.Raw)
	}
	if m.User != "u1.name" || m.UserID != "U1" {
		t.Fatalf("author %q / %q", m.User, m.UserID)
	}
	want := []Reaction{{Emoji: "👍🏼", Count: 2, Mine: true, Users: []string{"you"}}, {Emoji: ":party-parrot:", Count: 1, Users: []string{"u2.name"}}}
	if !reflect.DeepEqual(m.Reactions, want) {
		t.Fatalf("reactions %+v", m.Reactions)
	}
	if f := m.Files[0]; f.W != 1600 || f.H != 900 || !f.IsImage() {
		t.Fatalf("file %+v: no size", f)
	}
}

func TestSlackPictures(t *testing.T) {
	s, _ := fakeSlack(t)
	ctx := context.Background()
	if _, err := s.History(ctx, "C1"); err != nil {
		t.Fatal(err)
	}
	if b, err := s.File(ctx, "F1"); err != nil || !strings.HasSuffix(string(b), "/thumb720.png") {
		t.Fatalf("attachment: %q %v (want the thumbnail, not the original)", b, err)
	}
	if b, err := s.File(ctx, "full:F1"); err != nil || !strings.HasSuffix(string(b), "/original.png") {
		t.Fatalf("the viewer's: %q %v (want the original)", b, err)
	}
	if b, err := s.File(ctx, "avatar:U2"); err != nil || !strings.HasSuffix(string(b), "/avatar-U2.png") {
		t.Fatalf("avatar: %q %v", b, err)
	}
	if b, err := s.File(ctx, "avatar:UME"); err != nil || !strings.HasSuffix(string(b), "/avatar-UME.png") {
		t.Fatalf("your own avatar: %q %v", b, err)
	}
	names, err := s.CustomEmoji(ctx)
	if err != nil || !slices.Equal(names, []string{"parrot", "party-parrot"}) {
		t.Fatalf("custom emoji %v %v", names, err)
	}
	if b, err := s.File(ctx, "emoji:parrot"); err != nil || !strings.HasSuffix(string(b), "/parrot.gif") {
		t.Fatalf("an alias: %q %v", b, err)
	}
	if _, err := s.File(ctx, "emoji:nope"); err == nil {
		t.Fatal("a picture for an unknown emoji")
	}
	s.urls["avatar:U9"] = s.apiBase[:len(s.apiBase)-len("/api/")] + "/missing.png"
	s.users["U9"] = "u9"
	if _, err := s.File(ctx, "avatar:U9"); err == nil {
		t.Fatal("a 404 taken for a picture")
	}
}

func TestSlackReactsWithNames(t *testing.T) {
	s, reacted := fakeSlack(t)
	ctx := context.Background()
	for _, e := range []string{"👍", "✅", ":party-parrot:"} {
		if _, err := s.React(ctx, "C1", "1.0", e); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"reactions.add +1", "reactions.add white_check_mark", "reactions.add party-parrot"}
	if !slices.Equal(*reacted, want) {
		t.Fatalf("reacted %q, want %q", *reacted, want)
	}
}
