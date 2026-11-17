package chat

import (
	"context"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

// recordingAPI answers every call with success and keeps the requests.
func recordingAPI(t *Telegram) *[]bin.Encoder {
	var calls []bin.Encoder
	t.api = tg.NewClient(invokerFunc(func(_ context.Context, in bin.Encoder, out bin.Decoder) error {
		calls = append(calls, in)
		switch o := out.(type) {
		case *tg.BoolBox:
			o.Bool = &tg.BoolTrue{}
		case *tg.MessagesAffectedMessages:
			*o = tg.MessagesAffectedMessages{Pts: 1, PtsCount: 1}
		}
		return nil
	}))
	return &calls
}

func TestMarkUnreadSetsTheDialogFlag(t *testing.T) {
	tgm := newTestTelegram()
	calls := recordingAPI(tgm)
	if err := tgm.MarkUnread(context.Background(), "u2", true); err != nil {
		t.Fatal(err)
	}
	req, ok := (*calls)[0].(*tg.MessagesMarkDialogUnreadRequest)
	if !ok || !req.Unread {
		t.Fatalf("request %#v", (*calls)[0])
	}
	if p, ok := req.Peer.(*tg.InputDialogPeer); !ok || p.Peer.(*tg.InputPeerUser).UserID != 2 {
		t.Fatalf("peer %#v", req.Peer)
	}
	if !tgm.marked["u2"] {
		t.Fatal("the mark is not remembered")
	}
	// Reading the chat clears the mark too.
	*calls = nil
	if err := tgm.Mark(context.Background(), "u2", "10"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls %#v", *calls)
	}
	if _, ok := (*calls)[0].(*tg.MessagesReadHistoryRequest); !ok {
		t.Fatalf("first call %#v", (*calls)[0])
	}
	if r, ok := (*calls)[1].(*tg.MessagesMarkDialogUnreadRequest); !ok || r.Unread {
		t.Fatalf("second call %#v", (*calls)[1])
	}
	if tgm.marked["u2"] {
		t.Fatal("the mark survived reading")
	}
	// Without a mark, reading is one call.
	*calls = nil
	_ = tgm.Mark(context.Background(), "u2", "11")
	if len(*calls) != 1 {
		t.Fatalf("calls %#v", *calls)
	}
}

func TestReplyQuotesWithoutAThread(t *testing.T) {
	tgm := newTestTelegram()
	var sent *tg.MessagesSendMessageRequest
	tgm.api = tg.NewClient(invokerFunc(func(_ context.Context, in bin.Encoder, out bin.Decoder) error {
		if r, ok := in.(*tg.MessagesSendMessageRequest); ok {
			sent = r
		}
		if o, ok := out.(*tg.UpdatesBox); ok {
			o.Updates = &tg.UpdateShortSentMessage{ID: 77, Date: 1}
		}
		return nil
	}))
	m, err := tgm.Reply(context.Background(), "c4", "12", "hola")
	if err != nil || m.TS != "77" {
		t.Fatalf("reply: %v %+v", err, m)
	}
	rt, ok := sent.ReplyTo.(*tg.InputReplyToMessage)
	if !ok || rt.ReplyToMsgID != 12 {
		t.Fatalf("reply_to %#v", sent.ReplyTo)
	}
	if _, ok := rt.GetTopMsgID(); ok {
		t.Fatal("a reply in the chat must not go to a thread")
	}
}
