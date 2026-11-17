package chat

// Mentions on Slack: <@U…> in the text is Slack's own form, so sending
// needs nothing. Whom to offer: Enterprise Grid restricts the members of a
// conversation, so the TUI offers the people of the open history and, as
// the user types, Slack's people directory (what its own search uses).

import (
	"context"
	"net/url"
	"strings"
)

func (s *Slack) People(ctx context.Context, _, query string) ([]Person, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	var r struct {
		Items []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Deleted  bool   `json:"deleted"`
			Profile  struct {
				RealName    string `json:"real_name"`
				DisplayName string `json:"display_name"`
				Image72     string `json:"image_72"`
			} `json:"profile"`
		} `json:"items"`
	}
	form := url.Values{"query": {query}, "module": {"people"}, "count": {"10"}}
	if err := s.call(ctx, "search.modules", form, &r); err != nil {
		return nil, err
	}
	out := make([]Person, 0, len(r.Items))
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range r.Items {
		if it.ID == "" || it.Deleted || it.ID == s.me {
			continue
		}
		name := it.Username // as resolveUser names people in messages
		if name == "" {
			name = it.Profile.DisplayName
		}
		if _, ok := s.users[it.ID]; !ok && name != "" {
			s.users[it.ID] = name
			s.peopleDirty = true
		}
		if u := it.Profile.Image72; u != "" && s.urls["avatar:"+it.ID] == "" {
			s.urls["avatar:"+it.ID] = u
		}
		out = append(out, Person{ID: it.ID, Name: name, Full: Sanitize(it.Profile.RealName)})
	}
	return out, nil
}

func (s *Slack) PersonName(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.users[id]
	return n, ok
}
