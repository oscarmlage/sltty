package chat

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var markdownLink = regexp.MustCompile(`\[([^\[\]]+)\]\((https?://[^\s)]+)\)`)

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

// FormatLinks lets a composer use familiar [label](URL) text. Slack stores
// links as <URL|label>, which its official clients render as linked words.
func (s *Slack) FormatLinks(text string) string {
	return markdownLink.ReplaceAllString(text, "<$2|$1>")
}

func (s *Slack) clientMessageLink(convID, ts string) string {
	stamp := strings.ReplaceAll(ts, ".", "")
	if s.workspaceURL != "" {
		// This is Slack's canonical share URL. Unlike the app client route,
		// pasting it into Slack produces the familiar forwarded-message card.
		return s.workspaceURL + "/archives/" + convID + "/p" + stamp
	}
	return "https://app.slack.com/client/" + s.creds.TeamID + "/" + convID + "/thread/" + convID + "-" + stamp
}
