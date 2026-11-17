package chat

// The Telegram backend speaks MTProto as the user (not as a bot) through
// gotd/td. It needs an api_id/api_hash from https://my.telegram.org and a
// session made by `sltty login telegram`; both live, owner-only, in the config dir.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"

	"github.com/oscarmlage/sltty/internal/diag"
)

// App is the Telegram application sltty signs in as: api_id and api_hash
// from https://my.telegram.org (API development tools).
type App struct {
	ID   int    `json:"api_id"`
	Hash string `json:"api_hash"`
}

// ErrNoSession means there is no usable Telegram session yet.
var ErrNoSession = errors.New("no Telegram session; run `sltty login telegram`")

func userConfigBase() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return base
}

// telegramDir is sltty's config dir, where the Telegram session and app
// credentials live next to the Slack session. Files a previous tgtty
// install left in its own dir are copied over the first time.
var telegramDir = func() string {
	dir := filepath.Join(userConfigBase(), "sltty")
	migrateTelegramOnce.Do(func() { migrateTelegram(filepath.Join(userConfigBase(), "tgtty"), dir) })
	return dir
}

var migrateTelegramOnce sync.Once

func migrateTelegram(from, to string) {
	for _, name := range []string{"telegram-session.json", "telegram-app.json"} {
		dst := filepath.Join(to, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(from, name))
		if err != nil {
			continue
		}
		if os.MkdirAll(to, 0o700) == nil {
			_ = os.WriteFile(dst, b, 0o600)
		}
	}
}

// TelegramSessionPath is the MTProto session (the authorization key: as good as a
// password, kept owner-only).
func TelegramSessionPath() string { return filepath.Join(telegramDir(), "telegram-session.json") }

// AppPath is where the api_id/api_hash are kept.
func AppPath() string { return filepath.Join(telegramDir(), "telegram-app.json") }

// LoadApp reads the application credentials: SLTTY_TELEGRAM_API_ID and
// SLTTY_TELEGRAM_API_HASH (or tgtty's TGTTY_API_ID and TGTTY_API_HASH), else telegram-app.json, else the ones built in.
func LoadApp() (App, error) {
	id, hash := os.Getenv("SLTTY_TELEGRAM_API_ID"), os.Getenv("SLTTY_TELEGRAM_API_HASH")
	if id == "" || hash == "" {
		id, hash = os.Getenv("TGTTY_API_ID"), os.Getenv("TGTTY_API_HASH")
	}
	if id != "" && hash != "" {
		n, err := strconv.Atoi(id)
		if err != nil {
			return App{}, fmt.Errorf("Telegram api_id: %w", err)
		}
		return App{ID: n, Hash: hash}, nil
	}
	b, err := os.ReadFile(AppPath())
	if err != nil {
		if builtinApp.ID != 0 && builtinApp.Hash != "" {
			return builtinApp, nil
		}
		return App{}, err
	}
	var a App
	if err := json.Unmarshal(b, &a); err != nil {
		return App{}, fmt.Errorf("%s: %w", AppPath(), err)
	}
	if a.ID == 0 || a.Hash == "" {
		return App{}, fmt.Errorf("%s: api_id or api_hash missing", AppPath())
	}
	return a, nil
}

// SaveApp writes the application credentials owner-only.
func SaveApp(a App) error {
	if err := os.MkdirAll(telegramDir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(a, "", "  ")
	return os.WriteFile(AppPath(), b, 0o600)
}

// HasTelegramSession reports whether a session file exists.
func HasTelegramSession() bool {
	_, err := os.Stat(TelegramSessionPath())
	return err == nil
}

// Logout removes the saved session (the server side is ended by TelegramLogoutRemote).
func TelegramLogout() error {
	if err := os.Remove(TelegramSessionPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func newClient(app App, updates telegram.UpdateHandler) *telegram.Client {
	// A session file readable by others is tightened first.
	if st, err := os.Stat(TelegramSessionPath()); err == nil && st.Mode().Perm()&0o077 != 0 {
		_ = os.Chmod(TelegramSessionPath(), 0o600)
	}
	_ = os.MkdirAll(telegramDir(), 0o700)
	return telegram.NewClient(app.ID, app.Hash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: TelegramSessionPath()},
		UpdateHandler:  updates,
		Middlewares:    []telegram.Middleware{newPacer()},
		Device: telegram.DeviceConfig{
			DeviceModel:   "sltty",
			SystemVersion: runtime.GOOS,
			AppVersion:    "sltty",
		},
	})
}

// start runs the client in the background and calls f once connected. The
// client keeps running until stop is called; start returns f's error.
func start(ctx context.Context, c *telegram.Client, f func(ctx context.Context) error) (stop func(), err error) {
	runCtx, cancel := context.WithCancel(context.Background())
	ready := make(chan error, 1)
	go func() {
		err := c.Run(runCtx, func(ctx context.Context) error {
			if err := f(ctx); err != nil {
				ready <- err
				return err
			}
			ready <- nil
			<-ctx.Done()
			return nil
		})
		select {
		case ready <- err:
		default:
		}
	}()
	select {
	case err := <-ready:
		if err != nil {
			cancel()
			return nil, err
		}
		return cancel, nil
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
}

// Telegram is the live backend.
type Telegram struct {
	client *telegram.Client
	api    *tg.Client
	stop   func()
	self   *tg.User

	mu       sync.Mutex
	users    map[int64]*tg.User
	chats    map[int64]*tg.Chat
	channels map[int64]*tg.Channel
	files    map[string]tgFile // File IDs seen in messages

	dmu      sync.Mutex // one dialogs fetch at a time
	dialogs  []tg.Dialog
	dialogAt time.Time
	pinned   map[string]bool
	archived map[string]bool
	muted    map[string]bool
	marked   map[string]bool // marked unread (the dialog flag)

	// seen maps the IDs of private and basic group messages (unique per
	// account) to their conversation: deletions name the IDs only.
	seen   map[int]string
	quotes map[string]Quote // "conv/id" → who wrote it, its beginning
	events chan Event
}

// tgFile is where File downloads a picture from.
type tgFile struct {
	thumb, full tg.InputFileLocationClass
}

// NewTelegram connects with the saved session.
func NewTelegram(ctx context.Context) (*Telegram, error) {
	app, err := LoadApp()
	if err != nil {
		return nil, ErrNoSession
	}
	if !HasTelegramSession() {
		return nil, ErrNoSession
	}
	t := newTelegramState()
	d := tg.NewUpdateDispatcher()
	t.handle(d)
	gaps := updates.New(updates.Config{Handler: d})
	t.client = newClient(app, gaps)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	t.stop, err = start(ctx, t.client, func(ctx context.Context) error {
		st, err := t.client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !st.Authorized || st.User == nil {
			return ErrNoSession
		}
		t.self = st.User
		// The updates stream: gaps are filled with getDifference.
		go func() {
			err := gaps.Run(ctx, t.client.API(), st.User.ID, updates.AuthOptions{Forget: true})
			if err != nil && ctx.Err() == nil {
				diag.Printf("telegram updates stopped: %v", err)
			}
		}()
		return nil
	})
	if err != nil {
		return nil, err
	}
	t.api = t.client.API()
	t.users[t.self.ID] = t.self
	diag.Printf("telegram connected")
	return t, nil
}

func newTelegramState() *Telegram {
	return &Telegram{
		users: map[int64]*tg.User{}, chats: map[int64]*tg.Chat{}, channels: map[int64]*tg.Channel{},
		files: map[string]tgFile{}, pinned: map[string]bool{}, archived: map[string]bool{}, muted: map[string]bool{}, marked: map[string]bool{}, seen: map[int]string{},
		quotes: map[string]Quote{},
		events: make(chan Event, 1024),
	}
}

// Close disconnects.
func (t *Telegram) Close() { t.stop() }

func (t *Telegram) Me() string { return userName(t.self) }

func (t *Telegram) Workspace() string {
	if u, ok := t.self.GetUsername(); ok && u != "" {
		return "Telegram · @" + u
	}
	return "Telegram"
}

// remember keeps the users and chats a response carries: their access
// hashes are needed to address them later.
func (t *Telegram) remember(users []tg.UserClass, chats []tg.ChatClass) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, u := range users {
		if u, ok := u.(*tg.User); ok {
			// A "min" user lacks a usable access hash: keep the full one.
			if old, ok := t.users[u.ID]; ok && u.Min {
				if old.AccessHash != 0 {
					u.AccessHash = old.AccessHash
				}
			}
			t.users[u.ID] = u
		}
	}
	for _, c := range chats {
		switch c := c.(type) {
		case *tg.Chat:
			t.chats[c.ID] = c
		case *tg.Channel:
			if old, ok := t.channels[c.ID]; ok && c.Min && old.AccessHash != 0 {
				c.AccessHash = old.AccessHash
			}
			t.channels[c.ID] = c
		}
	}
}

// Conversation IDs: "u<id>" a user, "g<id>" a basic group, "c<id>" a
// channel or supergroup.
func convID(p tg.PeerClass) string {
	switch p := p.(type) {
	case *tg.PeerUser:
		return "u" + strconv.FormatInt(p.UserID, 10)
	case *tg.PeerChat:
		return "g" + strconv.FormatInt(p.ChatID, 10)
	case *tg.PeerChannel:
		return "c" + strconv.FormatInt(p.ChannelID, 10)
	}
	return ""
}

func splitConv(id string) (byte, int64, error) {
	if len(id) < 2 {
		return 0, 0, fmt.Errorf("bad conversation %q", id)
	}
	n, err := strconv.ParseInt(id[1:], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("bad conversation %q", id)
	}
	return id[0], n, nil
}

func (t *Telegram) inputPeer(id string) (tg.InputPeerClass, error) {
	k, n, err := splitConv(id)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch k {
	case 'u':
		if n == t.self.ID {
			return &tg.InputPeerSelf{}, nil
		}
		if u, ok := t.users[n]; ok {
			return &tg.InputPeerUser{UserID: n, AccessHash: u.AccessHash}, nil
		}
	case 'g':
		return &tg.InputPeerChat{ChatID: n}, nil
	case 'c':
		if c, ok := t.channels[n]; ok {
			return &tg.InputPeerChannel{ChannelID: n, AccessHash: c.AccessHash}, nil
		}
	}
	return nil, fmt.Errorf("unknown conversation %q", id)
}

// cleanName is a name for one line: no control or invisible padding
// characters (some names carry Hangul fillers or zero-width spaces), runs
// of white space as one space.
func cleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\u200e', '\u200f', '\u2060', '\ufeff',
			'\u115f', '\u1160', '\u3164', '\uffa0', '\u2800', '\u00ad':
			return -1
		}
		return r
	}, Sanitize(s))
	return strings.Join(strings.Fields(s), " ")
}

func userName(u *tg.User) string {
	if u == nil {
		return "unknown"
	}
	name := cleanName(u.FirstName + " " + u.LastName)
	if name == "" {
		name = u.Username
	}
	if name == "" {
		name = "deleted account"
	}
	return name
}

// peerName is the display name of a user, group or channel.
func (t *Telegram) peerName(p tg.PeerClass) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch p := p.(type) {
	case *tg.PeerUser:
		return userName(t.users[p.UserID])
	case *tg.PeerChat:
		if c, ok := t.chats[p.ChatID]; ok {
			return cleanName(c.Title)
		}
	case *tg.PeerChannel:
		if c, ok := t.channels[p.ChannelID]; ok {
			return cleanName(c.Title)
		}
	}
	return "unknown"
}
