package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/gotd/td/tg"
)

func newTestTelegram() *Telegram {
	self := &tg.User{ID: 1, FirstName: "Me", Self: true}
	t := newTelegramState()
	t.self = self
	t.remember([]tg.UserClass{self, &tg.User{ID: 2, FirstName: "Ana", LastName: "López", AccessHash: 22,
		Status: &tg.UserStatusOnline{}}},
		[]tg.ChatClass{&tg.Chat{ID: 3, Title: "Family", ParticipantsCount: 4},
			&tg.Channel{ID: 4, Title: "News", AccessHash: 44, Broadcast: true, Username: "news"}})
	return t
}

func TestEntitiesToMrkdwn(t *testing.T) {
	// "ñ" and the emoji check UTF-16 offsets.
	text := "ñ 😀 bold it code link"
	ents := []tg.MessageEntityClass{
		&tg.MessageEntityBold{Offset: 5, Length: 4},
		&tg.MessageEntityItalic{Offset: 10, Length: 2},
		&tg.MessageEntityCode{Offset: 13, Length: 4},
		&tg.MessageEntityTextURL{Offset: 18, Length: 4, URL: "https://x.y"},
	}
	want := "ñ 😀 *bold* _it_ `code` <https://x.y|link>"
	if got := entitiesToMrkdwn(text, ents); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	// Marks the renderer could not read back are dropped, unsafe links too.
	got := entitiesToMrkdwn(" a b\nc", []tg.MessageEntityClass{&tg.MessageEntityBold{Offset: 0, Length: 6},
		&tg.MessageEntityTextURL{Offset: 1, Length: 1, URL: "file:///etc/passwd"}})
	if got != " a b\nc" {
		t.Fatalf("got %q", got)
	}
	if got := entitiesToMrkdwn("x <b> y", nil); strings.Contains(got, "<b>") {
		t.Fatalf("a literal <b> must not read as a token: %q", got)
	}
	if got := entitiesToMrkdwn("go\nfmt", []tg.MessageEntityClass{&tg.MessageEntityPre{Offset: 0, Length: 6}}); got != "```\ngo\nfmt\n```" {
		t.Fatalf("pre: %q", got)
	}
}

func TestConvertMessage(t *testing.T) {
	tt := newTestTelegram()
	parent := &tg.Message{ID: 9, FromID: &tg.PeerUser{UserID: 1}, Out: true, PeerID: &tg.PeerChat{ChatID: 3}, Message: "dinner?"}
	m := &tg.Message{ID: 10, FromID: &tg.PeerUser{UserID: 2}, PeerID: &tg.PeerChat{ChatID: 3}, Date: 1700000000,
		Message: "yes \x1b[31m", EditDate: 1700000100,
		ReplyTo: &tg.MessageReplyHeader{ReplyToMsgID: 9}}
	m.SetReactions(tg.MessageReactions{Results: []tg.ReactionCount{{Reaction: &tg.ReactionEmoji{Emoticon: "👍"}, Count: 2}}})
	(&m.Reactions.Results[0]).SetChosenOrder(0)
	got, ok := tt.convert(m, map[int]tg.MessageClass{9: parent})
	if !ok {
		t.Fatal("not converted")
	}
	if got.TS != "10" || got.User != "Ana López" || got.UserID != "u2" || !got.Edited || strings.Contains(got.Text, "\x1b") {
		t.Fatalf("message: %+v", got)
	}
	if len(got.Context) != 1 || got.Context[0] != "↩ Me: dinner?" {
		t.Fatalf("reply quote: %q", got.Context)
	}
	if len(got.Reactions) != 1 || got.Reactions[0].Emoji != "👍" || !got.Reactions[0].Mine || got.Reactions[0].Count != 2 {
		t.Fatalf("reactions: %+v", got.Reactions)
	}
	post := &tg.Message{ID: 5, PeerID: &tg.PeerChannel{ChannelID: 4}, Post: true, Message: "breaking", PostAuthor: "Bob"}
	if got, _ := tt.convert(post, nil); got.User != "News (Bob)" || got.UserID != "c4" {
		t.Fatalf("channel post: %+v", got)
	}
}

func TestConvertPhoto(t *testing.T) {
	tt := newTestTelegram()
	p := &tg.Photo{ID: 7, AccessHash: 70, Sizes: []tg.PhotoSizeClass{
		&tg.PhotoSize{Type: "m", W: 320, H: 240}, &tg.PhotoSize{Type: "x", W: 800, H: 600}, &tg.PhotoSizeProgressive{Type: "y", W: 1280, H: 960}}}
	m := &tg.Message{ID: 1, PeerID: &tg.PeerUser{UserID: 2}, FromID: &tg.PeerUser{UserID: 2}, Media: &tg.MessageMediaPhoto{Photo: p}}
	got, _ := tt.convert(m, nil)
	if len(got.Files) != 1 || !got.Files[0].IsImage() || got.Files[0].W != 1280 {
		t.Fatalf("files: %+v", got.Files)
	}
	f := tt.files[got.Files[0].ID]
	if f.thumb.(*tg.InputPhotoFileLocation).ThumbSize != "x" || f.full.(*tg.InputPhotoFileLocation).ThumbSize != "y" {
		t.Fatalf("locations: %+v", f)
	}
}

func TestConversationsAndPeers(t *testing.T) {
	tt := newTestTelegram()
	for _, tc := range []struct {
		d    tg.Dialog
		name string
		kind Kind
	}{
		{tg.Dialog{Peer: &tg.PeerUser{UserID: 2}, UnreadCount: 3}, "Ana López", DM},
		{tg.Dialog{Peer: &tg.PeerUser{UserID: 1}}, "Saved Messages", DM},
		{tg.Dialog{Peer: &tg.PeerChat{ChatID: 3}}, "Family", Channel},
		{tg.Dialog{Peer: &tg.PeerChannel{ChannelID: 4}}, "News", Channel},
	} {
		c, ok := tt.conversation(tc.d)
		if !ok || c.Name != tc.name || c.Kind != tc.kind {
			t.Errorf("%v: %+v", tc.d.Peer, c)
		}
		if _, err := tt.inputPeer(c.ID); err != nil {
			t.Errorf("%s: %v", c.ID, err)
		}
	}
	if c, _ := tt.conversation(tg.Dialog{Peer: &tg.PeerUser{UserID: 2}, UnreadCount: 3}); !c.Online || c.Unread != 3 {
		t.Fatalf("dm: %+v", c)
	}
	if p, _ := tt.inputPeer("c4"); p.(*tg.InputPeerChannel).AccessHash != 44 {
		t.Fatal("channel access hash lost")
	}
	if _, err := tt.inputPeer("u99"); err == nil {
		t.Fatal("an unknown user must be an error")
	}
	if !tt.isBroadcast("c4") || tt.isBroadcast("g3") {
		t.Fatal("isBroadcast")
	}
}

func TestLiveUpdates(t *testing.T) {
	tt := newTestTelegram()
	tt.pinned["u2"] = true
	d := tg.NewUpdateDispatcher()
	tt.handle(d)
	ctx := context.Background()
	next := func() Event {
		t.Helper()
		select {
		case e := <-tt.Events():
			return e
		default:
			t.Fatal("no event")
		}
		return Event{}
	}
	newUser := &tg.User{ID: 5, FirstName: "Zoe", AccessHash: 55}
	msg := &tg.Message{ID: 77, FromID: &tg.PeerUser{UserID: 5}, PeerID: &tg.PeerUser{UserID: 5}, Message: "hola",
		ReplyTo: &tg.MessageReplyHeader{ReplyToMsgID: 70}}
	if err := d.Handle(ctx, &tg.Updates{Users: []tg.UserClass{newUser},
		Updates: []tg.UpdateClass{&tg.UpdateNewMessage{Message: msg}}}); err != nil {
		t.Fatal(err)
	}
	e := next()
	if e.Kind != EvMessage || e.ConvID != "u5" || e.Msg.User != "Zoe" || e.RootTS != "70" || e.Conv == nil || e.Conv.Name != "Zoe" {
		t.Fatalf("new message: %+v", e)
	}
	// A deletion names only the IDs: the conversation comes from what was seen.
	_ = d.Handle(ctx, &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateDeleteMessages{Messages: []int{77}}}})
	if e := next(); e.Kind != EvDeleted || e.ConvID != "u5" || e.TS[0] != "77" {
		t.Fatalf("delete: %+v", e)
	}
	_ = d.Handle(ctx, &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateReadChannelInbox{ChannelID: 4, StillUnreadCount: 2}}})
	if e := next(); e.Kind != EvRead || e.ConvID != "c4" || e.Unread != 2 {
		t.Fatalf("read: %+v", e)
	}
	_ = d.Handle(ctx, &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateUserStatus{UserID: 2, Status: &tg.UserStatusOffline{}}}})
	if e := next(); e.Kind != EvPresence || e.ConvID != "u2" || e.Online {
		t.Fatalf("presence: %+v", e)
	}
	_ = d.Handle(ctx, &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateNewMessage{Message: &tg.Message{ID: 78,
		FromID: &tg.PeerUser{UserID: 2}, PeerID: &tg.PeerUser{UserID: 2}, Message: "pinned chat"}}}})
	if e := next(); e.Conv == nil || e.Conv.Section != "Pinned" {
		t.Fatalf("pinned: %+v", e.Conv)
	}
}

func TestQRCodeDraws(t *testing.T) {
	var b strings.Builder
	if err := showQR(&b, "tg://login?token=abc"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "▀") || !strings.Contains(b.String(), "Link Desktop Device") {
		t.Fatalf("qr:\n%s", b.String())
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"Coctel Salseo\u3164\u3164\u3164": "Coctel Salseo",
		"  a\tb\n c  ":                    "a b c",
		"x\u200by":                        "xy",
		"Los Lage 🏠":                      "Los Lage 🏠",
	} {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlaceAndTyping(t *testing.T) {
	tt := newTestTelegram()
	tt.archived["g3"], tt.pinned["u2"], tt.muted["c4"] = true, true, true
	for id, want := range map[string]string{"g3": "Archived", "u2": "Pinned", "c4": "Chats"} {
		c := Conversation{ID: id}
		tt.place(&c)
		if c.Section != want {
			t.Errorf("%s: %q", id, c.Section)
		}
		if id == "g3" && !c.Folded || id == "u2" && !c.Pinned || id == "c4" && !c.Muted {
			t.Errorf("%s: %+v", id, c)
		}
	}
	d := tg.NewUpdateDispatcher()
	tt.handle(d)
	ctx := context.Background()
	_ = d.Handle(ctx, &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateChatUserTyping{ChatID: 3, FromID: &tg.PeerUser{UserID: 2}, Action: &tg.SendMessageRecordAudioAction{}}}})
	if e := <-tt.Events(); e.Kind != EvTyping || e.ConvID != "g3" || e.User != "Ana López" || e.Action != "recording a voice message" {
		t.Fatalf("typing: %+v", e)
	}
	_ = d.Handle(ctx, &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateUserTyping{UserID: 1, Action: &tg.SendMessageTypingAction{}}}})
	select {
	case e := <-tt.Events():
		t.Fatalf("one's own typing reported: %+v", e)
	default:
	}
	// Unmuted archived chat: a message brings it back to Chats.
	_ = d.Handle(ctx, &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateNewMessage{Message: &tg.Message{ID: 9,
		FromID: &tg.PeerUser{UserID: 2}, PeerID: &tg.PeerChat{ChatID: 3}, Message: "hey"}}}})
	if e := <-tt.Events(); e.Conv == nil || e.Conv.Section != "Chats" {
		t.Fatalf("not unarchived: %+v", e.Conv)
	}
	_ = d.Handle(ctx, &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateFolderPeers{FolderPeers: []tg.FolderPeer{{Peer: &tg.PeerChat{ChatID: 3}, FolderID: 1}}}}})
	if e := <-tt.Events(); e.Kind != EvConv || e.Conv.Section != "Archived" {
		t.Fatalf("archived: %+v", e)
	}
}
