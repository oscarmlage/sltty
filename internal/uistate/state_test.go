package uistate

import (
	"os"
	"testing"
)

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

func TestLoadMissingIsEmpty(t *testing.T) {
	isolate(t)
	s, err := Load()
	if err != nil || s.Workspaces == nil || len(s.Workspaces) != 0 {
		t.Fatalf("Load = %+v, %v", s, err)
	}
}

func TestSaveLoadRoundTripAndPermissions(t *testing.T) {
	isolate(t)
	on := true
	in := State{Workspaces: map[string]Workspace{"T1": {Theme: "nord", ImagesOn: &on, SidebarWidth: 30, Collapsed: map[string]bool{"Channels": true}, Conversation: "C1"}}}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	w := out.Workspaces["T1"]
	if w.Theme != "nord" || w.ImagesOn == nil || !*w.ImagesOn || w.SidebarWidth != 30 || !w.Collapsed["Channels"] || w.Conversation != "C1" {
		t.Fatalf("round trip: %+v", w)
	}
	if st, err := os.Stat(Path()); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode: %v %v", st, err)
	}
	if _, err := os.Stat(Path() + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temporary file left behind")
	}
}

func TestSaveNilWorkspaces(t *testing.T) {
	isolate(t)
	if err := Save(State{}); err != nil {
		t.Fatal(err)
	}
	if s, err := Load(); err != nil || s.Workspaces == nil {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestLoadCorruptIsAnError(t *testing.T) {
	isolate(t)
	if err := os.MkdirAll(Path()[:len(Path())-len("/state.json")], 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("corrupt state must be an error")
	}
}
