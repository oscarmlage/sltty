package chat

// People and bots: names, avatars, presence.

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
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
		s.peopleDirty = true
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
	s.peopleDirty = true
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

// resolveUsers looks up distinct authors, mentions and reactors concurrently.
// A busy channel can otherwise make its first history load wait for dozens of
// sequential users.info calls. Eight workers keep that burst polite to Slack.
func (s *Slack) resolveUsers(ctx context.Context, msgs []slackMessage) {
	users, bots := map[string]bool{}, map[string]bool{}
	for _, m := range msgs {
		if m.User != "" {
			users[m.User] = true
		} else if m.Username == "" && m.BotID != "" {
			bots[m.BotID] = true
		}
		for _, id := range richUsers(m.Blocks) {
			users[id] = true
		}
		for _, r := range m.Reactions {
			for _, id := range r.Users {
				users[id] = true
			}
		}
		for _, sub := range mention.FindAllStringSubmatch(m.Text, -1) {
			if ref, _, _ := strings.Cut(sub[1], "|"); strings.HasPrefix(ref, "@") {
				users[ref[1:]] = true
			}
		}
	}
	jobs := make(chan func(), len(users)+len(bots))
	for id := range users {
		id := id
		jobs <- func() { s.resolveUser(ctx, id) }
	}
	for id := range bots {
		id := id
		jobs <- func() { s.resolveBot(ctx, id) }
	}
	close(jobs)
	var wg sync.WaitGroup
	for range min(8, len(users)+len(bots)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				job()
			}
		}()
	}
	wg.Wait()
	s.savePeople()
}

// The people cache keeps the names and avatar addresses users.info and
// bots.info gave, a week, so a new session does not ask for them all again
// (dozens of calls on the first conversations opened). It holds nothing
// more than the sidebar and messages show, owner-only in the cache dir.
const peopleTTL = 7 * 24 * time.Hour

type peopleFile struct {
	Saved time.Time         `json:"saved"`
	Names map[string]string `json:"names"`
	URLs  map[string]string `json:"avatars"`
}

func (s *Slack) peoplePath() string {
	base, err := os.UserCacheDir()
	if err != nil || s.creds.TeamID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s.creds.TeamID + "/" + s.me))
	return filepath.Join(base, "sltty", "people-"+hex.EncodeToString(sum[:8])+".json")
}

func (s *Slack) loadPeople() {
	p := s.peoplePath()
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var f peopleFile
	if json.Unmarshal(b, &f) != nil || time.Since(f.Saved) > peopleTTL {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, n := range f.Names {
		if _, ok := s.users[id]; !ok && id != s.me {
			s.users[id] = n
		}
	}
	for k, u := range f.URLs {
		if strings.HasPrefix(k, "avatar:") && strings.HasPrefix(u, "https://") && s.urls[k] == "" {
			s.urls[k] = u
		}
	}
}

func (s *Slack) savePeople() {
	p := s.peoplePath()
	s.mu.Lock()
	if p == "" || !s.peopleDirty {
		s.mu.Unlock()
		return
	}
	s.peopleDirty = false
	f := peopleFile{Saved: time.Now(), Names: map[string]string{}, URLs: map[string]string{}}
	for id, n := range s.users {
		f.Names[id] = n
	}
	for k, u := range s.urls {
		if strings.HasPrefix(k, "avatar:") {
			f.URLs[k] = u
		}
	}
	s.mu.Unlock()
	b, err := json.Marshal(f)
	if err != nil || os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

func (s *Slack) Presence(ctx context.Context) (bool, error) {
	var r struct {
		Presence string `json:"presence"`
	}
	if err := s.call(ctx, "users.getPresence", url.Values{"user": {s.me}}, &r); err != nil {
		return false, err
	}
	return r.Presence == "away", nil
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
