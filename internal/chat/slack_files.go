package chat

// Pictures: attachments, avatars and custom emoji.

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/oscarmlage/sltty/internal/emoji"
)

// File downloads an attachment's thumbnail, a custom emoji ("emoji:name")
// or a person's avatar ("avatar:id").
func (s *Slack) File(ctx context.Context, id string) ([]byte, error) {
	if who, ok := strings.CutPrefix(id, "avatar:"); ok {
		s.resolveAuthor(ctx, who)
	}
	s.mu.Lock()
	u := s.urls[id]
	s.mu.Unlock()
	file, full := strings.CutPrefix(id, "full:")
	if u == "" && !strings.Contains(file, ":") {
		var r struct {
			File slackFile `json:"file"`
		}
		if err := s.call(ctx, "files.info", url.Values{"file": {file}}, &r); err != nil {
			return nil, err
		}
		u = r.File.picture()
		if full {
			u = cmp.Or(r.File.URLPrivate, u)
		}
	}
	if u == "" {
		return nil, fmt.Errorf("Slack has no picture for %s", id)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if req.URL.Scheme != "https" {
		return nil, fmt.Errorf("Slack %s: refusing a non-https picture URL", id)
	}
	// Only Slack's own file host gets the session; emoji and avatars come
	// from public CDNs.
	if h := req.URL.Hostname(); h == "slack.com" || strings.HasSuffix(h, ".slack.com") {
		req.Header.Set("Authorization", "Bearer "+s.creds.Token)
		req.Header.Set("Cookie", "d="+s.creds.Cookie)
	}
	res, err := s.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Slack %s: HTTP %d", id, res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return nil, fmt.Errorf("Slack %s: not a picture (%s)", id, ct)
	}
	return io.ReadAll(io.LimitReader(res.Body, 16<<20))
}

// CustomEmoji lists the workspace's emoji and remembers where each picture
// is, following aliases.
func (s *Slack) CustomEmoji(ctx context.Context) ([]string, error) {
	var r struct {
		Emoji map[string]string `json:"emoji"`
	}
	if err := s.call(ctx, "emoji.list", nil, &r); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(r.Emoji))
	s.mu.Lock()
	defer s.mu.Unlock()
	for n, u := range r.Emoji {
		for range 3 { // alias:other, at most a few hops
			to, ok := strings.CutPrefix(u, "alias:")
			if !ok {
				break
			}
			u = r.Emoji[to]
		}
		if strings.HasPrefix(u, "https://") {
			s.urls["emoji:"+n] = u
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}

// emojiName is the Slack name of an emoji character; :name: is already one.
func emojiName(e string) (string, bool) {
	if strings.HasPrefix(e, ":") {
		return "", false
	}
	return emoji.Name(e)
}
