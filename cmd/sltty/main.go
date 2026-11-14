// Command sltty is a keyboard-first chat client for the terminal.
// It uses demo data by default and can connect through a saved Slack session.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/config"
	"github.com/oscarmlage/sltty/internal/diag"
	"github.com/oscarmlage/sltty/internal/tui"
	"github.com/oscarmlage/sltty/internal/uistate"
)

// version is set by the Makefile (git describe).
var version = "dev"

// What run needs from the outside, replaced by the tests.
var (
	runTUI       = tui.Run
	loginBrowser = chat.Login
	loginDesktop = chat.LoginDesktop
	newSlack     = func(ctx context.Context, c chat.Credentials) (chat.Backend, error) { return chat.NewSlack(ctx, c) }
)

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
	demo := fs.Bool("demo", false, "use the built-in demo data, even when a Slack session exists")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: sltty [-config file]\n"+
			"       sltty keys    print every key binding as a config file\n"+
			"       sltty config  list what the config file takes\n"+
			"       sltty login [--browser|--desktop]  sign in through Chrome or Slack Desktop\n"+
			"       sltty logout  remove the saved Slack session\n"+
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
		loginFlags := flag.NewFlagSet("login", flag.ContinueOnError)
		loginFlags.SetOutput(stderr)
		browser := loginFlags.Bool("browser", false, "sign in in an isolated Chrome profile (default)")
		desktop := loginFlags.Bool("desktop", false, "import the active Slack Desktop session")
		if err := loginFlags.Parse(fs.Args()[1:]); err != nil || loginFlags.NArg() != 0 || *browser && *desktop {
			return fmt.Errorf("usage: sltty login [--browser|--desktop]")
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
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: sltty logout")
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
	backend := chat.Backend(chat.NewFake())
	if !*demo {
		if creds, err := chat.LoadSession(); err == nil {
			live, err := newSlack(context.Background(), creds)
			if err != nil {
				return fmt.Errorf("saved Slack session is no longer valid; run `sltty login`: %w", err)
			}
			backend = live
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("reading Slack session: %w", err)
		}
	}
	return runTUI(backend, tui.Options{
		Keys: cfg.Keys, Theme: cfg.Theme, Layout: cfg.Layout, Clock: cfg.Clock, Themes: cfg.Themes, Sections: sections, PrivateIcon: cfg.PrivateIcon,
		Version: version, Images: cfg.Images, CustomEmoji: cfg.CustomEmoji == nil || *cfg.CustomEmoji, ImageWidth: cfg.ImageWidth, ImageHeight: cfg.ImageHeight,
		ComposerHeight: cfg.ComposerHeight, SidebarWidth: cfg.SidebarWidth,
		ConfigPath: *cfgPath, ConfigFound: found, LogPath: diag.Path(), StatePath: uistate.Path(),
	})
}
