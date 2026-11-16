package tui

import (
	"testing"
	"time"

	"github.com/oscarmlage/sltty/internal/chat"
)

func TestHistoryCacheExpires(t *testing.T) {
	c := &historyCache{dir: t.TempDir(), ttl: time.Hour}
	want := []chat.Message{{TS: "1", Text: "cached"}}
	c.save("C1", want)
	if got, ok := c.load("C1"); !ok || len(got) != 1 || got[0].Text != "cached" {
		t.Fatalf("cached history: %v, %v", got, ok)
	}
	c.ttl = -time.Second
	if _, ok := c.load("C1"); ok {
		t.Fatal("expired history was returned")
	}
}
