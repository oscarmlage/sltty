package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

// SessionPath is where the Slack session is kept (owner-only).
func SessionPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "sltty", "slack-session.json")
}

// LoadSession reads the saved session. A file readable by others (it is as
// good as a password) is tightened to owner-only first.
func LoadSession() (Credentials, error) {
	p := SessionPath()
	if st, err := os.Stat(p); err == nil && st.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(p, 0o600); err != nil {
			return Credentials{}, err
		}
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Credentials{}, err
	}
	var c Credentials
	err = json.Unmarshal(b, &c)
	return c, err
}

// SaveSession writes the session with owner-only permissions.
func SaveSession(c Credentials) error {
	p := SessionPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// Logout removes the saved session.
func Logout() error {
	if err := os.Remove(SessionPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// validate checks credentials against Slack and fills in the workspace.
func validate(ctx context.Context, c Credentials) (Credentials, error) {
	s, err := NewSlack(ctx, c)
	if err != nil {
		return Credentials{}, err
	}
	c.TeamID, c.TeamName = s.creds.TeamID, s.creds.TeamName
	return c, nil
}
