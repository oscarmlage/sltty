// Command sltty is a keyboard-first chat client for the terminal.
// It uses demo data by default and can connect through a saved Slack session.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/oscarmlage/sltty/internal/chat"
	"github.com/oscarmlage/sltty/internal/config"
	"github.com/oscarmlage/sltty/internal/diag"
	"github.com/oscarmlage/sltty/internal/tui"
	"github.com/oscarmlage/sltty/internal/uistate"
)

// version is set by the Makefile (git describe).
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sltty:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := diag.Init(); err != nil {
		return fmt.Errorf("initializing log: %w", err)
	}
	cfgPath := flag.String("config", config.Path(), "config file")
	demo := flag.Bool("demo", false, "use the built-in demo data, even when a Slack session exists")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: sltty [-config file]\n"+
			"       sltty keys    print every key binding as a config file\n"+
			"       sltty config  list what the config file takes\n"+
			"       sltty login [--browser|--desktop]  sign in through Chrome or Slack Desktop\n"+
			"       sltty logout  remove the saved Slack session\n"+
			"       sltty -demo   use the built-in demo data\n\n"+
			"Key bindings are read from %s (\"keys\": action → keys).\n\n", config.Path())
		flag.PrintDefaults()
	}
	flag.Parse()
	switch flag.Arg(0) {
	case "login":
		loginFlags := flag.NewFlagSet("login", flag.ContinueOnError)
		loginFlags.SetOutput(os.Stderr)
		browser := loginFlags.Bool("browser", false, "sign in in an isolated Chrome profile (default)")
		desktop := loginFlags.Bool("desktop", false, "import the active Slack Desktop session")
		if err := loginFlags.Parse(flag.Args()[1:]); err != nil || loginFlags.NArg() != 0 || *browser && *desktop {
			return fmt.Errorf("usage: sltty login [--browser|--desktop]")
		}
		var creds chat.Credentials
		var err error
		if *desktop {
			fmt.Fprintln(os.Stderr, "Reading the active Slack Desktop session…")
			creds, err = chat.LoginDesktop(context.Background())
		} else {
			creds, err = chat.Login(context.Background(), func(s string) { fmt.Fprintln(os.Stderr, s) })
		}
		if err != nil {
			return err
		}
		if err := chat.SaveSession(creds); err != nil {
			return err
		}
		fmt.Printf("signed in to %s; session saved at %s\n", creds.TeamName, chat.SessionPath())
		return nil
	case "logout":
		if flag.NArg() != 1 {
			return fmt.Errorf("usage: sltty logout")
		}
		return chat.Logout()
	case "keys":
		fmt.Println(tui.DefaultsJSON())
		return nil
	case "config":
		fmt.Printf("%s takes (all optional):\n\n", *cfgPath)
		for _, f := range config.Reference {
			def := ""
			if f.Default != "" {
				def = ", default " + f.Default
			}
			fmt.Printf("  %-16s %s (%s%s)\n", f.Key, f.Help, f.Type, def)
		}
		return nil
	}
	if flag.NArg() > 0 {
		flag.Usage()
		return fmt.Errorf("unknown command %q", flag.Arg(0))
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
			live, err := chat.NewSlack(context.Background(), creds)
			if err != nil {
				return fmt.Errorf("saved Slack session is no longer valid; run `sltty login`: %w", err)
			}
			backend = live
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("reading Slack session: %w", err)
		}
	}
	return tui.Run(backend, tui.Options{
		Keys: cfg.Keys, Theme: cfg.Theme, Layout: cfg.Layout, Clock: cfg.Clock, Themes: cfg.Themes, Sections: sections, PrivateIcon: cfg.PrivateIcon,
		Version: version, Images: cfg.Images, CustomEmoji: cfg.CustomEmoji == nil || *cfg.CustomEmoji, ImageWidth: cfg.ImageWidth, ImageHeight: cfg.ImageHeight,
		ComposerHeight: cfg.ComposerHeight, SidebarWidth: cfg.SidebarWidth, ReadOnEnter: cfg.ReadOnEnter,
		ConfigPath: *cfgPath, ConfigFound: found, LogPath: diag.Path(), StatePath: uistate.Path(),
	})
}
