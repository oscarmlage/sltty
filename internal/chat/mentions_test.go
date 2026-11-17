package chat

import (
	"context"
	"net/url"
	"testing"

	"github.com/gotd/td/tg"
)

func TestTelegramMentions(t *testing.T) {
	tt := newTestTelegram()
	tt.remember([]tg.UserClass{&tg.User{ID: 5, FirstName: "Bob", Username: "bobby", AccessHash: 55}}, nil)
	// Ana has no username: her name, marked as a mention of her. The emoji
	// before it checks the UTF-16 offset.
	text, ents := tt.mentions("😀 <@u2> and <@u5>, <@u99>")
	if text != "😀 Ana López and @bobby, <@u99>" {
		t.Fatalf("text %q", text)
	}
	if len(ents) != 1 {
		t.Fatalf("entities %v", ents)
	}
	e := ents[0].(*tg.InputMessageEntityMentionName)
	if e.Offset != 3 || e.Length != 9 || e.UserID.(*tg.InputUser).UserID != 2 || e.UserID.(*tg.InputUser).AccessHash != 22 {
		t.Fatalf("entity %+v", e)
	}
	if text, ents := tt.mentions("no mentions"); text != "no mentions" || ents != nil {
		t.Fatal("plain text changed")
	}
	if n, ok := tt.PersonName("u5"); !ok || n != "bobby" {
		t.Fatalf("person name %q", n)
	}
	// A private chat's member is the other person, no call needed.
	if p, _ := tt.People(context.Background(), "u2", ""); len(p) != 1 || p[0].Name != "Ana López" || p[0].ID != "u2" {
		t.Fatalf("people %+v", p)
	}
}

func TestSlackPeopleFromTheDirectory(t *testing.T) {
	s, calls := apiServer(t, map[string]handler{
		"search.modules": func(f url.Values) any {
			if f.Get("module") != "people" || f.Get("query") != "ana" {
				t.Errorf("search %v", f)
			}
			return ok(obj{"items": []obj{
				{"id": "U2", "username": "ana.lopez", "profile": obj{"real_name": "Ana López", "image_72": "https://a/2"}},
				{"id": "UME", "username": "me"},
				{"id": "U3", "username": "gone", "deleted": true},
			}})
		},
	})
	got, err := s.People(context.Background(), "C1", "ana")
	if err != nil || len(got) != 1 || got[0] != (Person{ID: "U2", Name: "ana.lopez", Full: "Ana López"}) {
		t.Fatalf("people %+v %v", got, err)
	}
	if n, ok := s.PersonName("U2"); !ok || n != "ana.lopez" {
		t.Fatalf("now known: %q", n)
	}
	if p, _ := s.People(context.Background(), "C1", " "); p != nil || calls("search.modules") != 1 {
		t.Fatal("an empty query asks nothing")
	}
}
