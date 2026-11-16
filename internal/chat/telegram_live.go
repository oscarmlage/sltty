package chat

import (
	"context"
	"strconv"
	"time"

	"github.com/gotd/td/tg"

	"github.com/oscarmlage/sltty/internal/diag"
)

// Events is the live stream: new, edited and deleted messages, reactions,
// reads on other devices and presence.
func (t *Telegram) Events() <-chan Event { return t.events }

func (t *Telegram) emit(e Event) {
	select {
	case t.events <- e:
	default:
		diag.Printf("telegram: event dropped, the UI is behind")
	}
}

func (t *Telegram) rememberEntities(e tg.Entities) {
	var users []tg.UserClass
	var chats []tg.ChatClass
	for _, u := range e.Users {
		users = append(users, u)
	}
	for _, c := range e.Chats {
		chats = append(chats, c)
	}
	for _, c := range e.Channels {
		chats = append(chats, c)
	}
	t.remember(users, chats)
}

// handle registers the update handlers.
func (t *Telegram) handle(d tg.UpdateDispatcher) {
	d.OnNewMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		t.rememberEntities(e)
		t.newMessage(u.Message)
		return nil
	})
	d.OnNewChannelMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		t.rememberEntities(e)
		t.newMessage(u.Message)
		return nil
	})
	d.OnEditMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateEditMessage) error {
		t.rememberEntities(e)
		t.edited(u.Message)
		return nil
	})
	d.OnEditChannelMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateEditChannelMessage) error {
		t.rememberEntities(e)
		t.edited(u.Message)
		return nil
	})
	d.OnDeleteMessages(func(_ context.Context, _ tg.Entities, u *tg.UpdateDeleteMessages) error {
		byConv := map[string][]string{}
		t.mu.Lock()
		for _, id := range u.Messages {
			if c, ok := t.seen[id]; ok {
				byConv[c] = append(byConv[c], strconv.Itoa(id))
				delete(t.seen, id)
			}
		}
		t.mu.Unlock()
		for c, ts := range byConv {
			t.emit(Event{Kind: EvDeleted, ConvID: c, TS: ts})
		}
		return nil
	})
	d.OnDeleteChannelMessages(func(_ context.Context, _ tg.Entities, u *tg.UpdateDeleteChannelMessages) error {
		var ts []string
		for _, id := range u.Messages {
			ts = append(ts, strconv.Itoa(id))
		}
		t.emit(Event{Kind: EvDeleted, ConvID: "c" + strconv.FormatInt(u.ChannelID, 10), TS: ts})
		return nil
	})
	d.OnReadHistoryInbox(func(_ context.Context, _ tg.Entities, u *tg.UpdateReadHistoryInbox) error {
		t.emit(Event{Kind: EvRead, ConvID: convID(u.Peer), Unread: u.StillUnreadCount})
		return nil
	})
	d.OnReadChannelInbox(func(_ context.Context, _ tg.Entities, u *tg.UpdateReadChannelInbox) error {
		t.emit(Event{Kind: EvRead, ConvID: "c" + strconv.FormatInt(u.ChannelID, 10), Unread: u.StillUnreadCount})
		return nil
	})
	d.OnUserStatus(func(_ context.Context, _ tg.Entities, u *tg.UpdateUserStatus) error {
		_, online := u.Status.(*tg.UserStatusOnline)
		t.mu.Lock()
		if usr, ok := t.users[u.UserID]; ok {
			usr.Status = u.Status
		}
		t.mu.Unlock()
		t.emit(Event{Kind: EvPresence, ConvID: "u" + strconv.FormatInt(u.UserID, 10), Online: online})
		return nil
	})
	d.OnFolderPeers(func(_ context.Context, _ tg.Entities, u *tg.UpdateFolderPeers) error {
		for _, fp := range u.FolderPeers {
			id := convID(fp.Peer)
			t.mu.Lock()
			t.archived[id] = fp.FolderID == 1
			t.mu.Unlock()
			t.convChanged(fp.Peer)
		}
		return nil
	})
	d.OnNotifySettings(func(_ context.Context, _ tg.Entities, u *tg.UpdateNotifySettings) error {
		p, ok := u.Peer.(*tg.NotifyPeer)
		if !ok {
			return nil
		}
		until, _ := u.NotifySettings.GetMuteUntil()
		t.mu.Lock()
		t.muted[convID(p.Peer)] = until > int(time.Now().Unix())
		t.mu.Unlock()
		t.convChanged(p.Peer)
		return nil
	})
	d.OnDialogPinned(func(_ context.Context, _ tg.Entities, u *tg.UpdateDialogPinned) error {
		p, ok := u.Peer.(*tg.DialogPeer)
		if !ok {
			return nil
		}
		t.mu.Lock()
		t.pinned[convID(p.Peer)] = u.Pinned
		t.mu.Unlock()
		t.convChanged(p.Peer)
		return nil
	})
	d.OnUserTyping(func(_ context.Context, e tg.Entities, u *tg.UpdateUserTyping) error {
		t.rememberEntities(e)
		peer := &tg.PeerUser{UserID: u.UserID}
		t.typing(convID(peer), peer, u.Action, 0)
		return nil
	})
	d.OnChatUserTyping(func(_ context.Context, e tg.Entities, u *tg.UpdateChatUserTyping) error {
		t.rememberEntities(e)
		t.typing("g"+strconv.FormatInt(u.ChatID, 10), u.FromID, u.Action, 0)
		return nil
	})
	d.OnChannelUserTyping(func(_ context.Context, e tg.Entities, u *tg.UpdateChannelUserTyping) error {
		t.rememberEntities(e)
		top, _ := u.GetTopMsgID()
		t.typing("c"+strconv.FormatInt(u.ChannelID, 10), u.FromID, u.Action, top)
		return nil
	})
	d.OnMessageReactions(func(_ context.Context, e tg.Entities, u *tg.UpdateMessageReactions) error {
		t.rememberEntities(e)
		t.emit(Event{Kind: EvReactions, ConvID: convID(u.Peer),
			Msg: Message{TS: strconv.Itoa(u.MsgID), Reactions: t.reactions(u.Reactions)}})
		return nil
	})
}

// rootOf is the thread a message answers: its topic or the message it
// replies to.
func rootOf(m *tg.Message) string {
	h, ok := m.ReplyTo.(*tg.MessageReplyHeader)
	if !ok {
		return ""
	}
	switch {
	case h.ReplyToTopID != 0:
		return strconv.Itoa(h.ReplyToTopID)
	case h.ReplyToMsgID != 0:
		return strconv.Itoa(h.ReplyToMsgID)
	}
	return ""
}

func (t *Telegram) newMessage(raw tg.MessageClass) {
	msg, ok := t.convert(raw, nil)
	if !ok {
		return
	}
	peer := peerOf(raw)
	id := convID(peer)
	if id == "" {
		return
	}
	t.dmu.Lock()
	t.dialogAt = t.dialogAt.AddDate(-1, 0, 0) // the chat list changed: fetch it again next time
	t.dmu.Unlock()
	e := Event{Kind: EvMessage, ConvID: id, Msg: msg}
	if m, ok := raw.(*tg.Message); ok {
		e.RootTS = rootOf(m)
	}
	// A new message brings an archived chat back, unless it is muted
	// (as the official apps do).
	t.mu.Lock()
	if t.archived[id] && !t.muted[id] {
		if m, ok := raw.(*tg.Message); !ok || !m.Out {
			t.archived[id] = false
		}
	}
	t.mu.Unlock()
	if c, ok := t.conversation(tg.Dialog{Peer: peer}); ok {
		t.place(&c)
		e.Conv = &c
	}
	t.emit(e)
}

// convChanged reports a conversation's new place or state.
func (t *Telegram) convChanged(peer tg.PeerClass) {
	if c, ok := t.conversation(tg.Dialog{Peer: peer}); ok {
		t.place(&c)
		t.emit(Event{Kind: EvConv, ConvID: c.ID, Conv: &c})
	}
}

// typing reports what someone is doing in a conversation; an empty
// Action is a cancel. Telegram repeats the action every few seconds while
// it lasts; without it the UI lets it expire.
func (t *Telegram) typing(conv string, from tg.PeerClass, a tg.SendMessageActionClass, top int) {
	if p, ok := from.(*tg.PeerUser); ok && p.UserID == t.self.ID {
		return
	}
	action := ""
	switch a.(type) {
	case *tg.SendMessageCancelAction:
	case *tg.SendMessageTypingAction:
		action = "typing"
	case *tg.SendMessageRecordAudioAction, *tg.SendMessageUploadAudioAction:
		action = "recording a voice message"
	case *tg.SendMessageRecordRoundAction, *tg.SendMessageUploadRoundAction:
		action = "recording a video message"
	case *tg.SendMessageRecordVideoAction, *tg.SendMessageUploadVideoAction:
		action = "sending a video"
	case *tg.SendMessageUploadPhotoAction:
		action = "sending a photo"
	case *tg.SendMessageUploadDocumentAction:
		action = "sending a file"
	case *tg.SendMessageChooseStickerAction:
		action = "choosing a sticker"
	default:
		return // game, geo, emoji interactions...
	}
	e := Event{Kind: EvTyping, ConvID: conv, User: t.peerName(from), Action: action}
	if top != 0 {
		e.RootTS = strconv.Itoa(top)
	}
	t.emit(e)
}

func (t *Telegram) edited(raw tg.MessageClass) {
	msg, ok := t.convert(raw, nil)
	if !ok {
		return
	}
	t.emit(Event{Kind: EvEdited, ConvID: convID(peerOf(raw)), Msg: msg})
}
