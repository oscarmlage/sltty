package chat

// People and bots: names, avatars, presence.

import (
	"cmp"
	"context"
	"net/url"
	"strconv"
	"strings"
)

// resolveUser looks a person up once: their name and their avatar's URL
// (yours too: you are "you", but with your face).
func (s *Slack) resolveUser(ctx context.Context, id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	_, ok := s.users[id]
	s.mu.Unlock()
	if ok {
		return
	}
	var r struct {
		User struct {
			Name     string `json:"name"`
			RealName string `json:"real_name"`
			Profile  struct {
				Image72 string `json:"image_72"`
			} `json:"profile"`
		} `json:"user"`
	}
	if s.call(ctx, "users.info", url.Values{"user": {id}}, &r) == nil {
		n := r.User.Name
		if n == "" {
			n = r.User.RealName
		}
		s.mu.Lock()
		s.users[id] = n
		if u := r.User.Profile.Image72; u != "" {
			s.urls["avatar:"+id] = u
		}
		s.mu.Unlock()
	}
}

// botIcon keeps where a bot's avatar is when its message says.
func (s *Slack) botIcon(m slackMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := ""
	if m.Icons != nil {
		u = s.best(*m.Icons)
	}
	if m.BotProfile != nil {
		u = cmp.Or(u, s.best(m.BotProfile.Icons))
	}
	if k := "avatar:" + m.BotID; u != "" && s.urls[k] == "" {
		s.urls[k] = u
	}
}

// resolveBot looks a bot up once (bots.info): its name, for messages
// posted without one, and its avatar.
func (s *Slack) resolveBot(ctx context.Context, id string) {
	s.mu.Lock()
	_, ok := s.users[id]
	s.mu.Unlock()
	if ok || id == "" {
		return
	}
	var r struct {
		Bot struct {
			Name  string     `json:"name"`
			Icons slackIcons `json:"icons"`
		} `json:"bot"`
	}
	err := s.call(ctx, "bots.info", url.Values{"bot": {id}}, &r)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[id] = cmp.Or(r.Bot.Name, id) // asked once, found or not
	if k, u := "avatar:"+id, s.best(r.Bot.Icons); err == nil && u != "" && s.urls[k] == "" {
		s.urls[k] = u
	}
}

// resolveAuthor is resolveUser, or resolveBot for a bot's id (B…) whose
// avatar no message has told.
func (s *Slack) resolveAuthor(ctx context.Context, id string) {
	if strings.HasPrefix(id, "B") {
		s.mu.Lock()
		known := s.urls["avatar:"+id] != ""
		s.mu.Unlock()
		if !known {
			s.resolveBot(ctx, id)
		}
		return
	}
	s.resolveUser(ctx, id)
}

// resolveUsers looks up the authors of msgs and the people they mention.
func (s *Slack) resolveUsers(ctx context.Context, msgs []slackMessage) {
	for _, m := range msgs {
		s.resolveUser(ctx, m.User)
		if m.User == "" && m.Username == "" {
			s.resolveBot(ctx, m.BotID) // for its name
		}
		for _, id := range richUsers(m.Blocks) {
			s.resolveUser(ctx, id)
		}
		for _, r := range m.Reactions {
			for _, id := range r.Users {
				s.resolveUser(ctx, id)
			}
		}
		for _, sub := range mention.FindAllStringSubmatch(m.Text, -1) {
			if ref, _, _ := strings.Cut(sub[1], "|"); strings.HasPrefix(ref, "@") {
				s.resolveUser(ctx, ref[1:])
			}
		}
	}
}

func (s *Slack) SetPresence(ctx context.Context, away bool) error {
	p := "auto"
	if away {
		p = "away"
	}
	var r struct{}
	return s.call(ctx, "users.setPresence", url.Values{"presence": {p}}, &r)
}

func (s *Slack) SetSnooze(ctx context.Context, minutes int) error {
	var r struct{}
	if minutes <= 0 {
		return s.call(ctx, "dnd.endSnooze", nil, &r)
	}
	return s.call(ctx, "dnd.setSnooze", url.Values{"num_minutes": {strconv.Itoa(minutes)}}, &r)
}
