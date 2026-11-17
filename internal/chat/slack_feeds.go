package chat

// Threads and Activity feeds.

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/oscarmlage/sltty/internal/diag"
)

// Threads uses Slack's web-client feed of subscribed threads. Unlike channel
// history it includes replies from every conversation, including DMs.
func (s *Slack) Threads(ctx context.Context) ([]ThreadSummary, error) {
	var r struct {
		Threads []struct {
			Root   slackMessage   `json:"root_msg"`
			Latest []slackMessage `json:"latest_replies"`
			Unread []slackMessage `json:"unread_replies"`
		} `json:"threads"`
	}
	form := url.Values{"limit": {"50"}, "priority_mode": {"all"}}
	if s.creds.TeamID != "" {
		form.Set("team_id", s.creds.TeamID)
	}
	if err := s.call(ctx, "subscriptions.thread.getView", form, &r); err != nil {
		return nil, err
	}
	var all []slackMessage
	for _, t := range r.Threads {
		all = append(all, t.Root)
		all = append(all, t.Latest...)
		all = append(all, t.Unread...)
	}
	s.resolveUsers(ctx, all)
	out := make([]ThreadSummary, 0, len(r.Threads))
	for _, t := range r.Threads {
		if t.Root.Channel == "" || t.Root.TS == "" {
			continue
		}
		seen := map[string]bool{}
		var latest []Message
		for _, x := range append(t.Latest, t.Unread...) {
			if x.TS != "" && !seen[x.TS] {
				seen[x.TS] = true
				latest = append(latest, s.message(x))
			}
		}
		slices.SortFunc(latest, func(a, b Message) int { return a.At.Compare(b.At) })
		out = append(out, ThreadSummary{ConvID: t.Root.Channel, Root: s.message(t.Root), Latest: latest, Replies: t.Root.ReplyCount, Unread: len(t.Unread)})
	}
	slices.SortFunc(out, func(a, b ThreadSummary) int { return b.Root.LastReply.Compare(a.Root.LastReply) })
	return out, nil
}

// Activity reads Slack's native notification feed. The feed contains message
// references, so each entry is hydrated from its channel or thread before it
// reaches the TUI.
func (s *Slack) Activity(ctx context.Context) ([]Activity, error) {
	types := "at_user,at_user_group,at_channel,at_everyone,keyword,thread_v2,message_reaction,dm,bot_dm_bundle,channel"
	var r struct {
		Items []struct {
			Key    string `json:"key"`
			FeedTS string `json:"feed_ts"`
			Unread bool   `json:"is_unread"`
			Item   struct {
				Type     string       `json:"type"`
				Message  slackMessage `json:"message"`
				Reaction struct {
					User string `json:"user"`
					Name string `json:"name"`
				} `json:"reaction"`
				Bundle struct {
					Payload struct {
						// A DM: the newest message of the conversation.
						DM struct {
							Latest slackMessage `json:"latest_message"`
						} `json:"dm_entry"`
						// An app's DMs (bot_dm_bundle).
						Message slackMessage `json:"message"`
						Thread  struct {
							Channel string `json:"channel_id"`
							Latest  string `json:"latest_ts"`
							Root    string `json:"thread_ts"`
							Unread  int    `json:"unread_msg_count"`
						} `json:"thread_entry"`
					} `json:"payload"`
				} `json:"bundle_info"`
			} `json:"item"`
		} `json:"items"`
	}
	form := url.Values{"types": {types}, "mode": {"chrono_v1"}, "limit": {"50"}, "archive_only": {"false"}, "unread_only": {"false"}, "priority_only": {"false"}, "exclude_automations": {"false"}}
	if s.creds.TeamID != "" {
		form.Set("team_id", s.creds.TeamID)
	}
	if err := s.call(ctx, "activity.feed", form, &r); err != nil {
		return nil, err
	}
	out := make([]Activity, 0, len(r.Items))
	for _, x := range r.Items {
		kind := ActMention
		switch x.Item.Type {
		case "thread_v2":
			kind = ActReply
		case "message_reaction":
			kind = ActReaction
		case "dm", "bot_dm_bundle":
			kind = ActDM
		case "at_user", "at_user_group", "at_channel", "at_everyone", "keyword", "channel":
		default:
			continue
		}
		m, conv, root := x.Item.Message, x.Item.Message.Channel, x.Item.Message.ThreadTS
		// Bundles carry their message in bundle_info, not in item.message.
		switch p := x.Item.Bundle.Payload; x.Item.Type {
		case "thread_v2":
			conv, root, m.TS = p.Thread.Channel, p.Thread.Root, p.Thread.Latest
		case "dm":
			m = p.DM.Latest
			conv, root = m.Channel, m.ThreadTS
		case "bot_dm_bundle":
			if m.Channel == "" {
				m = p.Message
			}
			conv, root = m.Channel, m.ThreadTS
		}
		if conv == "" || m.TS == "" {
			continue
		}
		hydrated, err := s.activityMessage(ctx, conv, root, m.TS)
		if err != nil {
			diag.Printf("slack activity message %s/%s: %v", conv, m.TS, err)
			hydrated = s.message(m)
		}
		at, _ := parseSlackTS(x.FeedTS)
		if at.IsZero() {
			at = hydrated.At
		}
		by, byID := hydrated.User, hydrated.UserID
		reaction := ""
		if kind == ActReaction {
			s.resolveUser(ctx, x.Item.Reaction.User)
			by, byID = s.user(x.Item.Reaction.User), x.Item.Reaction.User
			reaction = ":" + strings.Trim(x.Item.Reaction.Name, ":") + ":"
		}
		out = append(out, Activity{ID: x.Key, Kind: kind, ConvID: conv, RootTS: root, Msg: hydrated, By: by, ByID: byID, Emoji: reaction, At: at, Unread: x.Unread})
	}
	return out, nil
}

func (s *Slack) activityMessage(ctx context.Context, channel, root, ts string) (Message, error) {
	if root != "" {
		rs, err := s.Replies(ctx, channel, root)
		if err != nil {
			return Message{}, err
		}
		for _, m := range rs {
			if m.TS == ts {
				return m, nil
			}
		}
	} else {
		var r struct {
			Messages []slackMessage `json:"messages"`
		}
		if err := s.call(ctx, "conversations.history", url.Values{"channel": {channel}, "oldest": {ts}, "latest": {ts}, "inclusive": {"true"}, "limit": {"1"}}, &r); err != nil {
			return Message{}, err
		}
		if len(r.Messages) > 0 {
			s.resolveUsers(ctx, r.Messages)
			return s.message(r.Messages[0]), nil
		}
	}
	return Message{}, fmt.Errorf("activity message %s not found", ts)
}
