package chat

// This backend intentionally uses the Slack web session obtained by
// `sltty login`. It is an experimental personal-use integration: Slack does
// not document xoxc browser tokens as a public API.

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/oscarmlage/sltty/internal/diag"
)

type Credentials struct {
	Token    string `json:"token"`
	Cookie   string `json:"cookie"`
	TeamID   string `json:"team_id"`
	TeamName string `json:"team_name"`
}

type Slack struct {
	creds         Credentials
	me, workspace string
	meName        string // the user's handle: how their messages are signed
	hc            *http.Client
	apiBase       string
	workspaceURL  string // canonical workspace origin, for shareable permalinks
	mu            sync.Mutex
	users         map[string]string
	peopleDirty   bool // users has names the people cache does not
	// urls are where File downloads from: the attachments' thumbnails seen
	// in messages, "emoji:name" and "avatar:id".
	urls map[string]string
}

func NewSlack(ctx context.Context, creds Credentials) (*Slack, error) {
	if creds.Token == "" || creds.Cookie == "" {
		return nil, fmt.Errorf("Slack session is incomplete; run sltty login")
	}
	s := &Slack{creds: creds, hc: newHTTPClient(), apiBase: slackAPI, users: map[string]string{}, urls: map[string]string{}}
	var a struct {
		OK     bool   `json:"ok"`
		UserID string `json:"user_id"`
		User   string `json:"user"`
		TeamID string `json:"team_id"`
		Team   string `json:"team"`
		URL    string `json:"url"`
		Error  string `json:"error"`
	}
	if err := s.call(ctx, "auth.test", nil, &a); err != nil {
		return nil, err
	}
	if !a.OK {
		return nil, fmt.Errorf("Slack authentication failed: %s", a.Error)
	}
	s.me, s.meName, s.workspace = a.UserID, a.User, a.Team
	s.creds.TeamID, s.creds.TeamName = a.TeamID, a.Team
	// Enterprise Grid workspaces can reject requests sent to slack.com/api;
	// auth.test provides their authoritative API host.
	if u, err := url.Parse(a.URL); err == nil && u.Scheme == "https" && strings.HasSuffix(u.Hostname(), "slack.com") {
		s.workspaceURL = u.Scheme + "://" + u.Host
		s.apiBase = strings.TrimRight(u.String(), "/") + "/api/"
	}
	s.loadPeople()
	return s, nil
}

// newHTTPClient refuses to follow redirects off https: the session headers
// must never travel in clear text.
func newHTTPClient() *http.Client {
	return httpClientFactory()
}

// slackAPI is where NewSlack starts; auth.test may point elsewhere (Grid).
// httpClientFactory builds the client. Both are variables for the tests.
var (
	slackAPI          = "https://slack.com/api/"
	httpClientFactory = defaultHTTPClient
)

func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "https" {
			return errors.New("refusing to follow a redirect to a non-https URL")
		}
		return nil
	}}
}

// Me is the user's name as their messages show it (their Slack handle).
func (s *Slack) Me() string        { return cmp.Or(s.meName, "you") }
func (s *Slack) Workspace() string { return s.workspace }

func (s *Slack) call(ctx context.Context, method string, form url.Values, out any) error {
	if form == nil {
		form = url.Values{}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiBase+method, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	diag.Printf("slack request %s", method)
	start := time.Now()
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+s.creds.Token)
	req.Header.Set("Cookie", "d="+s.creds.Cookie)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Origin", "https://app.slack.com")
	res, err := s.hc.Do(req)
	if err != nil {
		diag.Printf("slack %s transport error: %v", method, err)
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		diag.Printf("slack %s HTTP %d", method, res.StatusCode)
		return fmt.Errorf("Slack %s: HTTP %d", method, res.StatusCode)
	}
	body = StripControl(body)
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("Slack %s: %w", method, err)
	}
	var status struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &status) == nil && !status.OK {
		diag.Printf("slack %s API error: %s", method, status.Error)
		return fmt.Errorf("Slack %s: %s", method, status.Error)
	}
	diag.Printf("slack %s OK (%s)", method, time.Since(start).Round(time.Millisecond))
	return nil
}
