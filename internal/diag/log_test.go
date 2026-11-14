package diag

import (
	"os"
	"strings"
	"testing"
)

func reset() { mu.Lock(); path = ""; mu.Unlock() }

func TestLogWritesAndTails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	reset()
	defer reset()
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	if err := Init(); err != nil { // second call is a no-op
		t.Fatal(err)
	}
	for i := range 5 {
		Printf("line %d", i)
	}
	lines, err := Tail(3)
	if err != nil || len(lines) != 3 || !strings.HasSuffix(lines[2], "line 4") {
		t.Fatalf("Tail = %q, %v", lines, err)
	}
	if st, err := os.Stat(Path()); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("log mode: %v %v", st, err)
	}
}

func TestLogStartsOverWhenHuge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	reset()
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	p := Path()
	if err := os.WriteFile(p, make([]byte, maxLog+1), 0o600); err != nil {
		t.Fatal(err)
	}
	reset()
	defer reset()
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Size() > 1024 {
		t.Fatalf("log not truncated: %d bytes", st.Size())
	}
}
