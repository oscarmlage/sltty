package chat

import (
	"os"
	"testing"
)

func TestSessionFileIsOwnerOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if err := SaveSession(Credentials{Token: "xoxc-t", Cookie: "xoxd-c"}); err != nil {
		t.Fatal(err)
	}
	p := SessionPath()
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSession(); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", st.Mode().Perm())
	}
	if err := Logout(); err != nil {
		t.Fatal(err)
	}
}
