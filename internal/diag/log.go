// Package diag provides a small, secret-safe application log.
package diag

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// maxLog is the size past which the log starts over at launch.
const maxLog = 1 << 20

var (
	mu     sync.Mutex
	path   string
	logger = log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds)
)

func Init() error {
	mu.Lock()
	defer mu.Unlock()
	if path != "" {
		return nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(base, "sltty")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path = filepath.Join(dir, "sltty.log")
	flags := os.O_CREATE | os.O_APPEND | os.O_WRONLY
	if st, err := os.Stat(path); err == nil && st.Size() > maxLog {
		flags |= os.O_TRUNC // the log is a diagnostic aid, not an archive
	}
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return err
	}
	logger.SetOutput(f)
	logger.Printf("sltty started")
	return nil
}
func Path() string                      { mu.Lock(); defer mu.Unlock(); return path }
func Printf(format string, args ...any) { mu.Lock(); defer mu.Unlock(); logger.Printf(format, args...) }

// Tail returns the last n text lines. Secrets must never be logged.
func Tail(n int) ([]string, error) {
	p := Path()
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
