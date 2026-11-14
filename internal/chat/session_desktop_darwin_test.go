//go:build darwin

package chat

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
	"golang.org/x/crypto/pbkdf2"
)

// encrypt builds a Chromium "v10" cookie value the way Slack Desktop stores it.
func encrypt(t *testing.T, password, plain string, hashPrefix bool) string {
	t.Helper()
	key := pbkdf2.Key([]byte(password), []byte("saltysalt"), 1003, 16, sha1.New)
	data := []byte(plain)
	if hashPrefix {
		h := sha256.Sum256([]byte(".slack.com"))
		data = append(h[:], data...)
	}
	pad := aes.BlockSize - len(data)%aes.BlockSize
	data = append(data, bytes.Repeat([]byte{byte(pad)}, pad)...)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(data))
	cipher.NewCBCEncrypter(block, bytes.Repeat([]byte{' '}, aes.BlockSize)).CryptBlocks(out, data)
	return hex.EncodeToString(append([]byte("v10"), out...))
}

// profile makes a Slack Desktop profile with a cookie row and workspace tokens.
func profile(t *testing.T, cookieSQL string, tokens string) string {
	t.Helper()
	if _, err := os.Stat(sqliteBin); err != nil {
		t.Skip("sqlite3 not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "Library", "Application Support", "Slack")
	if err := os.MkdirAll(filepath.Join(dir, "Network"), 0o700); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "Network", "Cookies")
	sql := "CREATE TABLE cookies (host_key TEXT, name TEXT, value TEXT, encrypted_value BLOB);" + cookieSQL
	if out, err := exec.Command(sqliteBin, db, sql).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v %s", err, out)
	}
	if tokens != "" {
		ldb, err := leveldb.OpenFile(filepath.Join(dir, "Local Storage", "leveldb"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := ldb.Put([]byte("_https://app.slack.com\x00\x01localConfig_v2"), append([]byte{1}, tokens...), nil); err != nil {
			t.Fatal(err)
		}
		_ = ldb.Put([]byte("other"), []byte("x"), nil)
		_ = ldb.Put([]byte("zzlocalConfig_v2"), []byte("not json"), nil)
		if err := ldb.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func withKeychain(t *testing.T, pw string, err error) {
	t.Helper()
	old := keychainPassword
	keychainPassword = func() ([]byte, error) { return []byte(pw + "\n"), err }
	t.Cleanup(func() { keychainPassword = old })
}

func row(value, enc string) string {
	return "INSERT INTO cookies VALUES ('.slack.com','d','" + value + "',x'" + enc + "');"
}

func TestDesktopCookieDecryption(t *testing.T) {
	withKeychain(t, "pw", nil)
	for name, tc := range map[string]struct {
		sql  string
		want string
		err  string
	}{
		"plain value":     {row("xoxd-plain", ""), "xoxd-plain", ""},
		"v24 hash prefix": {row("", encrypt(t, "pw", "xoxd-secret", true)), "xoxd-secret", ""},
		"older, no hash":  {row("", encrypt(t, "pw", "xoxd-old", false)), "xoxd-old", ""},
		"no cookie":       {"", "", "no signed-in session"},
		"not v10":         {row("", "7632"+"3030"), "", "unsupported"},
		"wrong password":  {row("", encrypt(t, "other", "xoxd-secret", true)), "", "Slack Desktop cookie"},
		"not a cookie":    {row("", encrypt(t, "pw", "hello world, not slack", false)), "", "Slack Desktop cookie"},
		"short":           {row("", hex.EncodeToString([]byte("v10abc"))), "", "invalid"},
	} {
		dir := profile(t, tc.sql, "")
		got, err := desktopCookie(dir)
		if tc.err == "" && (err != nil || got != tc.want) {
			t.Errorf("%s: %q %v", name, got, err)
		}
		if tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)) {
			t.Errorf("%s: got %q %v, want an error with %q", name, got, err, tc.err)
		}
	}
}

func TestDesktopCookieNeedsTheKeychainAndSqlite(t *testing.T) {
	dir := profile(t, row("", encrypt(t, "pw", "xoxd-secret", true)), "")
	withKeychain(t, "", os.ErrPermission)
	if _, err := desktopCookie(dir); err == nil || !strings.Contains(err.Error(), "Keychain") {
		t.Fatalf("keychain refused: %v", err)
	}
	old := sqliteBin
	defer func() { sqliteBin = old }()
	sqliteBin = "/nonexistent/sqlite3"
	if _, err := desktopCookie(dir); err == nil || !strings.Contains(err.Error(), "sqlite3") {
		t.Fatalf("no sqlite3: %v", err)
	}
	if _, err := desktopCookie(t.TempDir()); err == nil {
		t.Fatal("a profile without a cookie database")
	}
}

func TestDesktopCookieInTheOldLocation(t *testing.T) {
	withKeychain(t, "pw", nil)
	dir := profile(t, row("xoxd-here", ""), "")
	if err := os.Rename(filepath.Join(dir, "Network", "Cookies"), filepath.Join(dir, "Cookies")); err != nil {
		t.Fatal(err)
	}
	if got, err := desktopCookie(dir); err != nil || got != "xoxd-here" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestDesktopTokens(t *testing.T) {
	dir := profile(t, "", `{"teams":{"T1":{"token":"xoxc-one"},"T2":{"token":"xoxp-not-ours"},"T3":{"token":"xoxc-three"}}}`)
	got, err := desktopTokens(dir)
	if err != nil || len(got) != 2 || got["T1"] != "xoxc-one" || got["T3"] != "xoxc-three" {
		t.Fatalf("tokens: %v %v", got, err)
	}
	if _, err := desktopTokens(t.TempDir()); err == nil {
		t.Fatal("a profile without local storage")
	}
	empty := profile(t, "", `{"teams":{}}`)
	if _, err := desktopTokens(empty); err == nil || !strings.Contains(err.Error(), "no workspace tokens") {
		t.Fatalf("no tokens: %v", err)
	}
}

func TestSlackDesktopDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := slackDesktopDir(); err == nil {
		t.Fatal("no Slack Desktop here")
	}
	sandboxed := filepath.Join(os.Getenv("HOME"), "Library", "Containers", "com.tinyspeck.slackmacgap", "Data", "Library", "Application Support", "Slack")
	if err := os.MkdirAll(sandboxed, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := slackDesktopDir(); err != nil || got != sandboxed {
		t.Fatalf("%q %v", got, err)
	}
}

func TestLoginDesktopPicksAWorkingWorkspace(t *testing.T) {
	withKeychain(t, "pw", nil)
	profile(t, row("", encrypt(t, "pw", "xoxd-secret", true)), `{"teams":{"T1":{"token":"xoxc-dead"},"T2":{"token":"xoxc-live"}}}`)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Header.Get("Authorization") == "Bearer xoxc-live" && r.Header.Get("Cookie") == "d=xoxd-secret" {
			_, _ = w.Write([]byte(`{"ok":true,"user_id":"UME","team_id":"T2","team":"Live Inc"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	}))
	defer srv.Close()
	oldAPI, oldFac := slackAPI, httpClientFactory
	defer func() { slackAPI, httpClientFactory = oldAPI, oldFac }()
	slackAPI, httpClientFactory = srv.URL+"/api/", srv.Client
	c, err := LoginDesktop(context.Background())
	if err != nil || c.TeamName != "Live Inc" || c.Token != "xoxc-live" || c.Cookie != "xoxd-secret" {
		t.Fatalf("%+v %v", c, err)
	}
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	})
	if _, err := LoginDesktop(context.Background()); err == nil || !strings.Contains(err.Error(), "no valid Slack Desktop workspace") {
		t.Fatalf("every workspace rejected: %v", err)
	}
}

func TestLoginDesktopErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := LoginDesktop(context.Background()); err == nil {
		t.Fatal("no profile")
	}
	withKeychain(t, "pw", nil)
	dir := profile(t, row("xoxd-plain", ""), "") // a cookie, but no local storage
	_ = dir
	if _, err := LoginDesktop(context.Background()); err == nil {
		t.Fatal("no tokens")
	}
	dir = profile(t, "", `{"teams":{"T1":{"token":"xoxc-a"}}}`) // tokens, but no cookie database
	if _, err := LoginDesktop(context.Background()); err == nil {
		t.Fatal("no cookie")
	}
	_ = dir
}
