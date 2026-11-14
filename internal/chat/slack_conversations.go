package chat

// The sidebar: conversations, native sections and unread counts.

import (
	"context"
	"net/url"
	"strings"

	"github.com/oscarmlage/sltty/internal/diag"
)

func (s *Slack) Conversations(ctx context.Context) ([]Conversation, error) {
	var all []Conversation
	cursor := ""
	for {
		var r struct {
			Channels []struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Topic struct {
					Value string `json:"value"`
				} `json:"topic"`
				IsIM               bool   `json:"is_im"`
				IsPrivate          bool   `json:"is_private"`
				User               string `json:"user"`
				UnreadCountDisplay int    `json:"unread_count_display"`
			} `json:"channels"`
			ResponseMetadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		f := url.Values{"types": {"public_channel,private_channel,im,mpim"}, "exclude_archived": {"true"}, "limit": {"200"}}
		if cursor != "" {
			f.Set("cursor", cursor)
		}
		if err := s.call(ctx, "users.conversations", f, &r); err != nil {
			if strings.Contains(err.Error(), "enterprise_is_restricted") {
				return s.userBoot(ctx)
			}
			return nil, err
		}
		for _, c := range r.Channels {
			name := c.Name
			kind := Channel
			if c.IsIM {
				kind = DM
				s.resolveUser(ctx, c.User)
				name = s.user(c.User)
			}
			all = append(all, Conversation{ID: c.ID, Name: name, Kind: kind, Topic: c.Topic.Value, Unread: c.UnreadCountDisplay, Private: c.IsPrivate})
		}
		cursor = r.ResponseMetadata.NextCursor
		if cursor == "" {
			break
		}
	}
	all = s.withSelfDM(ctx, all)
	if sections, err := s.nativeSections(ctx); err == nil {
		all = applyNativeSections(all, sections)
	} else {
		diag.Printf("Slack native sections unavailable: %v", err)
	}
	if counts, err := s.unreadCounts(ctx); err == nil {
		for i := range all {
			if n, ok := counts[all[i].ID]; ok {
				all[i].Unread = n
			}
		}
	} else {
		diag.Printf("Slack unread counts unavailable: %v", err)
	}
	return moveSelfDMEnd(all), nil
}

// userBoot is the browser client's initial sidebar snapshot. Enterprise Grid
// may intentionally disable users.conversations even for its own web tokens.
func (s *Slack) userBoot(ctx context.Context) ([]Conversation, error) {
	var r struct {
		Channels []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			IsPrivate bool   `json:"is_private"`
			Topic     struct {
				Value string `json:"value"`
			} `json:"topic"`
		} `json:"channels"`
		IMS []struct {
			ID         string `json:"id"`
			User       string `json:"user"`
			IsOpen     bool   `json:"is_open"`
			IsArchived bool   `json:"is_archived"`
		} `json:"ims"`
	}
	form := url.Values{"version_all_channels": {"false"}, "return_all_relevant_mpdms": {"true"}, "omit_extras": {"feature_usage_data,plan_info,salesforce_features"}}
	if err := s.call(ctx, "client.userBoot", form, &r); err != nil {
		return nil, err
	}
	out := make([]Conversation, 0, len(r.Channels)+len(r.IMS))
	for _, c := range r.Channels {
		out = append(out, Conversation{ID: c.ID, Name: c.Name, Topic: c.Topic.Value, Private: c.IsPrivate})
	}
	for _, im := range r.IMS {
		if im.IsArchived || !im.IsOpen {
			continue
		}
		s.resolveUser(ctx, im.User)
		out = append(out, Conversation{ID: im.ID, Name: s.user(im.User), Kind: DM})
	}
	// Both calls are best-effort: a Grid can restrict either, but userBoot
	// still gives us a usable flat sidebar in that case.
	out = s.withSelfDM(ctx, out)
	if sections, err := s.nativeSections(ctx); err == nil {
		out = applyNativeSections(out, sections)
	} else {
		diag.Printf("Slack native sections unavailable: %v", err)
	}
	if counts, err := s.unreadCounts(ctx); err == nil {
		for i := range out {
			if n, ok := counts[out[i].ID]; ok {
				out[i].Unread = n
			}
		}
	} else {
		diag.Printf("Slack unread counts unavailable: %v", err)
	}
	return moveSelfDMEnd(out), nil
}

// moveSelfDMEnd keeps the personal scratchpad at the end of the DM list,
// even if the server included it in a native sidebar section.
func moveSelfDMEnd(convs []Conversation) []Conversation {
	at := -1
	for i, c := range convs {
		if c.Kind == DM && c.Name == "you" {
			at = i
			break
		}
	}
	if at < 0 || at == len(convs)-1 {
		return convs
	}
	self := convs[at]
	copy(convs[at:], convs[at+1:])
	convs[len(convs)-1] = self
	return convs
}

// withSelfDM makes Slack's "chat with yourself" visible even where
// client.userBoot omits it. conversations.open returns the existing IM; it
// does not post a message.
func (s *Slack) withSelfDM(ctx context.Context, convs []Conversation) []Conversation {
	for _, c := range convs {
		if c.Kind == DM && c.Name == "you" {
			return convs
		}
	}
	var r struct {
		Channel struct {
			ID   string `json:"id"`
			IsIM bool   `json:"is_im"`
			User string `json:"user"`
		} `json:"channel"`
	}
	if err := s.call(ctx, "conversations.open", url.Values{"users": {s.me}}, &r); err != nil || r.Channel.ID == "" {
		if err != nil {
			diag.Printf("self DM unavailable: %v", err)
		}
		return convs
	}
	for _, c := range convs {
		if c.ID == r.Channel.ID {
			return convs
		}
	}
	return append(convs, Conversation{ID: r.Channel.ID, Name: "you", Kind: DM})
}

type nativeSection struct {
	ID, Name, Emoji, Next string
	IDs                   []string
}

func (s *Slack) nativeSections(ctx context.Context) ([]nativeSection, error) {
	var r struct {
		Sections []struct {
			ID    string `json:"channel_section_id"`
			Name  string `json:"name"`
			Emoji string `json:"emoji"`
			Next  string `json:"next_channel_section_id"`
			Page  struct {
				IDs []string `json:"channel_ids"`
			} `json:"channel_ids_page"`
		} `json:"channel_sections"`
		Cursor string `json:"cursor"`
	}
	form := url.Values{"token": {s.creds.Token}}
	if err := s.call(ctx, "users.channelSections.list", form, &r); err != nil {
		return nil, err
	}
	out := make([]nativeSection, 0, len(r.Sections))
	byID := map[string]nativeSection{}
	for _, x := range r.Sections {
		byID[x.ID] = nativeSection{ID: x.ID, Name: x.Name, Emoji: x.Emoji, Next: x.Next, IDs: x.Page.IDs}
	}
	// next_channel_section_id forms the authoritative linked order.
	pointed := map[string]bool{}
	for _, x := range byID {
		if x.Next != "" {
			pointed[x.Next] = true
		}
	}
	for id, x := range byID {
		if !pointed[id] {
			for {
				out = append(out, x)
				if x.Next == "" {
					break
				}
				next, ok := byID[x.Next]
				if !ok {
					break
				}
				x = next
			}
		}
	}
	if len(out) == 0 {
		for _, x := range byID {
			out = append(out, x)
		}
	}
	return out, nil
}
func applyNativeSections(convs []Conversation, sections []nativeSection) []Conversation {
	byID := map[string]Conversation{}
	for _, c := range convs {
		byID[c.ID] = c
	}
	used := map[string]bool{}
	out := make([]Conversation, 0, len(convs))
	for _, s := range sections {
		for _, id := range s.IDs {
			if c, ok := byID[id]; ok {
				c.Section, c.SectionEmoji = s.Name, s.Emoji
				out = append(out, c)
				used[id] = true
			}
		}
	}
	for _, c := range convs {
		if !used[c.ID] {
			out = append(out, c)
		}
	}
	return out
}
func (s *Slack) unreadCounts(ctx context.Context) (map[string]int, error) {
	var r struct {
		Channels []struct {
			ID       string `json:"id"`
			Has      bool   `json:"has_unreads"`
			Display  int    `json:"unread_count_display"`
			Mentions int    `json:"mention_count"`
		} `json:"channels"`
		IMS []struct {
			ID       string `json:"id"`
			Has      bool   `json:"has_unreads"`
			Mentions int    `json:"mention_count"`
		} `json:"ims"`
		MPIMS []struct {
			ID       string `json:"id"`
			Has      bool   `json:"has_unreads"`
			Mentions int    `json:"mention_count"`
		} `json:"mpims"`
	}
	if err := s.call(ctx, "client.counts", url.Values{"token": {s.creds.Token}}, &r); err != nil {
		return nil, err
	}
	out := map[string]int{}
	add := func(id string, has bool, display, mentions int) {
		if display > 0 {
			out[id] = display
		} else if mentions > 0 {
			out[id] = mentions
		} else if has {
			out[id] = 1
		}
	}
	for _, x := range r.Channels {
		add(x.ID, x.Has, x.Display, x.Mentions)
	}
	for _, x := range r.IMS {
		add(x.ID, x.Has, 0, x.Mentions)
	}
	for _, x := range r.MPIMS {
		add(x.ID, x.Has, 0, x.Mentions)
	}
	return out, nil
}
