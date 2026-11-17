// Command sltty is a keyboard-first chat client for the terminal: Slack and
// Telegram, each account in a tab. It uses demo data until a session is
// saved with `sltty login` (Slack) or `sltty login telegram`.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/config"
	"github.com/oscarmlage/sltty/internal/diag"
	"github.com/oscarmlage/sltty/internal/tui"
	"github.com/oscarmlage/sltty/internal/uistate"
	"golang.org/x/term"
)

// version is set by the Makefile (git describe).
var version = "dev"

// What run needs from the outside, replaced by the tests.
var (
	runTUI         = tui.RunAccounts
	loginBrowser   = chat.Login
	loginDesktop   = chat.LoginDesktop
	newSlack       = func(ctx context.Context, c chat.Credentials) (chat.Backend, error) { return chat.NewSlack(ctx, c) }
	loginTelegram  = chat.TelegramLogin
	logoutTelegram = chat.TelegramLogoutRemote
	hasTelegram    = chat.HasTelegramSession
	newTelegram    = func(ctx context.Context) (chat.Backend, error) { return chat.NewTelegram(ctx) }
)

// secret reads a password without echo when stdin is a terminal.
func secret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	defer fmt.Fprintln(os.Stderr)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		return string(b), err
	}
	s, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return s[:max(0, len(s)-1)], err
}

// service splits an optional "slack" or "telegram" off a sub-command's
// arguments; Slack when none is given.
func service(args []string) (string, []string) {
	if len(args) > 0 && (args[0] == "slack" || args[0] == "telegram") {
		return args[0], args[1:]
	}
	return "slack", args
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "sltty:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if err := diag.Init(); err != nil {
		return fmt.Errorf("initializing log: %w", err)
	}
	fs := flag.NewFlagSet("sltty", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("config", config.Path(), "config file")
	demo := fs.Bool("demo", false, "use the built-in demo data, even when sessions exist")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: sltty [-config file]\n"+
			"       sltty keys    print every key binding as a config file\n"+
			"       sltty config  list what the config file takes\n"+
			"       sltty login [slack] [--browser|--desktop]  sign in to Slack through Chrome or Slack Desktop\n"+
			"       sltty login telegram [--phone]  sign in to Telegram: scan a QR code (or phone number and code)\n"+
			"       sltty logout [slack|telegram]  end that session and remove it\n"+
			"       sltty -demo   use the built-in demo data\n\n"+
			"Key bindings are read from %s (\"keys\": action → keys).\n\n", config.Path())
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	switch fs.Arg(0) {
	case "login":
		svc, rest := service(fs.Args()[1:])
		if svc == "telegram" {
			return telegramLogin(rest, stdout, stderr)
		}
		loginFlags := flag.NewFlagSet("login", flag.ContinueOnError)
		loginFlags.SetOutput(stderr)
		browser := loginFlags.Bool("browser", false, "sign in in an isolated Chrome profile (default)")
		desktop := loginFlags.Bool("desktop", false, "import the active Slack Desktop session")
		if err := loginFlags.Parse(rest); err != nil || loginFlags.NArg() != 0 || *browser && *desktop {
			return fmt.Errorf("usage: sltty login [slack] [--browser|--desktop]")
		}
		var creds chat.Credentials
		var err error
		if *desktop {
			fmt.Fprintln(stderr, "Reading the active Slack Desktop session…")
			creds, err = loginDesktop(context.Background())
		} else {
			creds, err = loginBrowser(context.Background(), func(s string) { fmt.Fprintln(stderr, s) })
		}
		if err != nil {
			return err
		}
		if err := chat.SaveSession(creds); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "signed in to %s; session saved at %s\n", creds.TeamName, chat.SessionPath())
		return nil
	case "logout":
		svc, rest := service(fs.Args()[1:])
		if len(rest) != 0 {
			return fmt.Errorf("usage: sltty logout [slack|telegram]")
		}
		if svc == "telegram" {
			return logoutTelegram(context.Background())
		}
		return chat.Logout()
	case "keys":
		fmt.Fprintln(stdout, tui.DefaultsJSON())
		return nil
	case "config":
		fmt.Fprintf(stdout, "%s takes (all optional):\n\n", *cfgPath)
		for _, f := range config.Reference {
			def := ""
			if f.Default != "" {
				def = ", default " + f.Default
			}
			fmt.Fprintf(stdout, "  %-16s %s (%s%s)\n", f.Key, f.Help, f.Type, def)
		}
		return nil
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return fmt.Errorf("unknown command %q", fs.Arg(0))
	}
	cfg, found, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	var sections []tui.Section
	for _, s := range cfg.Sections {
		sections = append(sections, tui.Section{Name: s.Name, Channels: s.Channels, Collapsed: s.Collapsed})
	}
	accounts, err := connect(*demo, cfg.Accounts, sections, stderr)
	if err != nil {
		return err
	}
	return runTUI(accounts, tui.Options{
		Keys: cfg.Keys, Theme: cfg.Theme, Layout: cfg.Layout, Clock: cfg.Clock, Themes: cfg.Themes, Sections: sections, PrivateIcon: cfg.PrivateIcon,
		Version: version, Images: cfg.Images, CustomEmoji: cfg.CustomEmoji == nil || *cfg.CustomEmoji, ImageWidth: cfg.ImageWidth, ImageHeight: cfg.ImageHeight,
		ComposerHeight: cfg.ComposerHeight, SidebarWidth: cfg.SidebarWidth, HistoryCacheTTL: time.Duration(cfg.HistoryCacheTTL) * time.Minute,
		PollInterval: time.Duration(cfg.PollSeconds) * time.Second,
		SettleDelay:  200 * time.Millisecond,
		ConfigPath:   *cfgPath, ConfigFound: found, LogPath: diag.Path(), StatePath: uistate.Path(),
	})
}

// connect opens the accounts to show, in order: the services of the
// config's "accounts" (all with a saved session when it has none), or the
// demo when there is none. Config sections only apply to Slack: Telegram
// brings its own folders.
func connect(demo bool, wanted []string, sections []tui.Section, stderr io.Writer) ([]tui.Account, error) {
	fake := []tui.Account{{Backend: chat.NewFake(), Sections: sections}}
	if demo {
		return fake, nil
	}
	if len(wanted) == 0 {
		wanted = []string{"slack", "telegram"}
	}
	type result struct {
		acct tui.Account
		ok   bool
		err  error
	}
	results := make([]result, len(wanted))
	done := make(chan struct{}, len(wanted))
	for i, svc := range wanted {
		go func() {
			defer func() { done <- struct{}{} }()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			switch svc {
			case "slack":
				creds, err := chat.LoadSession()
				if os.IsNotExist(err) {
					return
				}
				if err != nil {
					results[i].err = fmt.Errorf("reading Slack session: %w", err)
					return
				}
				b, err := newSlack(ctx, creds)
				if err != nil {
					results[i].err = fmt.Errorf("saved Slack session is no longer valid; run `sltty login`: %w", err)
					return
				}
				results[i] = result{acct: tui.Account{Backend: b, Sections: sections}, ok: true}
			case "telegram":
				if !hasTelegram() {
					return
				}
				b, err := newTelegram(ctx)
				if err != nil {
					results[i].err = fmt.Errorf("could not connect to Telegram (run `sltty login telegram` if the session ended): %w", err)
					return
				}
				results[i] = result{acct: tui.Account{Backend: b}, ok: true}
			default:
				results[i].err = fmt.Errorf("accounts: unknown service %q (slack or telegram)", svc)
			}
		}()
	}
	for range wanted {
		<-done
	}
	var out []tui.Account
	var errs []error
	for _, r := range results {
		if r.ok {
			out = append(out, r.acct)
		} else if r.err != nil {
			errs = append(errs, r.err)
		}
	}
	if len(out) == 0 {
		if len(errs) > 0 {
			return nil, errors.Join(errs...)
		}
		return fake, nil
	}
	// One account failing does not keep the others closed.
	for _, err := range errs {
		fmt.Fprintln(stderr, "sltty:", err)
		diag.Printf("%v", err)
	}
	return out, nil
}

func telegramLogin(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("login telegram", flag.ContinueOnError)
	fs.SetOutput(stderr)
	phone := fs.Bool("phone", false, "sign in with the phone number and a code instead of a QR code")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return fmt.Errorf("usage: sltty login telegram [--phone]")
	}
	how := chat.LoginQR
	if *phone {
		how = chat.LoginPhone
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	name, err := loginTelegram(ctx, chat.Prompter{In: bufio.NewReader(os.Stdin), Out: stderr, Secret: secret}, how)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	fmt.Fprintf(stdout, "signed in to Telegram as %s; session saved at %s\n", name, chat.TelegramSessionPath())
	return nil
}
