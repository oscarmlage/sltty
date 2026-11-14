//go:build !darwin

package chat

import (
	"context"
	"fmt"
)

// LoginDesktop currently supports macOS, where the Slack Safe Storage key is
// available through the system Keychain.
func LoginDesktop(context.Context) (Credentials, error) {
	return Credentials{}, fmt.Errorf("Slack Desktop login is currently supported on macOS only; use `sltty login --browser`")
}
