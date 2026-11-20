package chat

// Mentions on Telegram: whom to offer (the chat's members, the contacts
// search as the user types) and how a mention is sent: @username for
// whoever has one, Telegram links it; otherwise the person's name marked
// as a mention of them (an entity, at its offset in UTF-16 units).

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/gotd/td/tg"

	"github.com/oscarmlage/sltty/internal/diag"
)

// mentionToken is a mention as the TUI writes it in the text.
var mentionToken = regexp.MustCompile(`<@u([0-9]+)>`)

func (t *Telegram) person(u *tg.User) Person {
	full := userName(u)
	name := u.Username
	if name == "" {
		name = full
	}
	return Person{ID: "u" + strconv.FormatInt(u.ID, 10), Name: name, Full: full}
}

// Members lists every member of a Telegram group or supergroup. Broadcast
// channels intentionally have no member list: Telegram does not expose their
// subscribers to ordinary members.
func (t *Telegram) Members(ctx context.Context, convID string) ([]Person, error) {
	k, n, err := splitConv(convID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	switch k {
	case 'g':
		full, err := t.api.MessagesGetFullChat(ctx, n)
		if err != nil {
			return nil, err
		}
		t.remember(full.Users, full.Chats)
		if f, ok := full.FullChat.(*tg.ChatFull); ok {
			if ps, ok := f.Participants.(*tg.ChatParticipants); ok {
				for _, p := range ps.Participants {
					ids = append(ids, p.GetUserID())
				}
			}
		}
	case 'c':
		if t.isBroadcast(convID) {
			return nil, fmt.Errorf("Telegram does not expose a channel's subscribers")
		}
		peer, err := t.inputPeer(convID)
		if err != nil {
			return nil, err
		}
		p, ok := peer.(*tg.InputPeerChannel)
		if !ok {
			return nil, fmt.Errorf("not a Telegram group")
		}
		for offset := 0; ; {
			r, err := t.api.ChannelsGetParticipants(ctx, &tg.ChannelsGetParticipantsRequest{
				Channel: &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
				Filter:  &tg.ChannelParticipantsRecent{}, Offset: offset, Limit: 200,
			})
			if err != nil {
				return nil, err
			}
			ps, ok := r.(*tg.ChannelsChannelParticipants)
			if !ok {
				break
			}
			t.remember(ps.Users, ps.Chats)
			for _, u := range ps.Users {
				ids = append(ids, u.GetID())
			}
			offset += len(ps.Participants)
			if offset >= ps.Count || len(ps.Participants) == 0 {
				break
			}
		}
	default:
		return nil, fmt.Errorf("open a Telegram group first")
	}
	return t.memberPeople(ids), nil
}

func (t *Telegram) memberPeople(ids []int64) []Person {
	t.mu.Lock()
	defer t.mu.Unlock()
	seen := map[int64]bool{}
	out := make([]Person, 0, len(ids))
	for _, id := range ids {
		u, ok := t.users[id]
		if !ok || u.Deleted || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, t.person(u))
	}
	slices.SortFunc(out, func(a, b Person) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out
}

func (t *Telegram) People(ctx context.Context, convID, query string) ([]Person, error) {
	ids := t.members(ctx, convID)
	if q := strings.TrimSpace(query); q != "" {
		r, err := t.api.ContactsSearch(ctx, &tg.ContactsSearchRequest{Q: q, Limit: 10})
		if err != nil {
			diag.Printf("telegram contacts.search: %v", err)
		} else {
			t.remember(r.Users, r.Chats)
			for _, u := range r.Users {
				if u, ok := u.(*tg.User); ok {
					ids = append(ids, u.ID)
				}
			}
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	seen := map[int64]bool{t.self.ID: true}
	var out []Person
	for _, id := range ids {
		u, ok := t.users[id]
		if !ok || seen[id] || u.Deleted || u.Bot && u.Username == "" {
			continue
		}
		seen[id] = true
		out = append(out, t.person(u))
	}
	return out, nil
}

// members are the users of a chat (fetched once): the other person of a
// private chat, a group's members, a supergroup's recent ones.
func (t *Telegram) members(ctx context.Context, convID string) []int64 {
	t.mu.Lock()
	ids, ok := t.memberIDs[convID]
	t.mu.Unlock()
	if ok {
		return ids
	}
	k, n, err := splitConv(convID)
	if err != nil {
		return nil
	}
	switch k {
	case 'u':
		ids = []int64{n}
	case 'g':
		full, err := t.api.MessagesGetFullChat(ctx, n)
		if err != nil {
			diag.Printf("telegram members of %s: %v", convID, err)
			return nil
		}
		t.remember(full.Users, full.Chats)
		if f, ok := full.FullChat.(*tg.ChatFull); ok {
			if ps, ok := f.Participants.(*tg.ChatParticipants); ok {
				for _, p := range ps.Participants {
					ids = append(ids, p.GetUserID())
				}
			}
		}
	case 'c':
		if t.isBroadcast(convID) {
			return nil // a channel's subscribers are not for mentioning
		}
		peer, err := t.inputPeer(convID)
		if err != nil {
			return nil
		}
		p, ok := peer.(*tg.InputPeerChannel)
		if !ok {
			return nil
		}
		r, err := t.api.ChannelsGetParticipants(ctx, &tg.ChannelsGetParticipantsRequest{
			Channel: &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
			Filter:  &tg.ChannelParticipantsRecent{}, Limit: 200})
		if err != nil {
			diag.Printf("telegram members of %s: %v", convID, err)
			return nil
		}
		if ps, ok := r.(*tg.ChannelsChannelParticipants); ok {
			t.remember(ps.Users, ps.Chats)
			for _, u := range ps.Users {
				ids = append(ids, u.GetID())
			}
		}
	}
	t.mu.Lock()
	t.memberIDs[convID] = ids
	t.mu.Unlock()
	return ids
}

func (t *Telegram) PersonName(id string) (string, bool) {
	k, n, err := splitConv(id)
	if err != nil || k != 'u' {
		return "", false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	u, ok := t.users[n]
	if !ok {
		return "", false
	}
	return t.person(u).Name, true
}

// mentions turns the <@u…> mentions of text into Telegram's: @username, or
// the name with an entity pointing at the person.
func (t *Telegram) mentions(text string) (string, []tg.MessageEntityClass) {
	if !strings.Contains(text, "<@u") {
		return text, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var b strings.Builder
	var ents []tg.MessageEntityClass
	units, last := 0, 0 // UTF-16 units written so far
	write := func(s string) {
		b.WriteString(s)
		units += len(utf16.Encode([]rune(s)))
	}
	for _, loc := range mentionToken.FindAllStringSubmatchIndex(text, -1) {
		write(text[last:loc[0]])
		last = loc[1]
		id, _ := strconv.ParseInt(text[loc[2]:loc[3]], 10, 64)
		u, ok := t.users[id]
		switch {
		case !ok:
			write(text[loc[0]:loc[1]]) // nobody known: as typed
		case u.Username != "":
			write("@" + u.Username)
		default:
			name := userName(u)
			at := units
			write(name)
			ents = append(ents, &tg.InputMessageEntityMentionName{Offset: at, Length: units - at,
				UserID: &tg.InputUser{UserID: u.ID, AccessHash: u.AccessHash}})
		}
	}
	write(text[last:])
	return b.String(), ents
}
