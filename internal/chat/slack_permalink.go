package chat

import (
	"context"
	"fmt"
	"net/url"
)

// MessageLink returns Slack's canonical permalink for a message.
func (s *Slack) MessageLink(ctx context.Context, convID, ts string) (string, error) {
	var r struct {
		Permalink string `json:"permalink"`
	}
	if err := s.call(ctx, "chat.getPermalink", url.Values{"channel": {convID}, "message_ts": {ts}}, &r); err != nil {
		return "", err
	}
	if r.Permalink == "" {
		return "", fmt.Errorf("Slack did not return a message link")
	}
	return r.Permalink, nil
}
