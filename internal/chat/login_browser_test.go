package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestMain doubles as a fake Chrome: Login runs this test binary as the
// browser, with SLTTY_FAKE_CHROME telling it what to serve on the DevTools
// port it is given.
func TestMain(m *testing.M) {
	if mode := os.Getenv("SLTTY_FAKE_CHROME"); mode != "" {
		fakeChrome(mode)
		return
	}
	os.Exit(m.Run())
}

func fakeChrome(mode string) {
	port := ""
	for _, a := range os.Args {
		if v, ok := strings.CutPrefix(a, "--remote-debugging-port="); ok {
			port = v
		}
	}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		tabs := []map[string]string{{"title": "no debugger"}, {"webSocketDebuggerUrl": "ws://127.0.0.1:" + port + "/ws"}}
		_ = json.NewEncoder(w).Encode(tabs)
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			var req struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
			}
			if c.ReadJSON(&req) != nil {
				return
			}
			_ = c.WriteJSON(map[string]any{"method": "Noise.event"}) // events are not answers
			var result any
			switch req.Method {
			case "Network.getAllCookies":
				cookies := []map[string]string{{"name": "other", "value": "x", "domain": ".slack.com"}}
				if mode != "nocookie" {
					cookies = append(cookies, map[string]string{"name": "d", "value": "xoxd-cookie", "domain": ".slack.com"})
				}
				result = map[string]any{"cookies": cookies}
			case "Runtime.evaluate":
				value := `["{\"token\":\"xoxc-abc-123\"}"]`
				if mode == "notoken" {
					value = `[]`
				}
				result = map[string]any{"result": map[string]any{"type": "string", "value": value}}
			}
			_ = c.WriteJSON(map[string]any{"id": req.ID, "result": result})
		}
	})
	_ = http.ListenAndServe("127.0.0.1:"+port, mux)
}

// loginEnv points Login at a fake Chrome and a fake Slack and makes it quick.
func loginEnv(t *testing.T, mode string, handlers map[string]handler) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client" {
			if r.Header.Get("Cookie") != "d=xoxd-cookie" {
				t.Errorf("boot page asked with cookie %q", r.Header.Get("Cookie"))
			}
			_, _ = w.Write([]byte("<html>boot xoxc-minted-77 end</html>"))
			return
		}
		method := strings.TrimPrefix(r.URL.Path, "/api/")
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if h, ok := handlers[method]; ok {
			_ = json.NewEncoder(w).Encode(h(r.Form))
			return
		}
		_, _ = w.Write([]byte(`{"ok":false,"error":"unknown_method"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SLTTY_FAKE_CHROME", mode)
	exe, _ := os.Executable()
	oldCand, oldAPI, oldFac, oldCDP, oldClient, oldT, oldP := candidatesFor, slackAPI, httpClientFactory, cdpClient, slackClientURL, loginTimeout, loginPoll
	candidatesFor = func(string) []string { return []string{exe} }
	slackAPI, httpClientFactory, cdpClient, slackClientURL = srv.URL+"/api/", srv.Client, srv.Client(), srv.URL+"/client"
	loginTimeout, loginPoll = 20*time.Second, 50*time.Millisecond
	t.Cleanup(func() {
		candidatesFor, slackAPI, httpClientFactory, cdpClient, slackClientURL, loginTimeout, loginPoll = oldCand, oldAPI, oldFac, oldCDP, oldClient, oldT, oldP
	})
}

var authOK = map[string]handler{"auth.test": func(url.Values) any {
	return obj{"ok": true, "user_id": "UME", "team_id": "T9", "team": "Acme"}
}}

func TestLoginReadsTheBrowserSession(t *testing.T) {
	loginEnv(t, "ok", authOK)
	var said []string
	c, err := Login(context.Background(), func(s string) { said = append(said, s) })
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "xoxc-abc-123" || c.Cookie != "xoxd-cookie" || c.TeamName != "Acme" || c.TeamID != "T9" {
		t.Fatalf("credentials: %+v", c)
	}
	if len(said) == 0 || !strings.Contains(said[0], "Complete Slack login") {
		t.Fatalf("progress: %q", said)
	}
}

func TestLoginFallsBackToTheBootPage(t *testing.T) {
	loginEnv(t, "notoken", authOK)
	var said []string
	c, err := Login(context.Background(), func(s string) { said = append(said, s) })
	if err != nil || c.Token != "xoxc-minted-77" {
		t.Fatalf("%+v %v", c, err)
	}
	if !strings.Contains(strings.Join(said, "|"), "SSO complete") {
		t.Fatalf("progress: %q", said)
	}
}

func TestLoginTimesOutAndStops(t *testing.T) {
	loginEnv(t, "nocookie", authOK)
	loginTimeout = 400 * time.Millisecond
	if _, err := Login(context.Background(), func(string) {}); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout: %v", err)
	}
	loginTimeout = time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := Login(ctx, func(string) {}); err == nil {
		t.Fatal("a cancelled login must fail")
	}
}

func TestLoginKeepsWaitingWhileSlackRejects(t *testing.T) {
	loginEnv(t, "ok", nil) // auth.test unknown: every validation fails
	loginTimeout = 500 * time.Millisecond
	if _, err := Login(context.Background(), func(string) {}); err == nil {
		t.Fatal("a session Slack rejects must not be returned")
	}
}

func TestLoginWithoutABrowser(t *testing.T) {
	old := candidatesFor
	defer func() { candidatesFor = old }()
	candidatesFor = func(string) []string { return []string{"/nope/chrome", "no-such-browser-xyz"} }
	if _, err := Login(context.Background(), func(string) {}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("no browser: %v", err)
	}
	candidatesFor = func(string) []string { return []string{filepath.Join(t.TempDir(), "x")} }
	dir := t.TempDir()
	bad := filepath.Join(dir, "chrome")
	if err := os.WriteFile(bad, []byte("not executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	candidatesFor = func(string) []string { return []string{bad} }
	if _, err := Login(context.Background(), func(string) {}); err == nil {
		t.Fatal("a browser that cannot start must fail")
	}
}

func TestBrowserCandidatesPerPlatform(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "Google Chrome.app", "windows": "chrome.exe", "linux": "chromium"} {
		if got := strings.Join(browserCandidates(goos), "|"); !strings.Contains(got, want) {
			t.Errorf("%s: %s", goos, got)
		}
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "brave-browser")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if got, err := findChrome("linux"); err != nil || got != fake {
		t.Fatalf("PATH lookup: %q %v", got, err)
	}
	candidatesFor = func(string) []string { return []string{"/nope/chrome"} }
	defer func() { candidatesFor = browserCandidates }()
	if got, err := findChrome("darwin"); err == nil {
		t.Fatalf("a missing path was found: %q", got)
	}
	candidatesFor = func(string) []string { return []string{fake} }
	if got, err := findChrome("darwin"); err != nil || got != fake {
		t.Fatalf("an existing path: %q %v", got, err)
	}
}

func TestReadBrowserSessionIgnoresBrokenEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("not json")) }))
	defer srv.Close()
	port := 0
	_, _ = fmt.Sscanf(srv.URL[strings.LastIndex(srv.URL, ":")+1:], "%d", &port)
	if tok, ck := readBrowserSession(port); tok != "" || ck != "" {
		t.Fatalf("%q %q", tok, ck)
	}
	if tok, ck := readBrowserSession(1); tok != "" || ck != "" { // nothing listens
		t.Fatalf("%q %q", tok, ck)
	}
	if tok, ck := cdpSession("ws://127.0.0.1:1/x"); tok != "" || ck != "" {
		t.Fatalf("%q %q", tok, ck)
	}
	old := slackClientURL
	defer func() { slackClientURL = old }()
	slackClientURL = "https://127.0.0.1:1/client" // nothing listens
	if got := mintBrowserToken(context.Background(), "c"); got != "" {
		t.Fatalf("mint: %q", got)
	}
	slackClientURL = "://bad url"
	if got := mintBrowserToken(context.Background(), "c"); got != "" {
		t.Fatalf("mint: %q", got)
	}
}
