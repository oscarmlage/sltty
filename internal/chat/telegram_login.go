package chat

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"
	"rsc.io/qr"
)

// Prompter asks the user for what signing in needs. Secret is for
// passwords: it must not echo.
type Prompter struct {
	In     *bufio.Reader
	Out    io.Writer
	Secret func(prompt string) (string, error)
}

func (p Prompter) ask(prompt string) (string, error) {
	fmt.Fprint(p.Out, prompt)
	s, err := p.In.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

// LoginMethod is how Login signs in.
type LoginMethod int

const (
	// LoginQR shows a QR code to scan with Telegram on the phone
	// (Settings → Devices → Link Desktop Device), as Telegram Desktop does.
	LoginQR LoginMethod = iota
	// LoginPhone asks for the phone number and the code Telegram sends.
	LoginPhone
)

// Login signs in and returns the account's name. The two-step
// verification password is asked when the account has one. Without
// built-in application credentials it asks for an api_id/api_hash first.
func TelegramLogin(ctx context.Context, p Prompter, how LoginMethod) (string, error) {
	app, err := LoadApp()
	if err != nil {
		if app, err = askApp(p); err != nil {
			return "", err
		}
	}
	had := HasTelegramSession()
	d := tg.NewUpdateDispatcher()
	loggedIn := qrlogin.OnLoginToken(d)
	client := newClient(app, d)
	var name string
	stop, err := start(ctx, client, func(ctx context.Context) error {
		st, err := client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !st.Authorized {
			switch how {
			case LoginPhone:
				err = client.Auth().IfNecessary(ctx, auth.NewFlow(termAuth{p}, auth.SendCodeOptions{}))
			default:
				qr := qrlogin.NewQR(client.API(), app.ID, app.Hash, qrlogin.Options{
					Migrate:  client.MigrateTo,
					Password: func(ctx context.Context) (string, error) { return p.Secret("Two-step verification password: ") },
				})
				_, err = qr.Auth(ctx, loggedIn, func(_ context.Context, tok qrlogin.Token) error {
					return showQR(p.Out, tok.URL())
				})
			}
			if err != nil {
				return err
			}
		}
		self, err := client.Self(ctx)
		if err != nil {
			return err
		}
		name = userName(self)
		return nil
	})
	if err != nil {
		if !had { // a failed first login leaves no half-made session
			_ = TelegramLogout()
		}
		return "", err
	}
	stop()
	return name, nil
}

// askApp is for builds without application credentials.
func askApp(p Prompter) (App, error) {
	fmt.Fprintln(p.Out, "This build has no Telegram API id: create one at https://my.telegram.org → API development tools.")
	id, err := p.ask("api_id: ")
	if err != nil {
		return App{}, err
	}
	n, err := strconv.Atoi(id)
	if err != nil || n <= 0 {
		return App{}, fmt.Errorf("api_id must be a number")
	}
	hash, err := p.ask("api_hash: ")
	if err != nil {
		return App{}, err
	}
	if hash == "" {
		return App{}, fmt.Errorf("api_hash is empty")
	}
	app := App{ID: n, Hash: hash}
	return app, SaveApp(app)
}

// showQR draws the login code with half blocks, dark on light so phones
// read it on any terminal theme. It is redrawn when the code expires.
func showQR(w io.Writer, url string) error {
	code, err := qr.Encode(url, qr.L)
	if err != nil {
		return err
	}
	const quiet = 2
	n := code.Size + 2*quiet
	black := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
	}
	var b strings.Builder
	b.WriteString("\nScan this with Telegram on your phone: Settings → Devices → Link Desktop Device\n\n")
	for y := 0; y < n; y += 2 {
		b.WriteString("  \x1b[30;107m")
		for x := 0; x < n; x++ {
			top, bottom := black(x, y), y+1 < n && black(x, y+1)
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	b.WriteString("\nWaiting… (the code refreshes by itself; ctrl+c to cancel, `sltty login telegram --phone` to use a code instead)\n")
	_, err = io.WriteString(w, b.String())
	return err
}

// TelegramLogoutRemote ends the session on Telegram's side too, then removes it.
func TelegramLogoutRemote(ctx context.Context) error {
	app, err := LoadApp()
	if err != nil || !HasTelegramSession() {
		return TelegramLogout()
	}
	client := newClient(app, nil)
	stop, err := start(ctx, client, func(ctx context.Context) error {
		_, err := client.API().AuthLogOut(ctx)
		return err
	})
	if err == nil {
		stop()
	}
	return errors.Join(err, TelegramLogout())
}

type termAuth struct{ p Prompter }

func (a termAuth) Phone(context.Context) (string, error) {
	return a.p.ask("Phone number (international format, +34…): ")
}

func (a termAuth) Code(_ context.Context, sent *tg.AuthSentCode) (string, error) {
	where := "Telegram"
	switch sent.Type.(type) {
	case *tg.AuthSentCodeTypeSMS:
		where = "SMS"
	case *tg.AuthSentCodeTypeApp:
		where = "your other Telegram apps"
	}
	return a.p.ask("Code (sent to " + where + "): ")
}

func (a termAuth) Password(context.Context) (string, error) {
	return a.p.Secret("Two-step verification password: ")
}

func (a termAuth) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return errors.New("this phone number has no Telegram account; sign up in a Telegram app first")
}

func (a termAuth) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("sign up in a Telegram app first")
}
