//go:build darwin

package chat

// Desktop login is deliberately separate from browser login. It reads only the
// Slack Electron profile's d cookie and localConfig_v2 tokens, then feeds the
// same Credentials/NewSlack path used by the rest of the TUI.

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"golang.org/x/crypto/pbkdf2"
)

// The system tools this relies on; variables for the tests.
var (
	sqliteBin        = "/usr/bin/sqlite3"
	keychainPassword = func() ([]byte, error) {
		return exec.Command("/usr/bin/security", "find-generic-password", "-w", "-s", "Slack Safe Storage").Output()
	}
)

func slackDesktopDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, p := range []string{
		filepath.Join(home, "Library", "Application Support", "Slack"),
		filepath.Join(home, "Library", "Containers", "com.tinyspeck.slackmacgap", "Data", "Library", "Application Support", "Slack"),
	} {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("Slack Desktop profile not found; sign in to Slack Desktop first")
}

// LoginDesktop imports the active Slack Desktop account. No browser is opened.
func LoginDesktop(ctx context.Context) (Credentials, error) {
	dir, err := slackDesktopDir()
	if err != nil {
		return Credentials{}, err
	}
	cookie, err := desktopCookie(dir)
	if err != nil {
		return Credentials{}, err
	}
	tokens, err := desktopTokens(dir)
	if err != nil {
		return Credentials{}, err
	}
	ids := make([]string, 0, len(tokens))
	for id := range tokens {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var last error
	for _, id := range ids {
		c, err := validate(ctx, Credentials{Token: tokens[id], Cookie: cookie, TeamID: id})
		if err == nil {
			return c, nil
		}
		last = err
	}
	return Credentials{}, fmt.Errorf("no valid Slack Desktop workspace: %w", last)
}

func desktopCookie(dir string) (string, error) {
	path := filepath.Join(dir, "Cookies")
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(dir, "Network", "Cookies")
	}
	tmp, err := os.MkdirTemp("", "sltty-desktop-cookie-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	for _, suffix := range []string{"", "-wal"} {
		b, e := os.ReadFile(path + suffix)
		if e != nil {
			if suffix == "-wal" && os.IsNotExist(e) {
				continue
			}
			return "", fmt.Errorf("reading Slack cookie database: %w", e)
		}
		if e = os.WriteFile(filepath.Join(tmp, "Cookies")+suffix, b, 0600); e != nil {
			return "", e
		}
	}
	out, err := exec.Command(sqliteBin, "-noheader", filepath.Join(tmp, "Cookies"), `SELECT value || '|' || hex(encrypted_value) FROM cookies WHERE host_key='.slack.com' AND name='d' LIMIT 1`).Output()
	if err != nil {
		return "", fmt.Errorf("reading Slack Desktop cookies requires sqlite3: %w", err)
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("Slack Desktop has no signed-in session cookie")
	}
	if parts[0] != "" {
		return parts[0], nil
	}
	enc, err := hex.DecodeString(parts[1])
	if err != nil || len(enc) < 4 || string(enc[:3]) != "v10" {
		return "", fmt.Errorf("unsupported Slack Desktop cookie encryption")
	}
	pw, err := keychainPassword()
	if err != nil {
		return "", fmt.Errorf("reading Slack Safe Storage from Keychain: %w", err)
	}
	key := pbkdf2.Key(bytes.TrimSpace(pw), []byte("saltysalt"), 1003, 16, sha1.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	ct := enc[3:]
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid Slack Desktop cookie")
	}
	plain := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, bytes.Repeat([]byte{' '}, aes.BlockSize)).CryptBlocks(plain, ct)
	pad := int(plain[len(plain)-1])
	if pad < 1 || pad > aes.BlockSize {
		return "", fmt.Errorf("invalid Slack Desktop cookie padding")
	}
	plain = plain[:len(plain)-pad]
	// Chromium cookie DB v24 prefixes the value with a 32-byte SHA-256 of the
	// host; older versions do not. Strip it whenever the value does not start
	// like a Slack cookie.
	if !bytes.HasPrefix(plain, []byte("xoxd-")) && len(plain) >= 32 {
		plain = plain[32:]
	}
	if !strings.HasPrefix(string(plain), "xoxd-") {
		return "", fmt.Errorf("could not decrypt Slack Desktop cookie")
	}
	return string(plain), nil
}

func desktopTokens(dir string) (map[string]string, error) {
	src := filepath.Join(dir, "Local Storage", "leveldb")
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, fmt.Errorf("Slack Desktop local storage unavailable: %w", err)
	}
	tmp, err := os.MkdirTemp("", "sltty-desktop-storage-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, er := os.ReadFile(filepath.Join(src, e.Name()))
		if er != nil {
			return nil, er
		}
		if er = os.WriteFile(filepath.Join(tmp, e.Name()), b, 0600); er != nil {
			return nil, er
		}
	}
	db, err := leveldb.OpenFile(tmp, &opt.Options{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer db.Close()
	out := map[string]string{}
	it := db.NewIterator(nil, nil)
	defer it.Release()
	for it.Next() {
		if !strings.HasSuffix(string(it.Key()), "localConfig_v2") {
			continue
		}
		v := it.Value()
		if len(v) > 0 && v[0] == 1 {
			v = v[1:]
		}
		var cfg struct {
			Teams map[string]struct {
				Token string `json:"token"`
			} `json:"teams"`
		}
		if json.Unmarshal(v, &cfg) != nil {
			continue
		}
		for id, t := range cfg.Teams {
			if strings.HasPrefix(t.Token, "xoxc-") {
				out[id] = t.Token
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("Slack Desktop has no workspace tokens")
	}
	return out, it.Error()
}
