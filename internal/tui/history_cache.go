package tui

// Local, opt-in cache for recently opened channel histories.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/oscarmlage/sltty/internal/chat"
)

type historyCache struct {
	dir string
	ttl time.Duration
}

type cachedHistory struct {
	Saved time.Time      `json:"saved"`
	Msgs  []chat.Message `json:"messages"`
}

func newHistoryCache(workspace string, ttl time.Duration) *historyCache {
	base, err := os.UserCacheDir()
	if err != nil || ttl <= 0 {
		return nil
	}
	sum := sha256.Sum256([]byte(workspace))
	return &historyCache{dir: filepath.Join(base, "sltty", "history", hex.EncodeToString(sum[:])), ttl: ttl}
}

func (c *historyCache) path(id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:])+".json")
}

func (c *historyCache) load(id string) ([]chat.Message, bool) {
	b, err := os.ReadFile(c.path(id))
	if err != nil {
		return nil, false
	}
	var x cachedHistory
	if json.Unmarshal(b, &x) != nil || x.Saved.IsZero() || time.Since(x.Saved) > c.ttl {
		_ = os.Remove(c.path(id))
		return nil, false
	}
	return x.Msgs, true
}

func (c *historyCache) save(id string, msgs []chat.Message) {
	if c == nil {
		return
	}
	if os.MkdirAll(c.dir, 0700) != nil {
		return
	}
	b, err := json.Marshal(cachedHistory{Saved: time.Now(), Msgs: msgs})
	if err != nil {
		return
	}
	tmp := c.path(id) + ".tmp"
	if os.WriteFile(tmp, b, 0600) == nil {
		_ = os.Rename(tmp, c.path(id))
	}
}
