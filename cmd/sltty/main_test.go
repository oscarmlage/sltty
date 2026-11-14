package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/tui"
)

// sandbox keeps config, state, log and session inside the test.
func sandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "cfg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	return dir
}

func do(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := run(args, &out, &errOut)
	return out.String(), errOut.String(), err
}

func TestKeysAndConfigPrint(t *testing.T) {
	sandbox(t)
	out, _, err := do(t, "keys")
	if err != nil || !strings.Contains(out, `"react"`) {
		t.Fatalf("keys: %v\n%s", err, out)
	}
	out, _, err = do(t, "config")
	if err != nil || !strings.Contains(out, "takes (all optional)") || !strings.Contains(out, "sidebar_width") {
		t.Fatalf("config: %v\n%s", err, out)
	}
}

func TestUsageAndUnknownCommand(t *testing.T) {
	sandbox(t)
	if _, errOut, err := do(t, "frobnicate"); err == nil || !strings.Contains(err.Error(), "unknown command") || !strings.Contains(errOut, "usage: sltty") {
		t.Fatalf("unknown command: %v\n%s", err, errOut)
	}
	if _, _, err := do(t, "-h"); err != nil {
		t.Fatalf("-h is not an error: %v", err)
	}
	if _, _, err := do(t, "-nope"); err == nil {
		t.Fatal("an unknown flag must be an error")
	}
	if _, _, err := do(t, "logout", "extra"); err == nil {
		t.Fatal("logout takes no arguments")
	}
	for _, args := range [][]string{{"login", "--browser", "--desktop"}, {"login", "stray"}, {"login", "--nope"}} {
		if _, _, err := do(t, args...); err == nil || !strings.Contains(err.Error(), "usage: sltty login") && !strings.Contains(err.Error(), "flag provided") {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestLoginSavesAndLogoutRemoves(t *testing.T) {
	sandbox(t)
	oldB, oldD := loginBrowser, loginDesktop
	defer func() { loginBrowser, loginDesktop = oldB, oldD }()
	var via string
	loginBrowser = func(ctx context.Context, progress func(string)) (chat.Credentials, error) {
		via = "browser"
		progress("waiting")
		return chat.Credentials{Token: "xoxc-t", Cookie: "xoxd-c", TeamName: "Acme"}, nil
	}
	loginDesktop = func(context.Context) (chat.Credentials, error) {
		via = "desktop"
		return chat.Credentials{Token: "xoxc-t", Cookie: "xoxd-c", TeamName: "Desk"}, nil
	}
	out, errOut, err := do(t, "login")
	if err != nil || via != "browser" || !strings.Contains(out, "signed in to Acme") || !strings.Contains(errOut, "waiting") {
		t.Fatalf("login: %v %q\n%s\n%s", err, via, out, errOut)
	}
	if _, err := os.Stat(chat.SessionPath()); err != nil {
		t.Fatal("session not saved")
	}
	out, _, err = do(t, "login", "--desktop")
	if err != nil || via != "desktop" || !strings.Contains(out, "Desk") {
		t.Fatalf("desktop login: %v %q %s", err, via, out)
	}
	if _, _, err := do(t, "logout"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(chat.SessionPath()); !os.IsNotExist(err) {
		t.Fatal("session still there after logout")
	}
	loginBrowser = func(context.Context, func(string)) (chat.Credentials, error) {
		return chat.Credentials{}, errors.New("no chrome")
	}
	if _, _, err := do(t, "login"); err == nil || !strings.Contains(err.Error(), "no chrome") {
		t.Fatalf("a failed login must fail: %v", err)
	}
}

func captureTUI(t *testing.T) *struct {
	b    chat.Backend
	opts tui.Options
} {
	t.Helper()
	got := &struct {
		b    chat.Backend
		opts tui.Options
	}{}
	old := runTUI
	runTUI = func(b chat.Backend, o tui.Options) error { got.b, got.opts = b, o; return nil }
	t.Cleanup(func() { runTUI = old })
	return got
}

func TestRunsTheDemoByDefault(t *testing.T) {
	dir := sandbox(t)
	got := captureTUI(t)
	cfg := filepath.Join(dir, "my.json")
	if err := os.WriteFile(cfg, []byte(`{"theme":"nord","custom_emoji":false,"sections":[{"name":"Team","channels":["a"],"collapsed":true}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := do(t, "-config", cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.b.(*chat.Fake); !ok {
		t.Fatalf("backend %T, want the demo", got.b)
	}
	o := got.opts
	if o.Theme != "nord" || o.CustomEmoji || len(o.Sections) != 1 || !o.Sections[0].Collapsed || !o.ConfigFound || o.ConfigPath != cfg || o.Version != version {
		t.Fatalf("options: %+v", o)
	}
	if _, _, err := do(t, "-config", filepath.Join(dir, "typo.json")); err != nil || got.opts.ConfigFound {
		t.Fatalf("a missing config is the defaults: %v %+v", err, got.opts)
	}
}

func TestBadConfigStopsEarly(t *testing.T) {
	dir := sandbox(t)
	captureTUI(t)
	cfg := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(cfg, []byte(`{"thme":"x"}`), 0o600)
	if _, _, err := do(t, "-config", cfg); err == nil {
		t.Fatal("a typo in the config must be an error")
	}
}

func TestSavedSessionIsUsedUnlessDemo(t *testing.T) {
	sandbox(t)
	got := captureTUI(t)
	if err := chat.SaveSession(chat.Credentials{Token: "xoxc-t", Cookie: "xoxd-c"}); err != nil {
		t.Fatal(err)
	}
	old := newSlack
	defer func() { newSlack = old }()
	newSlack = func(context.Context, chat.Credentials) (chat.Backend, error) {
		return chat.NewFake(), errors.New("invalid_auth")
	}
	if _, _, err := do(t); err == nil || !strings.Contains(err.Error(), "run `sltty login`") {
		t.Fatalf("a dead session must say how to fix it: %v", err)
	}
	if _, _, err := do(t, "-demo"); err != nil || got.b == nil {
		t.Fatalf("-demo must ignore the session: %v", err)
	}
	live := chat.NewFake()
	newSlack = func(context.Context, chat.Credentials) (chat.Backend, error) { return live, nil }
	if _, _, err := do(t); err != nil || got.b != chat.Backend(live) {
		t.Fatalf("the live backend was not used: %v", err)
	}
	// an unreadable session is an error, not a silent demo
	if err := os.WriteFile(chat.SessionPath(), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := do(t); err == nil || !strings.Contains(err.Error(), "reading Slack session") {
		t.Fatalf("broken session: %v", err)
	}
}
