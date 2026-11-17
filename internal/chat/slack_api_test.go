package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// handler answers one Web API method with a JSON-able value, or a string
// holding raw (possibly invalid) JSON.
type handler func(form url.Values) any

// apiServer is a TLS server for the Web API methods in handlers; every
// request must carry the session headers. Calls are counted per method.
func apiServer(t *testing.T, handlers map[string]handler) (*Slack, func(string) int) {
	t.Helper()
	var mu sync.Mutex
	calls := map[string]int{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/api/")
		mu.Lock()
		calls[method]++
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer xoxc-test" || r.Header.Get("Cookie") != "d=xoxd-test" {
			t.Errorf("%s: session headers missing (%q, %q)", method, r.Header.Get("Authorization"), r.Header.Get("Cookie"))
		}
		h, ok := handlers[method]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":false,"error":"unknown_method"}`))
			return
		}
		_ = r.ParseForm()
		out := h(r.Form)
		w.Header().Set("Content-Type", "application/json")
		switch v := out.(type) {
		case string:
			_, _ = w.Write([]byte(v))
		case int:
			w.WriteHeader(v)
		default:
			_, _ = w.Write([]byte(jsonString(t, v)))
		}
	}))
	t.Cleanup(srv.Close)
	s := &Slack{creds: Credentials{Token: "xoxc-test", Cookie: "xoxd-test", TeamID: "T1"}, me: "UME", hc: srv.Client(),
		apiBase: srv.URL + "/api/", users: map[string]string{}, urls: map[string]string{}}
	return s, func(m string) int { mu.Lock(); defer mu.Unlock(); return calls[m] }
}

type obj = map[string]any

func ok(extra obj) obj {
	extra["ok"] = true
	return extra
}

func users(form url.Values) any {
	id := form.Get("user")
	return ok(obj{"user": obj{"name": strings.ToLower(id), "profile": obj{"image_72": "https://avatars.test/" + id}}})
}

func TestNewSlackValidatesTheSession(t *testing.T) {
	if _, err := NewSlack(context.Background(), Credentials{Token: "xoxc-x"}); err == nil {
		t.Fatal("a session without cookie is incomplete")
	}
	s, _ := apiServer(t, map[string]handler{"auth.test": func(url.Values) any {
		return ok(obj{"user_id": "UME", "team_id": "T9", "team": "Acme"})
	}})
	// NewSlack builds its own client; reuse the test server's through call.
	var a struct{ Team string }
	if err := s.call(context.Background(), "auth.test", nil, &a); err != nil || a.Team != "Acme" {
		t.Fatalf("auth.test: %+v %v", a, err)
	}
	if s.Me() != "UME" || s.Workspace() != "" {
		t.Fatalf("Me %q Workspace %q", s.Me(), s.Workspace())
	}
}

func TestCallErrors(t *testing.T) {
	s, calls := apiServer(t, map[string]handler{
		"boom":    func(url.Values) any { return 500 },
		"notok":   func(url.Values) any { return obj{"ok": false, "error": "channel_not_found"} },
		"garbage": func(url.Values) any { return "{not json" },
		"dirty": func(url.Values) any {
			return `{"ok":true,"text":"a\u001b]52;c;eA==\u0007b","name":"x\u009by"}`
		},
	})
	ctx := context.Background()
	var out struct{ Text, Name string }
	if err := s.call(ctx, "boom", nil, &out); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("500: %v", err)
	}
	if err := s.call(ctx, "notok", nil, &out); err == nil || !strings.Contains(err.Error(), "channel_not_found") {
		t.Fatalf("ok=false: %v", err)
	}
	if err := s.call(ctx, "garbage", nil, &out); err == nil {
		t.Fatal("invalid JSON must be an error")
	}
	if err := s.call(ctx, "dirty", nil, &out); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.Text+out.Name, "\x1b\x07\u009b") {
		t.Fatalf("control characters reached the caller: %q %q", out.Text, out.Name)
	}
	if calls("boom") != 1 {
		t.Fatal("a failing call must not be retried silently")
	}
}

func TestRedirectsMustStayOnHTTPS(t *testing.T) {
	c := newHTTPClient()
	req := func(u string) *http.Request { r, _ := http.NewRequest("GET", u, nil); return r }
	if err := c.CheckRedirect(req("http://files.slack.com/x"), []*http.Request{req("https://slack.com")}); err == nil {
		t.Fatal("followed a redirect to http")
	}
	if err := c.CheckRedirect(req("https://files.slack.com/x"), []*http.Request{req("https://slack.com")}); err != nil {
		t.Fatal(err)
	}
	via := make([]*http.Request, 5)
	if err := c.CheckRedirect(req("https://slack.com/x"), via); err == nil {
		t.Fatal("no redirect limit")
	}
}

func TestFileRefusesPlainHTTP(t *testing.T) {
	s, _ := apiServer(t, nil)
	s.urls["emoji:x"] = "http://files.slack.com/x.png"
	if _, err := s.File(context.Background(), "emoji:x"); err == nil || !strings.Contains(err.Error(), "non-https") {
		t.Fatalf("File over http: %v", err)
	}
}

func TestConversations(t *testing.T) {
	pages := 0
	s, calls := apiServer(t, map[string]handler{
		"users.conversations": func(f url.Values) any {
			pages++
			if f.Get("cursor") == "" {
				return ok(obj{"channels": []obj{
					{"id": "C1", "name": "general", "topic": obj{"value": "all hands"}, "unread_count_display": 2},
					{"id": "C2", "name": "secret", "is_private": true},
				}, "response_metadata": obj{"next_cursor": "p2"}})
			}
			return ok(obj{"channels": []obj{{"id": "D1", "is_im": true, "user": "UANA"}}})
		},
		"users.info":         users,
		"conversations.open": func(url.Values) any { return ok(obj{"channel": obj{"id": "DME", "is_im": true, "user": "UME"}}) },
		"users.channelSections.list": func(url.Values) any {
			return ok(obj{"channel_sections": []obj{
				{"channel_section_id": "S2", "name": "Later", "emoji": "zzz", "channel_ids_page": obj{"channel_ids": []string{"C1"}}},
				{"channel_section_id": "S1", "name": "Now", "next_channel_section_id": "S2", "channel_ids_page": obj{"channel_ids": []string{"C2", "D1"}}},
			}})
		},
		"client.counts": func(url.Values) any {
			return ok(obj{"channels": []obj{{"id": "C1", "unread_count_display": 5}, {"id": "C2", "has_unreads": true}},
				"ims": []obj{{"id": "D1", "mention_count": 3}}})
		},
	})
	got, err := s.Conversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range got {
		names = append(names, c.Name+"/"+c.Section)
	}
	// native sections order: Now (C2, D1) then Later (C1); the self DM, which
	// no section holds, goes last
	if want := "secret/Now uana/Now general/Later you/"; strings.Join(names, " ") != want {
		t.Fatalf("conversations %q, want %q", strings.Join(names, " "), want)
	}
	if pages != 2 {
		t.Fatalf("pagination: %d pages", pages)
	}
	by := map[string]Conversation{}
	for _, c := range got {
		by[c.ID] = c
	}
	if by["C1"].Unread != 5 || by["C2"].Unread != 1 || by["D1"].Unread != 3 || !by["C2"].Private || by["C1"].Topic != "all hands" || by["D1"].Kind != DM {
		t.Fatalf("details: %+v", by)
	}
	if by["C1"].SectionEmoji != "zzz" || calls("conversations.open") != 1 {
		t.Fatalf("emoji %q, opens %d", by["C1"].SectionEmoji, calls("conversations.open"))
	}
}

func TestNativeSectionNameDropsInvisibleFormatting(t *testing.T) {
	if got := nativeSectionName(" ON\u200bCALL "); got != "ONCALL" {
		t.Fatalf("section name %q", got)
	}
}

func TestNativeSectionsDoNotRepeatAConversation(t *testing.T) {
	convs := []Conversation{{ID: "C1", Name: "general"}, {ID: "C2", Name: "random"}}
	got := applyNativeSections(convs, []nativeSection{
		{Name: "First", IDs: []string{"C1", "C2"}},
		{Name: "Again", IDs: []string{"C1"}},
	})
	if len(got) != 2 || got[0].Section != "First" || got[1].Section != "First" {
		t.Fatalf("duplicated native section membership: %+v", got)
	}
}

func TestConversationsEnterpriseFallback(t *testing.T) {
	s, _ := apiServer(t, map[string]handler{
		"users.conversations": func(url.Values) any { return obj{"ok": false, "error": "enterprise_is_restricted"} },
		"client.userBoot": func(url.Values) any {
			return ok(obj{"channels": []obj{{"id": "C1", "name": "general", "is_private": false}},
				"ims": []obj{{"id": "D1", "user": "UANA", "is_open": true}, {"id": "D2", "user": "UOLD", "is_open": false}, {"id": "D3", "user": "UGONE", "is_open": true, "is_archived": true}}})
		},
		"users.info":         users,
		"conversations.open": func(url.Values) any { return ok(obj{"channel": obj{"id": "DME"}}) },
	})
	got, err := s.Conversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range got {
		names = append(names, c.Name)
	}
	if strings.Join(names, " ") != "general uana you" {
		t.Fatalf("fallback conversations: %v (closed and archived DMs must not show)", names)
	}
}

func TestConversationsError(t *testing.T) {
	s, _ := apiServer(t, map[string]handler{"users.conversations": func(url.Values) any { return obj{"ok": false, "error": "invalid_auth"} }})
	if _, err := s.Conversations(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid_auth") {
		t.Fatalf("error: %v", err)
	}
}

func TestMessagesRepliesSendEditSearchMark(t *testing.T) {
	var sent url.Values
	s, calls := apiServer(t, map[string]handler{
		"users.info": users,
		"conversations.replies": func(f url.Values) any {
			return ok(obj{"messages": []obj{{"ts": "1.0", "user": "U1", "text": "root", "reply_count": 1}, {"ts": "2.0", "user": "U2", "text": "reply", "thread_ts": "1.0"}}})
		},
		"chat.postMessage": func(f url.Values) any {
			sent = f
			return ok(obj{"message": obj{"ts": "3.0", "user": "UME", "text": f.Get("text")}})
		},
		"chat.update": func(f url.Values) any {
			return ok(obj{"message": obj{"ts": f.Get("ts"), "user": "UME", "text": f.Get("text"), "edited": obj{"ts": "9.0"}}})
		},
		"search.messages": func(f url.Values) any {
			return ok(obj{"messages": obj{"matches": []obj{{"channel": obj{"id": "C1"}, "ts": "2.0", "thread_ts": "1.0", "user": "U2", "text": "a runner"}}}})
		},
		"conversations.mark": func(f url.Values) any { return ok(obj{}) },
	})
	ctx := context.Background()
	rs, err := s.Replies(ctx, "C1", "1.0")
	if err != nil || len(rs) != 2 || rs[0].Text != "root" || rs[0].Replies != 1 || rs[1].User != "u2" {
		t.Fatalf("replies: %+v %v", rs, err)
	}
	m, err := s.Send(ctx, "C1", "1.0", "hello")
	if err != nil || m.Text != "hello" || m.User != "you" || sent.Get("thread_ts") != "1.0" || sent.Get("channel") != "C1" {
		t.Fatalf("send: %+v %v %v", m, err, sent)
	}
	if _, err := s.Send(ctx, "C1", "", "top level"); err != nil || sent.Get("thread_ts") != "" {
		t.Fatalf("a top-level message carried thread_ts %q (%v)", sent.Get("thread_ts"), err)
	}
	e, err := s.Edit(ctx, "C1", "3.0", "fixed")
	if err != nil || e.Text != "fixed" || !e.Edited {
		t.Fatalf("edit: %+v %v", e, err)
	}
	hits, err := s.Search(ctx, "runner in:#general")
	if err != nil || len(hits) != 1 || hits[0].ConvID != "C1" || hits[0].RootTS != "1.0" || hits[0].Msg.Text != "a runner" {
		t.Fatalf("search: %+v %v", hits, err)
	}
	if err := s.Mark(ctx, "C1", "2.0"); err != nil || calls("conversations.mark") != 1 {
		t.Fatalf("mark: %v", err)
	}
}

func TestMessagesErrorsPropagate(t *testing.T) {
	s, _ := apiServer(t, nil) // every method answers unknown_method
	ctx := context.Background()
	if _, err := s.History(ctx, "C"); err == nil {
		t.Error("History")
	}
	if _, err := s.Replies(ctx, "C", "1"); err == nil {
		t.Error("Replies")
	}
	if _, err := s.Send(ctx, "C", "", "x"); err == nil {
		t.Error("Send")
	}
	if _, err := s.Edit(ctx, "C", "1", "x"); err == nil {
		t.Error("Edit")
	}
	if _, err := s.React(ctx, "C", "1", "👍"); err == nil {
		t.Error("React")
	}
	if _, err := s.Search(ctx, "x"); err == nil {
		t.Error("Search")
	}
	if _, err := s.Threads(ctx); err == nil {
		t.Error("Threads")
	}
	if _, err := s.Activity(ctx); err == nil {
		t.Error("Activity")
	}
	if err := s.Mark(ctx, "C", "1"); err == nil {
		t.Error("Mark")
	}
	if _, err := s.CustomEmoji(ctx); err == nil {
		t.Error("CustomEmoji")
	}
	if err := s.SetPresence(ctx, true); err == nil {
		t.Error("SetPresence")
	}
	if err := s.SetSnooze(ctx, 0); err == nil {
		t.Error("SetSnooze")
	}
}

func TestReactTogglesWhatIsMine(t *testing.T) {
	for _, tc := range []struct {
		users  []string
		method string
	}{{[]string{"UME"}, "reactions.remove"}, {[]string{"UOTHER"}, "reactions.add"}} {
		var did, name string
		s, _ := apiServer(t, map[string]handler{
			"users.info": users,
			"reactions.get": func(url.Values) any {
				return ok(obj{"message": obj{"ts": "1.0", "user": "U1", "text": "x", "reactions": []obj{{"name": "tada", "count": 1, "users": tc.users}}}})
			},
			"reactions.add":    func(f url.Values) any { did, name = "reactions.add", f.Get("name"); return ok(obj{}) },
			"reactions.remove": func(f url.Values) any { did, name = "reactions.remove", f.Get("name"); return ok(obj{}) },
		})
		if _, err := s.React(context.Background(), "C1", "1.0", ":tada:"); err != nil || did != tc.method || name != "tada" {
			t.Fatalf("users %v: did %q name %q err %v, want %s", tc.users, did, name, err, tc.method)
		}
	}
}

func TestThreadsFeed(t *testing.T) {
	s, _ := apiServer(t, map[string]handler{
		"users.info": users,
		"subscriptions.thread.getView": func(f url.Values) any {
			if f.Get("team_id") != "T1" {
				t.Errorf("team_id %q", f.Get("team_id"))
			}
			return ok(obj{"threads": []obj{
				{"root_msg": obj{"ts": "1.0", "channel": "C1", "user": "U1", "text": "old", "reply_count": 2, "latest_reply": "5.0"},
					"latest_replies": []obj{{"ts": "5.0", "user": "U2", "text": "r2"}, {"ts": "4.0", "user": "U2", "text": "r1"}},
					"unread_replies": []obj{{"ts": "5.0", "user": "U2", "text": "r2"}}},
				{"root_msg": obj{"ts": "2.0", "channel": "C2", "user": "U1", "text": "newer", "reply_count": 1, "latest_reply": "9.0"}},
				{"root_msg": obj{"ts": "3.0", "text": "no channel: skipped"}},
			}})
		},
	})
	got, err := s.Threads(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("threads: %+v %v", got, err)
	}
	if got[0].Root.Text != "newer" || got[1].Root.Text != "old" {
		t.Fatalf("not latest reply first: %q, %q", got[0].Root.Text, got[1].Root.Text)
	}
	old := got[1]
	if old.Unread != 1 || old.Replies != 2 || len(old.Latest) != 2 || old.Latest[0].Text != "r1" || old.ConvID != "C1" {
		t.Fatalf("thread details (replies oldest first, deduplicated): %+v", old)
	}
}

func TestActivityFeed(t *testing.T) {
	s, _ := apiServer(t, map[string]handler{
		"users.info": users,
		"activity.feed": func(f url.Values) any {
			return ok(obj{"items": []obj{
				{"key": "k1", "feed_ts": "100.0", "is_unread": true, "item": obj{"type": "at_user", "message": obj{"ts": "1.0", "channel": "C1", "user": "U1", "text": "hi @you"}}},
				{"key": "k2", "feed_ts": "200.0", "item": obj{"type": "thread_v2", "bundle_info": obj{"payload": obj{"thread_entry": obj{"channel_id": "C2", "latest_ts": "7.0", "thread_ts": "6.0"}}}}},
				{"key": "k3", "feed_ts": "300.0", "item": obj{"type": "message_reaction", "message": obj{"ts": "2.0", "channel": "C1", "user": "UME", "text": "mine"}, "reaction": obj{"user": "U3", "name": "tada"}}},
				{"key": "k6", "feed_ts": "400.0", "is_unread": true, "item": obj{"type": "dm", "bundle_info": obj{"payload": obj{"dm_entry": obj{"latest_message": obj{"channel": "D1", "ts": "3.0"}}}}}},
				{"key": "k7", "feed_ts": "500.0", "item": obj{"type": "bot_dm_bundle", "bundle_info": obj{"payload": obj{"message": obj{"channel": "D2", "ts": "4.0"}}, "unread_count": 2}}},
				{"key": "k4", "item": obj{"type": "something_new"}},
				{"key": "k5", "item": obj{"type": "at_user", "message": obj{"text": "no channel"}}},
			}})
		},
		"conversations.history": func(f url.Values) any {
			return ok(obj{"messages": []obj{{"ts": f.Get("latest"), "user": "U1", "text": "hydrated " + f.Get("latest")}}})
		},
		"conversations.replies": func(f url.Values) any {
			return ok(obj{"messages": []obj{{"ts": "6.0", "user": "U1", "text": "root"}, {"ts": "7.0", "user": "U4", "text": "the reply", "thread_ts": "6.0"}}})
		},
	})
	got, err := s.Activity(context.Background())
	if err != nil || len(got) != 5 {
		t.Fatalf("activity: %+v %v", got, err)
	}
	// DMs come in bundle_info, not in item.message.
	if a := got[3]; a.Kind != ActDM || a.ConvID != "D1" || a.Msg.Text != "hydrated 3.0" || !a.Unread || a.By != "u1" {
		t.Fatalf("dm: %+v", a)
	}
	if a := got[4]; a.Kind != ActDM || a.ConvID != "D2" || a.Msg.Text != "hydrated 4.0" {
		t.Fatalf("app dm: %+v", a)
	}
	if a := got[0]; a.Kind != ActMention || a.Msg.Text != "hydrated 1.0" || !a.Unread || a.By != "u1" || a.At.Unix() != 100 {
		t.Fatalf("mention: %+v", a)
	}
	if a := got[1]; a.Kind != ActReply || a.RootTS != "6.0" || a.Msg.Text != "the reply" || a.ConvID != "C2" {
		t.Fatalf("reply: %+v", a)
	}
	if a := got[2]; a.Kind != ActReaction || a.Emoji != ":tada:" || a.By != "u3" || a.ByID != "U3" {
		t.Fatalf("reaction: %+v", a)
	}
}

func TestActivityFallsBackToTheFeedsMessage(t *testing.T) {
	s, _ := apiServer(t, map[string]handler{
		"users.info": users,
		"activity.feed": func(url.Values) any {
			return ok(obj{"items": []obj{{"key": "k1", "feed_ts": "1.0", "item": obj{"type": "at_user", "message": obj{"ts": "1.0", "channel": "C1", "user": "U1", "text": "from the feed"}}}}})
		},
		"conversations.history": func(url.Values) any { return ok(obj{"messages": []obj{}}) }, // already deleted
	})
	got, err := s.Activity(context.Background())
	if err != nil || len(got) != 1 || got[0].Msg.Text != "from the feed" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestPresenceAndSnooze(t *testing.T) {
	var presence string
	var snoozed string
	s, calls := apiServer(t, map[string]handler{
		"users.getPresence": func(f url.Values) any {
			if f.Get("user") != "UME" {
				t.Errorf("presence user: %q", f.Get("user"))
			}
			return ok(obj{"presence": "away"})
		},
		"users.setPresence": func(f url.Values) any { presence = f.Get("presence"); return ok(obj{}) },
		"dnd.setSnooze":     func(f url.Values) any { snoozed = f.Get("num_minutes"); return ok(obj{}) },
		"dnd.endSnooze":     func(url.Values) any { return ok(obj{}) },
	})
	ctx := context.Background()
	if away, err := s.Presence(ctx); err != nil || !away {
		t.Fatalf("presence: away %v, err %v", away, err)
	}
	if err := s.SetPresence(ctx, true); err != nil || presence != "away" {
		t.Fatalf("away: %q %v", presence, err)
	}
	if err := s.SetPresence(ctx, false); err != nil || presence != "auto" {
		t.Fatalf("active: %q %v", presence, err)
	}
	if err := s.SetSnooze(ctx, 20); err != nil || snoozed != "20" {
		t.Fatalf("snooze: %q %v", snoozed, err)
	}
	if err := s.SetSnooze(ctx, 0); err != nil || calls("dnd.endSnooze") != 1 {
		t.Fatalf("end snooze: %v", err)
	}
}

func TestParseQuery(t *testing.T) {
	q := ParseQuery("deploy failed in:#Tools from:@Ana  in: from:")
	if strings.Join(q.Words, " ") != "deploy failed in: from:" || q.In != "tools" || q.From != "ana" {
		t.Fatalf("%+v", q)
	}
	if q := ParseQuery("in:@bob"); q.In != "bob" || len(q.Words) != 0 {
		t.Fatalf("%+v", q)
	}
}

func TestNativeSectionsOrderFollowsTheLinks(t *testing.T) {
	convs := []Conversation{{ID: "A"}, {ID: "B"}, {ID: "C"}}
	out := applyNativeSections(convs, []nativeSection{{Name: "Top", IDs: []string{"C"}}, {Name: "Low", IDs: []string{"A", "ghost"}}})
	if out[0].ID != "C" || out[0].Section != "Top" || out[1].ID != "A" || out[2].ID != "B" || out[2].Section != "" {
		t.Fatalf("%+v", out)
	}
	if got := moveSelfDMEnd([]Conversation{{ID: "1", Kind: DM, Name: "you"}, {ID: "2"}}); got[1].Name != "you" {
		t.Fatalf("self DM not last: %+v", got)
	}
}

func jsonString(t *testing.T, v any) string {
	t.Helper()
	b, err := jsonMarshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var jsonMarshal = json.Marshal

func TestPeopleCacheSavesLookups(t *testing.T) {
	s, calls := apiServer(t, map[string]handler{"users.info": users})
	s.creds.TeamID = "T1"
	s.resolveUsers(context.Background(), []slackMessage{{User: "U1"}})
	if calls("users.info") != 1 {
		t.Fatal("no lookup")
	}
	fresh, calls2 := apiServer(t, map[string]handler{"users.info": users})
	fresh.creds.TeamID, fresh.me = "T1", s.me
	fresh.loadPeople()
	fresh.resolveUsers(context.Background(), []slackMessage{{User: "U1"}})
	if calls2("users.info") != 0 || fresh.user("U1") != s.user("U1") || fresh.urls["avatar:U1"] == "" {
		t.Fatalf("the cache was not used: %d calls, %q", calls2("users.info"), fresh.user("U1"))
	}
}

func TestUploadInThreeSteps(t *testing.T) {
	var base string
	var completed url.Values
	s, calls := apiServer(t, map[string]handler{
		"files.getUploadURLExternal": func(f url.Values) any {
			if f.Get("filename") != "shot.png" || f.Get("length") != "4" {
				t.Errorf("upload URL asked for %v", f)
			}
			return ok(obj{"upload_url": base + "/upload/v1/abc", "file_id": "F1"})
		},
		"/upload/v1/abc": func(url.Values) any { return "OK - 4" },
		"files.completeUploadExternal": func(f url.Values) any {
			completed = f
			return ok(obj{"files": []obj{{"id": "F1"}}})
		},
	})
	base = strings.TrimSuffix(s.apiBase, "/api/")
	got, err := s.Upload(context.Background(), "C1", "5.0", "look", Upload{Name: "shot.png", Data: []byte("\x89PNG")})
	if err != nil || got.TS != "" {
		t.Fatalf("upload: %+v %v", got, err)
	}
	if calls("/upload/v1/abc") != 1 {
		t.Fatal("the bytes were not sent")
	}
	if completed.Get("channel_id") != "C1" || completed.Get("thread_ts") != "5.0" || completed.Get("initial_comment") != "look" || !strings.Contains(completed.Get("files"), `"id":"F1"`) {
		t.Fatalf("complete: %v", completed)
	}
}
