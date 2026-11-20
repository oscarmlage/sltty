package chat

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// MessageLink returns Slack's canonical permalink for a message.
func (s *Slack) MessageLink(ctx context.Context, convID, ts string) (string, error) {
	var r struct {
		Permalink string `json:"permalink"`
	}
	if err := s.call(ctx, "chat.getPermalink", url.Values{"channel": {convID}, "message_ts": {ts}}, &r); err != nil {
		// Enterprise Grid can deny chat.getPermalink to a web session. The
		// Slack client route is still a stable, shareable deep link and does
		// not need another request.
		return s.clientMessageLink(convID, ts), nil
	}
	if r.Permalink == "" {
		return "", fmt.Errorf("Slack did not return a message link")
	}
	return r.Permalink, nil
}

func (s *Slack) clientMessageLink(convID, ts string) string {
	return "https://app.slack.com/client/" + s.creds.TeamID + "/" + convID + "/thread/" + convID + "-" + strings.ReplaceAll(ts, ".", "")
}
