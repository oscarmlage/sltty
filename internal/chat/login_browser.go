package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Login opens an isolated Chromium profile. CDP is used only to read the
// resulting Slack session; the password and any SSO/MFA flow stay in Chrome.
func Login(ctx context.Context, progress func(string)) (Credentials, error) {
	exe, err := findChrome(runtime.GOOS)
	if err != nil {
		return Credentials{}, err
	}
	profile, err := os.MkdirTemp("", "sltty-login-")
	if err != nil {
		return Credentials{}, err
	}
	defer os.RemoveAll(profile)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Credentials{}, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	args := []string{"--user-data-dir=" + profile, "--remote-debugging-port=" + fmt.Sprint(port), "--no-first-run", "--no-default-browser-check", "--disable-extensions", "https://slack.com/signin"}
	cmd := exec.CommandContext(ctx, exe, args...)
	if err := cmd.Start(); err != nil {
		return Credentials{}, err
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	progress("Complete Slack login/SSO in the Chrome window; waiting for the session…")
	deadline := time.Now().Add(loginTimeout)
	sawCookie := false
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return Credentials{}, ctx.Err()
		default:
		}
		token, cookie := readBrowserSession(port)
		if cookie != "" && token == "" {
			if !sawCookie {
				progress("SSO complete; waiting for Slack to finish opening…")
			}
			sawCookie = true
			token = mintBrowserToken(ctx, cookie)
		}
		if token != "" && cookie != "" {
			if c, err := validate(ctx, Credentials{Token: token, Cookie: cookie}); err == nil {
				return c, nil
			}
		}
		time.Sleep(loginPoll)
	}
	return Credentials{}, fmt.Errorf("timed out waiting for Slack login")
}

// candidatesFor is browserCandidates; a variable for the tests.
var candidatesFor = browserCandidates

// browserCandidates are where each platform keeps a Chromium-family browser:
// full paths, or names to look up in PATH.
func browserCandidates(goos string) []string {
	switch goos {
	case "darwin":
		return []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium", "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser", "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"}
	case "windows":
		return []string{`C:\Program Files\Google\Chrome\Application\chrome.exe`, `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`}
	}
	return []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "brave-browser", "microsoft-edge"}
}

func findChrome(goos string) (string, error) {
	candidates := candidatesFor(goos)
	for _, p := range candidates {
		if strings.Contains(p, "/") || strings.Contains(p, `\`) {
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		} else if x, err := exec.LookPath(p); err == nil {
			return x, nil
		}
	}
	return "", fmt.Errorf("Chrome/Chromium not found; install it or use demo mode")
}
func readBrowserSession(port int) (string, string) {
	res, err := cdpClient.Get(fmt.Sprintf("http://127.0.0.1:%d/json", port))
	if err != nil {
		return "", ""
	}
	defer res.Body.Close()
	var tabs []struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if json.NewDecoder(res.Body).Decode(&tabs) != nil {
		return "", ""
	}
	var cookie string
	for _, t := range tabs {
		if t.WebSocketDebuggerURL == "" {
			continue
		}
		token, c := cdpSession(t.WebSocketDebuggerURL)
		if c != "" {
			cookie = c
		}
		if token != "" && c != "" {
			return token, c
		}
	}
	return "", cookie
}

// Recent Slack clients may defer localStorage until after the desktop-app
// handoff. The authenticated boot document remains a useful fallback.
func mintBrowserToken(ctx context.Context, cookie string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, slackClientURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Cookie", "d="+cookie)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	res, err := cdpClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	return xoxc.FindString(string(b))
}
func cdpSession(wsURL string) (string, string) {
	ws, _, err := cdpDialer.Dial(wsURL, nil)
	if err != nil {
		return "", ""
	}
	defer ws.Close()
	id := 0
	call := func(method string, params any) (json.RawMessage, error) {
		id++
		b, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
		if err := ws.WriteMessage(websocket.TextMessage, b); err != nil {
			return nil, err
		}
		for {
			_, raw, err := ws.ReadMessage()
			if err != nil {
				return nil, err
			}
			var r struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
			}
			if json.Unmarshal(raw, &r) == nil && r.ID == id {
				return r.Result, nil
			}
		}
	}
	cookiesRaw, err := call("Network.getAllCookies", map[string]any{})
	if err != nil {
		return "", ""
	}
	var cookies struct {
		Cookies []struct{ Name, Value, Domain string }
	}
	_ = json.Unmarshal(cookiesRaw, &cookies)
	cookie := ""
	for _, c := range cookies.Cookies {
		if c.Name == "d" && strings.HasSuffix(c.Domain, "slack.com") {
			cookie = c.Value
			break
		}
	}
	if cookie == "" {
		return "", ""
	}
	raw, err := call("Runtime.evaluate", map[string]any{"expression": "JSON.stringify(Object.values(localStorage))", "returnByValue": true})
	if err != nil {
		return "", ""
	}
	// raw is the *result* object of Runtime.evaluate, therefore its shape is
	// {"result":{"type":"string","value":"..."}}, not one level deeper.
	var v struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	_ = json.Unmarshal(raw, &v)
	return xoxc.FindString(v.Result.Value), cookie
}

var xoxc = regexp.MustCompile(`xoxc-[A-Za-z0-9-]+`)

// The DevTools port is local and only answers while Chrome is healthy: never
// wait on it forever.
var (
	cdpClient = &http.Client{Timeout: 10 * time.Second}
	// slackClientURL is the authenticated boot document, a token fallback.
	slackClientURL = "https://app.slack.com/client"
	// how long, and how often, Login waits for the session
	loginTimeout = 5 * time.Minute
	loginPoll    = time.Second
	cdpDialer    = &websocket.Dialer{HandshakeTimeout: 10 * time.Second}
)
