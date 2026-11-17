package chat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"

	"github.com/oscarmlage/sltty/internal/diag"
)

const (
	maxDialogs  = 300 // the sidebar's length at most
	historySize = 60
)

// loadDialogs fetches the chat list, the main folder then the archive,
// reusing it for a few seconds: Conversations and Activity both want it at
// start.
func (t *Telegram) loadDialogs(ctx context.Context) ([]tg.Dialog, error) {
	t.dmu.Lock()
	defer t.dmu.Unlock()
	if t.dialogs != nil && time.Since(t.dialogAt) < 10*time.Second {
		return t.dialogs, nil
	}
	main, err := t.loadFolder(ctx, 0, maxDialogs)
	if err != nil {
		return nil, err
	}
	archive, err := t.loadFolder(ctx, 1, maxArchived)
	if err != nil {
		diag.Printf("telegram archive: %v", err)
	}
	out := append(main, archive...)
	t.dialogs, t.dialogAt = out, time.Now()
	now := int(time.Now().Unix())
	t.mu.Lock()
	t.pinned, t.archived, t.muted = map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, d := range out {
		id := convID(d.Peer)
		t.pinned[id] = d.Pinned
		t.archived[id] = d.FolderID == 1
		until, _ := d.NotifySettings.GetMuteUntil()
		t.muted[id] = until > now
	}
	t.mu.Unlock()
	return out, nil
}

const maxArchived = 100

func (t *Telegram) loadFolder(ctx context.Context, folder, limit int) ([]tg.Dialog, error) {
	var out []tg.Dialog
	req := &tg.MessagesGetDialogsRequest{OffsetPeer: &tg.InputPeerEmpty{}, Limit: 100}
	req.SetFolderID(folder)
	for len(out) < limit {
		res, err := t.api.MessagesGetDialogs(ctx, req)
		if err != nil {
			return out, err
		}
		mod, ok := res.AsModified()
		if !ok {
			break
		}
		t.remember(mod.GetUsers(), mod.GetChats())
		dialogs := mod.GetDialogs()
		var batch []tg.Dialog
		for _, d := range dialogs {
			if d, ok := d.(*tg.Dialog); ok {
				// FolderID is supplied by Telegram with each dialog. Do not
				// replace it with the requested folder: a response may contain
				// dialogs from another folder (notably pinned dialogs).
				batch = append(batch, *d)
			}
		}
		out = append(out, batch...)
		if _, all := res.(*tg.MessagesDialogs); all || len(dialogs) < req.Limit || len(batch) == 0 {
			break
		}
		// The next page starts after the last dialog's top message.
		lastD := batch[len(batch)-1]
		date := 0
		for _, m := range mod.GetMessages() {
			if m.GetID() == lastD.TopMessage && convID(peerOf(m)) == convID(lastD.Peer) {
				if mm, ok := m.(*tg.Message); ok {
					date = mm.Date
				} else if ms, ok := m.(*tg.MessageService); ok {
					date = ms.Date
				}
			}
		}
		peer, err := t.inputPeer(convID(lastD.Peer))
		if err != nil || date == 0 {
			break
		}
		req.OffsetDate, req.OffsetID, req.OffsetPeer = date, lastD.TopMessage, peer
		// Pinned dialogs are included only in the first page. Asking for
		// them again can make Telegram return them with a different folder
		// placement instead of advancing through the remaining dialogs.
		req.SetExcludePinned(true)
	}
	return out, nil
}

func peerOf(m tg.MessageClass) tg.PeerClass {
	switch m := m.(type) {
	case *tg.Message:
		return m.PeerID
	case *tg.MessageService:
		return m.PeerID
	}
	return nil
}

func (t *Telegram) Conversations(ctx context.Context) ([]Conversation, error) {
	dialogs, err := t.loadDialogs(ctx)
	if err != nil {
		return nil, err
	}
	var pinned, rest, archived []Conversation
	for _, d := range dialogs {
		c, ok := t.conversation(d)
		if !ok {
			continue
		}
		t.place(&c)
		switch {
		case c.Folded:
			archived = append(archived, c)
		case c.Pinned:
			pinned = append(pinned, c)
		default:
			rest = append(rest, c)
		}
	}
	return append(append(pinned, rest...), archived...), nil
}

// place puts a conversation in its section: Pinned, Chats (most recent
// first) or Archived (folded, as in the official apps).
func (t *Telegram) place(c *Conversation) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c.Muted = t.muted[c.ID]
	switch {
	case t.archived[c.ID]:
		c.Section, c.SectionEmoji, c.Folded = "Archived", "🗄", true
	case t.pinned[c.ID]:
		c.Section, c.SectionEmoji, c.Pinned = "Pinned", "📌", true
	default:
		c.Section = "Chats"
	}
}

func (t *Telegram) NoThreadsView() {}

func (t *Telegram) conversation(d tg.Dialog) (Conversation, bool) {
	c := Conversation{ID: convID(d.Peer), Unread: d.UnreadCount}
	if d.UnreadMark && c.Unread == 0 {
		c.Unread = 1
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.marked[c.ID] = d.UnreadMark
	switch p := d.Peer.(type) {
	case *tg.PeerUser:
		u, ok := t.users[p.UserID]
		if !ok {
			return c, false
		}
		c.Kind, c.Name = DM, userName(u)
		if u.Self {
			c.Name = "Saved Messages"
		}
		_, c.Online = u.Status.(*tg.UserStatusOnline)
		switch {
		case u.Bot:
			c.Topic = "bot"
		case u.Username != "":
			c.Topic = "@" + u.Username
		}
	case *tg.PeerChat:
		ch, ok := t.chats[p.ChatID]
		if !ok || ch.Deactivated {
			return c, false
		}
		c.Kind, c.Name = Channel, cleanName(ch.Title)
		c.Topic = fmt.Sprintf("group · %d members", ch.ParticipantsCount)
	case *tg.PeerChannel:
		ch, ok := t.channels[p.ChannelID]
		if !ok {
			return c, false
		}
		c.Kind, c.Name = Channel, cleanName(ch.Title)
		kind := "group"
		if ch.Broadcast {
			kind = "channel"
		}
		if ch.Forum {
			kind = "forum"
		}
		if ch.Username != "" {
			kind += " · @" + ch.Username
		}
		c.Topic = kind
	default:
		return c, false
	}
	return c, true
}

// messages converts a batch, oldest first, remembering its people.
func (t *Telegram) messages(res tg.MessagesMessagesClass) []Message {
	mod, ok := res.AsModified()
	if !ok {
		return nil
	}
	t.remember(mod.GetUsers(), mod.GetChats())
	raw := mod.GetMessages()
	byID := map[int]tg.MessageClass{}
	for _, m := range raw {
		byID[m.GetID()] = m
	}
	var out []Message
	for i := len(raw) - 1; i >= 0; i-- {
		if m, ok := t.convert(raw[i], byID); ok {
			out = append(out, m)
		}
	}
	return out
}

func (t *Telegram) History(ctx context.Context, id string) ([]Message, error) {
	peer, err := t.inputPeer(id)
	if err != nil {
		return nil, err
	}
	res, err := t.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, Limit: historySize})
	if err != nil {
		return nil, err
	}
	return t.messages(res), nil
}

func msgID(ts string) (int, error) {
	n, err := strconv.Atoi(ts)
	if err != nil {
		return 0, fmt.Errorf("bad message id %q", ts)
	}
	return n, nil
}

// message fetches one message of a conversation.
func (t *Telegram) message(ctx context.Context, id, ts string) (Message, error) {
	n, err := msgID(ts)
	if err != nil {
		return Message{}, err
	}
	peer, err := t.inputPeer(id)
	if err != nil {
		return Message{}, err
	}
	ids := []tg.InputMessageClass{&tg.InputMessageID{ID: n}}
	var res tg.MessagesMessagesClass
	if ch, ok := peer.(*tg.InputPeerChannel); ok {
		res, err = t.api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash}, ID: ids})
	} else {
		res, err = t.api.MessagesGetMessages(ctx, ids)
	}
	if err != nil {
		return Message{}, err
	}
	ms := t.messages(res)
	if len(ms) == 0 {
		return Message{}, fmt.Errorf("message %s not found", ts)
	}
	return ms[0], nil
}

func (t *Telegram) Replies(ctx context.Context, id, rootTS string) ([]Message, error) {
	root, err := t.message(ctx, id, rootTS)
	if err != nil {
		return nil, err
	}
	n, _ := msgID(rootTS)
	peer, err := t.inputPeer(id)
	if err != nil {
		return nil, err
	}
	if _, ok := peer.(*tg.InputPeerChannel); !ok {
		// Private chats and basic groups have no threads: the replies to
		// the message among the recent history stand in.
		res, err := t.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, Limit: 100})
		if err != nil {
			return nil, err
		}
		out := []Message{root}
		mod, ok := res.AsModified()
		if !ok {
			return out, nil
		}
		raw := mod.GetMessages()
		for i := len(raw) - 1; i >= 0; i-- {
			m, ok := raw[i].(*tg.Message)
			if !ok {
				continue
			}
			if h, ok := m.ReplyTo.(*tg.MessageReplyHeader); ok && h.ReplyToMsgID == n {
				out = append(out, t.convertMessage(m, nil))
			}
		}
		return out, nil
	}
	res, err := t.api.MessagesGetReplies(ctx, &tg.MessagesGetRepliesRequest{Peer: peer, MsgID: n, Limit: 100})
	if err != nil {
		return nil, err
	}
	return append([]Message{root}, t.messages(res)...), nil
}

func randomID() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int64(binary.LittleEndian.Uint64(b[:]))
}

func (t *Telegram) Send(ctx context.Context, id, rootTS, text string) (Message, error) {
	peer, err := t.inputPeer(id)
	if err != nil {
		return Message{}, err
	}
	req := &tg.MessagesSendMessageRequest{Peer: peer, Message: text, RandomID: randomID()}
	if rootTS != "" {
		root, err := msgID(rootTS)
		if err != nil {
			return Message{}, err
		}
		reply := &tg.InputReplyToMessage{ReplyToMsgID: root}
		if _, ok := peer.(*tg.InputPeerChannel); ok {
			reply.SetTopMsgID(root) // a supergroup thread
		}
		// A channel post's comments live in its discussion group.
		if t.isBroadcast(id) {
			d, err := t.api.MessagesGetDiscussionMessage(ctx, &tg.MessagesGetDiscussionMessageRequest{Peer: peer, MsgID: root})
			if err != nil {
				return Message{}, fmt.Errorf("comments: %w", err)
			}
			t.remember(d.Users, d.Chats)
			if len(d.Messages) == 0 {
				return Message{}, fmt.Errorf("this post has no comments")
			}
			first := d.Messages[len(d.Messages)-1]
			if req.Peer, err = t.inputPeer(convID(peerOf(first))); err != nil {
				return Message{}, err
			}
			reply = &tg.InputReplyToMessage{ReplyToMsgID: first.GetID()}
			reply.SetTopMsgID(first.GetID())
		}
		req.SetReplyTo(reply)
	}
	upd, err := t.api.MessagesSendMessage(ctx, req)
	if err != nil {
		return Message{}, err
	}
	if m, ok := t.fromUpdates(upd); ok {
		return m, nil
	}
	if s, ok := upd.(*tg.UpdateShortSentMessage); ok {
		return Message{TS: strconv.Itoa(s.ID), User: t.Me(), UserID: "u" + strconv.FormatInt(t.self.ID, 10),
			Text: Sanitize(text), At: time.Unix(int64(s.Date), 0)}, nil
	}
	return Message{TS: "0", User: t.Me(), Text: Sanitize(text), At: time.Now()}, nil
}

func (t *Telegram) isBroadcast(id string) bool {
	k, n, err := splitConv(id)
	if err != nil || k != 'c' {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	ch, ok := t.channels[n]
	return ok && ch.Broadcast
}

// fromUpdates finds the new or edited message an action produced.
func (t *Telegram) fromUpdates(u tg.UpdatesClass) (Message, bool) {
	var ups []tg.UpdateClass
	switch u := u.(type) {
	case *tg.Updates:
		t.remember(u.Users, u.Chats)
		ups = u.Updates
	case *tg.UpdatesCombined:
		t.remember(u.Users, u.Chats)
		ups = u.Updates
	case *tg.UpdateShort:
		ups = []tg.UpdateClass{u.Update}
	}
	for _, up := range ups {
		var m tg.MessageClass
		switch up := up.(type) {
		case *tg.UpdateNewMessage:
			m = up.Message
		case *tg.UpdateNewChannelMessage:
			m = up.Message
		case *tg.UpdateEditMessage:
			m = up.Message
		case *tg.UpdateEditChannelMessage:
			m = up.Message
		}
		if m != nil {
			if out, ok := t.convert(m, nil); ok {
				return out, true
			}
		}
	}
	return Message{}, false
}

func (t *Telegram) Edit(ctx context.Context, id, ts, text string) (Message, error) {
	n, err := msgID(ts)
	if err != nil {
		return Message{}, err
	}
	peer, err := t.inputPeer(id)
	if err != nil {
		return Message{}, err
	}
	req := &tg.MessagesEditMessageRequest{Peer: peer, ID: n}
	req.SetMessage(text)
	upd, err := t.api.MessagesEditMessage(ctx, req)
	if err != nil {
		return Message{}, err
	}
	if m, ok := t.fromUpdates(upd); ok {
		return m, nil
	}
	return t.message(ctx, id, ts)
}

// React toggles the user's emoji. Without Premium a message takes one
// reaction per person, so a new one replaces the old.
func (t *Telegram) React(ctx context.Context, id, ts, emoji string) (Message, error) {
	n, err := msgID(ts)
	if err != nil {
		return Message{}, err
	}
	peer, err := t.inputPeer(id)
	if err != nil {
		return Message{}, err
	}
	cur, err := t.message(ctx, id, ts)
	if err != nil {
		return Message{}, err
	}
	var mine []string
	for _, r := range cur.Reactions {
		if r.Mine {
			mine = append(mine, r.Emoji)
		}
	}
	var next []tg.ReactionClass
	if !slices.Contains(mine, emoji) {
		next = []tg.ReactionClass{&tg.ReactionEmoji{Emoticon: emoji}}
	}
	req := &tg.MessagesSendReactionRequest{Peer: peer, MsgID: n}
	req.SetReaction(next)
	if _, err := t.api.MessagesSendReaction(ctx, req); err != nil {
		return Message{}, err
	}
	return t.message(ctx, id, ts)
}

// findConv resolves an in: or from: name against the chat list.
func (t *Telegram) findConv(ctx context.Context, name string, dmOnly bool) (string, bool) {
	norm := func(s string) string {
		return strings.NewReplacer(" ", "", "-", "", "_", "", "@", "", "#", "").Replace(strings.ToLower(s))
	}
	want := norm(name)
	convs, err := t.Conversations(ctx)
	if err != nil {
		return "", false
	}
	for _, c := range convs {
		if dmOnly && c.Kind != DM {
			continue
		}
		if norm(c.Name) == want || norm(c.Topic) == want || strings.HasSuffix(norm(c.Topic), want) && want != "" && strings.Contains(c.Topic, "@") {
			return c.ID, true
		}
	}
	for _, c := range convs {
		if (!dmOnly || c.Kind == DM) && strings.Contains(norm(c.Name), want) {
			return c.ID, true
		}
	}
	return "", false
}

func (t *Telegram) Search(ctx context.Context, query string) ([]Hit, error) {
	q := ParseQuery(query)
	text := strings.Join(q.Words, " ")
	var res tg.MessagesMessagesClass
	var err error
	if q.In != "" {
		id, ok := t.findConv(ctx, q.In, false)
		if !ok {
			return nil, fmt.Errorf("no chat matches in:%s", q.In)
		}
		peer, err := t.inputPeer(id)
		if err != nil {
			return nil, err
		}
		req := &tg.MessagesSearchRequest{Peer: peer, Q: text, Filter: &tg.InputMessagesFilterEmpty{}, Limit: 50}
		if q.From != "" {
			uid, ok := t.findConv(ctx, q.From, true)
			if !ok {
				return nil, fmt.Errorf("no one matches from:%s", q.From)
			}
			from, err := t.inputPeer(uid)
			if err != nil {
				return nil, err
			}
			req.SetFromID(from)
		}
		res, err = t.api.MessagesSearch(ctx, req)
		if err != nil {
			return nil, err
		}
	} else {
		if text == "" {
			return nil, nil
		}
		res, err = t.api.MessagesSearchGlobal(ctx, &tg.MessagesSearchGlobalRequest{
			Q: text, Filter: &tg.InputMessagesFilterEmpty{}, OffsetPeer: &tg.InputPeerEmpty{}, Limit: 50})
		if err != nil {
			return nil, err
		}
	}
	mod, ok := res.AsModified()
	if !ok {
		return nil, nil
	}
	t.remember(mod.GetUsers(), mod.GetChats())
	var hits []Hit
	for _, raw := range mod.GetMessages() {
		m, ok := t.convert(raw, nil)
		if !ok {
			continue
		}
		if q.From != "" && q.In == "" && !strings.Contains(strings.ToLower(m.User), strings.ToLower(q.From)) {
			continue
		}
		hits = append(hits, Hit{ConvID: convID(peerOf(raw)), Msg: m})
	}
	return hits, nil
}

// Threads: Telegram has no list of the threads one takes part in.
func (t *Telegram) Threads(context.Context) ([]ThreadSummary, error) { return nil, nil }

// Activity is the unread mentions and reactions of the chat list.
func (t *Telegram) Activity(ctx context.Context) ([]Activity, error) {
	dialogs, err := t.loadDialogs(ctx)
	if err != nil {
		return nil, err
	}
	var out []Activity
	asked := 0
	for _, d := range dialogs {
		if asked >= 8 { // each costs a request: the busiest chats only
			break
		}
		id := convID(d.Peer)
		peer, err := t.inputPeer(id)
		if err != nil {
			continue
		}
		if d.UnreadMentionsCount > 0 {
			asked++
			res, err := t.api.MessagesGetUnreadMentions(ctx, &tg.MessagesGetUnreadMentionsRequest{Peer: peer, Limit: 20})
			if err != nil {
				diag.Printf("telegram unread mentions: %v", err)
			} else {
				for _, m := range t.messages(res) {
					out = append(out, Activity{ID: id + "/m" + m.TS, Kind: ActMention, ConvID: id, Msg: m,
						By: m.User, ByID: m.UserID, At: m.At, Unread: true})
				}
			}
		}
		if d.UnreadReactionsCount > 0 {
			asked++
			res, err := t.api.MessagesGetUnreadReactions(ctx, &tg.MessagesGetUnreadReactionsRequest{Peer: peer, Limit: 20})
			if err != nil {
				diag.Printf("telegram unread reactions: %v", err)
				continue
			}
			mod, ok := res.AsModified()
			if !ok {
				continue
			}
			t.remember(mod.GetUsers(), mod.GetChats())
			for _, raw := range mod.GetMessages() {
				rm, ok := raw.(*tg.Message)
				if !ok {
					continue
				}
				m, _ := t.convert(rm, nil)
				rs, _ := rm.GetReactions()
				for _, p := range rs.RecentReactions {
					if !p.Unread {
						continue
					}
					out = append(out, Activity{ID: id + "/r" + m.TS + convID(p.PeerID), Kind: ActReaction, ConvID: id, Msg: m,
						By: t.peerName(p.PeerID), ByID: convID(p.PeerID), Emoji: reactionGlyph(p.Reaction),
						At: time.Unix(int64(p.Date), 0), Unread: true})
				}
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Activity) int { return b.At.Compare(a.At) })
	return out, nil
}

func (t *Telegram) Mark(ctx context.Context, id, ts string) error {
	n, err := msgID(ts)
	if err != nil {
		return err
	}
	peer, err := t.inputPeer(id)
	if err != nil {
		return err
	}
	if ch, ok := peer.(*tg.InputPeerChannel); ok {
		_, err = t.api.ChannelsReadHistory(ctx, &tg.ChannelsReadHistoryRequest{
			Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash}, MaxID: n})
	} else {
		_, err = t.api.MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{Peer: peer, MaxID: n})
	}
	if err != nil {
		return err
	}
	// Reading it clears a "Mark as unread", as Telegram's apps do.
	t.mu.Lock()
	marked := t.marked[id]
	t.mu.Unlock()
	if marked {
		return t.MarkUnread(ctx, id, false)
	}
	return nil
}

// MarkUnread sets or clears the dialog's unread mark ("Mark as unread" in
// Telegram's apps): it shows unread on every device until it is read.
func (t *Telegram) MarkUnread(ctx context.Context, id string, unread bool) error {
	peer, err := t.inputPeer(id)
	if err != nil {
		return err
	}
	if _, err := t.api.MessagesMarkDialogUnread(ctx, &tg.MessagesMarkDialogUnreadRequest{
		Unread: unread, Peer: &tg.InputDialogPeer{Peer: peer}}); err != nil {
		return err
	}
	t.mu.Lock()
	t.marked[id] = unread
	t.mu.Unlock()
	return nil
}

func (t *Telegram) File(ctx context.Context, id string) ([]byte, error) {
	var loc tg.InputFileLocationClass
	switch {
	case strings.HasPrefix(id, "avatar:"):
		l, err := t.avatar(strings.TrimPrefix(id, "avatar:"))
		if err != nil {
			return nil, err
		}
		loc = l
	case strings.HasPrefix(id, "emoji:"):
		return nil, fmt.Errorf("no custom emoji on Telegram")
	default:
		full := strings.HasPrefix(id, "full:")
		t.mu.Lock()
		f, ok := t.files[strings.TrimPrefix(id, "full:")]
		t.mu.Unlock()
		if !ok {
			return nil, fmt.Errorf("unknown file %q", id)
		}
		loc = f.thumb
		if full {
			loc = f.full
		}
	}
	var buf bytes.Buffer
	if _, err := downloader.NewDownloader().Download(t.api, loc).Stream(ctx, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (t *Telegram) avatar(id string) (tg.InputFileLocationClass, error) {
	k, n, err := splitConv(id)
	if err != nil {
		return nil, err
	}
	peer, err := t.inputPeer(id)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var photo int64
	switch k {
	case 'u':
		if p, ok := t.users[n].Photo.(*tg.UserProfilePhoto); ok {
			photo = p.PhotoID
		}
	case 'g':
		if p, ok := t.chats[n].Photo.(*tg.ChatPhoto); ok {
			photo = p.PhotoID
		}
	case 'c':
		if p, ok := t.channels[n].Photo.(*tg.ChatPhoto); ok {
			photo = p.PhotoID
		}
	}
	if photo == 0 {
		return nil, fmt.Errorf("no picture")
	}
	return &tg.InputPeerPhotoFileLocation{Peer: peer, PhotoID: photo}, nil
}

func (t *Telegram) CustomEmoji(context.Context) ([]string, error) { return nil, nil }

// Presence: Telegram shows whoever is connected as online.
func (t *Telegram) Presence(context.Context) (bool, error) { return false, nil }

func (t *Telegram) SetPresence(ctx context.Context, away bool) error {
	_, err := t.api.AccountUpdateStatus(ctx, away)
	return err
}

// SetSnooze mutes private chats, groups and channels for minutes; zero
// unmutes them.
func (t *Telegram) SetSnooze(ctx context.Context, minutes int) error {
	until := 0
	if minutes > 0 {
		until = int(time.Now().Add(time.Duration(minutes) * time.Minute).Unix())
	}
	for _, scope := range []tg.InputNotifyPeerClass{&tg.InputNotifyUsers{}, &tg.InputNotifyChats{}, &tg.InputNotifyBroadcasts{}} {
		var s tg.InputPeerNotifySettings
		s.SetMuteUntil(until)
		if _, err := t.api.AccountUpdateNotifySettings(ctx, &tg.AccountUpdateNotifySettingsRequest{Peer: scope, Settings: s}); err != nil {
			return err
		}
	}
	return nil
}

// Typing tells the conversation the user is typing (Telegram shows it for
// about six seconds).
func (t *Telegram) Typing(ctx context.Context, id, rootTS string) error {
	peer, err := t.inputPeer(id)
	if err != nil {
		return err
	}
	req := &tg.MessagesSetTypingRequest{Peer: peer, Action: &tg.SendMessageTypingAction{}}
	if _, ok := peer.(*tg.InputPeerChannel); ok && rootTS != "" {
		if n, err := msgID(rootTS); err == nil {
			req.SetTopMsgID(n)
		}
	}
	_, err = t.api.MessagesSetTyping(ctx, req)
	return err
}
