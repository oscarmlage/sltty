package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type invokerFunc func(ctx context.Context, in bin.Encoder, out bin.Decoder) error

func (f invokerFunc) Invoke(ctx context.Context, in bin.Encoder, out bin.Decoder) error {
	return f(ctx, in, out)
}

// fakeTime is a clock the pacer's sleeps advance.
func fakeTime(p *pacer) *time.Time {
	now := time.Unix(1000, 0)
	p.now = func() time.Time { return now }
	p.sleep = func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil }
	return &now
}

func TestPacerSpacesTheSameMethod(t *testing.T) {
	p := newPacer()
	now := fakeTime(p)
	var at []time.Time
	inv := p.Handle(invokerFunc(func(context.Context, bin.Encoder, bin.Decoder) error { at = append(at, *now); return nil }))
	for range 3 {
		_ = inv(context.Background(), &tg.MessagesGetHistoryRequest{}, nil)
	}
	_ = inv(context.Background(), &tg.MessagesReadHistoryRequest{}, nil)
	if at[1].Sub(at[0]) != p.gap || at[2].Sub(at[1]) != p.gap {
		t.Fatalf("not spaced: %v", at)
	}
	if !at[3].Equal(at[2]) {
		t.Fatal("another method waited for this one")
	}
}

func TestPacerWaitsOutFloodWait(t *testing.T) {
	p := newPacer()
	now := fakeTime(p)
	calls := 0
	inv := p.Handle(invokerFunc(func(context.Context, bin.Encoder, bin.Decoder) error {
		calls++
		if calls == 1 {
			return tgerr.New(420, "FLOOD_WAIT_5")
		}
		return nil
	}))
	start := *now
	if err := inv(context.Background(), &tg.MessagesGetHistoryRequest{}, nil); err != nil || calls != 2 {
		t.Fatalf("not retried: %v, %d calls", err, calls)
	}
	if now.Sub(start) < 5*time.Second {
		t.Fatalf("did not wait: %v", now.Sub(start))
	}
}

func TestPacerFailsFastPastTheDeadline(t *testing.T) {
	p := newPacer()
	fakeTime(p)
	calls := 0
	inv := p.Handle(invokerFunc(func(context.Context, bin.Encoder, bin.Decoder) error {
		calls++
		return tgerr.New(420, "FLOOD_WAIT_60")
	}))
	ctx, cancel := context.WithDeadline(context.Background(), p.now().Add(15*time.Second))
	defer cancel()
	err := inv(ctx, &tg.MessagesGetHistoryRequest{}, nil)
	var w *WaitError
	if !errors.As(err, &w) || w.Method != "messages.getHistory" || w.Wait < 60*time.Second || calls != 1 {
		t.Fatalf("err %v, %d calls", err, calls)
	}
	// While blocked, the method is not asked again.
	if err := inv(ctx, &tg.MessagesGetHistoryRequest{}, nil); !errors.As(err, &w) || calls != 1 {
		t.Fatalf("asked again under FLOOD_WAIT: %v, %d calls", err, calls)
	}
}
