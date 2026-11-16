package chat

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/oscarmlage/sltty/internal/diag"
)

// WaitError is Telegram's FLOOD_WAIT, when it asks for longer than the
// call can wait: Method may be called again after Wait.
type WaitError struct {
	Method string
	Wait   time.Duration
}

func (e *WaitError) Error() string {
	return fmt.Sprintf("Telegram asks to slow down (%s): retrying in %s", e.Method, e.Wait.Round(time.Second))
}

// pacer keeps sltty under Telegram's limits: the same method at most every
// gap, a few downloads at a time, and FLOOD_WAIT waited out (then retried)
// when the caller's deadline allows it. While a method is under a
// FLOOD_WAIT, new calls to it wait (or fail) without asking again.
type pacer struct {
	gap       time.Duration
	downloads chan struct{}

	mu      sync.Mutex
	next    map[string]time.Time // method → earliest next call
	blocked map[string]time.Time // method → end of its FLOOD_WAIT
	now     func() time.Time
	sleep   func(ctx context.Context, d time.Duration) error
}

func newPacer() *pacer {
	return &pacer{
		gap:       300 * time.Millisecond,
		downloads: make(chan struct{}, 4),
		next:      map[string]time.Time{},
		blocked:   map[string]time.Time{},
		now:       time.Now,
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-t.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
}

func methodOf(input bin.Encoder) string {
	if n, ok := input.(interface{ TypeName() string }); ok {
		return n.TypeName()
	}
	return fmt.Sprintf("%T", input)
}

// unpaced methods are cheap or must not be delayed.
var unpaced = map[string]bool{
	"upload.getFile":               true, // limited by the download slots
	"messages.setTyping":           true,
	"updates.getDifference":        true,
	"updates.getState":             true,
	"updates.getChannelDifference": true,
}

// slot waits for method's turn; it fails at once when the wait would pass
// the caller's deadline.
func (p *pacer) slot(ctx context.Context, method string) error {
	p.mu.Lock()
	now := p.now()
	at := now
	if b := p.blocked[method]; b.After(at) {
		at = b
	}
	if !unpaced[method] {
		if n := p.next[method]; n.After(at) {
			at = n
		}
		p.next[method] = at.Add(p.gap)
	}
	p.mu.Unlock()
	wait := at.Sub(now)
	if wait <= 0 {
		return nil
	}
	if dl, ok := ctx.Deadline(); ok && now.Add(wait).After(dl) {
		return &WaitError{Method: method, Wait: wait}
	}
	return p.sleep(ctx, wait)
}

func (p *pacer) Handle(next tg.Invoker) telegram.InvokeFunc {
	return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
		method := methodOf(input)
		if method == "upload.getFile" {
			select {
			case p.downloads <- struct{}{}:
				defer func() { <-p.downloads }()
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		for attempt := 0; ; attempt++ {
			if err := p.slot(ctx, method); err != nil {
				return err
			}
			err := next.Invoke(ctx, input, output)
			d, flood := tgerr.AsFloodWait(err)
			if !flood {
				return err
			}
			d += time.Second
			diag.Printf("telegram FLOOD_WAIT %s: %s", method, d)
			p.mu.Lock()
			if until := p.now().Add(d); until.After(p.blocked[method]) {
				p.blocked[method] = until
			}
			p.mu.Unlock()
			if attempt >= 2 {
				return &WaitError{Method: method, Wait: d}
			}
		}
	}
}
