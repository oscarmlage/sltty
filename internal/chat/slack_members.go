package chat

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Members lists the people in a Slack channel. conversations.members is
// paginated, then users.info supplies the names Slack displays.
func (s *Slack) Members(ctx context.Context, convID string) ([]Person, error) {
	var ids []string
	cursor := ""
	for {
		var r struct {
			Members          []string `json:"members"`
			ResponseMetadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		form := url.Values{"channel": {convID}, "limit": {"200"}}
		if cursor != "" {
			form.Set("cursor", cursor)
		}
		if err := s.call(ctx, "conversations.members", form, &r); err != nil {
			// Some Enterprise Grid organisations deny the public endpoint even
			// to a signed-in member. Their web client may still put members in
			// its bootstrap snapshot, which uses the same session.
			if strings.Contains(err.Error(), "enterprise_is_restricted") {
				return s.membersFromBoot(ctx, convID)
			}
			return nil, err
		}
		ids = append(ids, r.Members...)
		cursor = r.ResponseMetadata.NextCursor
		if cursor == "" {
			break
		}
	}

	return s.peopleForIDs(ctx, ids)
}

// membersFromBoot reads members from Slack's web-client bootstrap response.
// It is an Enterprise fallback only; ordinary workspaces use the documented
// conversations.members endpoint above.
func (s *Slack) membersFromBoot(ctx context.Context, convID string) ([]Person, error) {
	var r struct {
		Channels []struct {
			ID      string   `json:"id"`
			Members []string `json:"members"`
		} `json:"channels"`
	}
	form := url.Values{"version_all_channels": {"false"}, "return_all_relevant_mpdms": {"true"}, "omit_extras": {"feature_usage_data,plan_info,salesforce_features"}}
	if err := s.call(ctx, "client.userBoot", form, &r); err != nil {
		return nil, err
	}
	for _, c := range r.Channels {
		if c.ID == convID && c.Members != nil {
			return s.peopleForIDs(ctx, c.Members)
		}
	}
	return nil, fmt.Errorf("Slack did not include this channel's members in its Enterprise bootstrap")
}

func (s *Slack) peopleForIDs(ctx context.Context, ids []string) ([]Person, error) {
	people := make([]Person, 0, len(ids))
	for _, id := range ids {
		var r struct {
			User struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Deleted bool   `json:"deleted"`
				Profile struct {
					RealName    string `json:"real_name"`
					DisplayName string `json:"display_name"`
					Image72     string `json:"image_72"`
				} `json:"profile"`
			} `json:"user"`
		}
		if err := s.call(ctx, "users.info", url.Values{"user": {id}}, &r); err != nil {
			return nil, err
		}
		u := r.User
		if u.Deleted || u.ID == "" {
			continue
		}
		name := u.Name
		if name == "" {
			name = u.Profile.DisplayName
		}
		if name == "" {
			name = u.Profile.RealName
		}
		s.mu.Lock()
		s.users[u.ID] = name
		s.peopleDirty = true
		if u.Profile.Image72 != "" {
			s.urls["avatar:"+u.ID] = u.Profile.Image72
		}
		s.mu.Unlock()
		people = append(people, Person{ID: u.ID, Name: Sanitize(name), Full: Sanitize(u.Profile.RealName)})
	}
	slices.SortFunc(people, func(a, b Person) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return people, nil
}
