// readscope: terminal reading scope — RSS feeds, Hacker News and your
// local EPUB library, all in one terminal.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/doctor"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/store"
	"github.com/smy-101/readscope/internal/theme"
	"github.com/smy-101/readscope/internal/ui"

	_ "github.com/smy-101/readscope/internal/source/hn" // register the hn source
)

// Version is injected at build time (-ldflags "-X main.Version=…").
var Version = "dev"

const usage = `readscope — terminal reading scope (RSS · Hacker News · EPUB)

Usage:
  readscope            Start the TUI (default)
  readscope doctor     Environment self-check
  readscope rss refresh  Fetch RSS/Atom feeds from sources.toml into the offline cache
  readscope rss import F  Merge OPML subscriptions into sources.toml
  readscope rss export F  Write subscriptions as OPML for other readers
  readscope render     Render a local HTML file or URL to terminal text
  readscope zhihu probe  Probe zhihu API endpoints and save raw samples (M4 tracer)
  readscope zhihu login  Paste a browser Cookie header (zhihu login ceremony)
  readscope version    Print version

Environment:
  READSCOPE_CONFIG     Config directory override (default: OS config dir/readscope)
  READSCOPE_STATE      State directory override (default: ~/.local/state/readscope)
`

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "", "tui":
		if err := runTUI(); err != nil {
			fmt.Fprintln(os.Stderr, "readscope:", err)
			os.Exit(1)
		}
	case "doctor":
		cfg, err := core.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
			os.Exit(1)
		}
		if !doctor.Run(os.Stdout, cfg, Version) {
			os.Exit(1)
		}
	case "version", "--version", "-v":
		fmt.Println("readscope", Version)
	case "render":
		os.Exit(runRender(args[1:]))
	case "rss":
		os.Exit(runRss(args[1:]))
	case "zhihu":
		os.Exit(runZhihu(args[1:]))
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "readscope: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

func runTUI() error {
	cfg, err := core.Load()
	if err != nil {
		return err
	}
	cat, err := i18n.Load(cfg.Language)
	if err != nil {
		return fmt.Errorf("%s", cat.Get("errors.i18n_load", err))
	}
	th, err := theme.Load(cfg.Theme)
	if err != nil {
		return err
	}
	// The database is opened up-front so a broken state dir fails loudly
	// before the alt screen takes over.
	dbPath, err := store.DefaultPath()
	if err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("%s", cat.Get("errors.db_open", err))
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		return fmt.Errorf("%s", cat.Get("errors.db_migrate", err))
	}
	srcs, err := core.LoadSources()
	if err != nil {
		return err
	}

	_, err = tea.NewProgram(ui.New(cfg, cat, th, st, srcs), tea.WithAltScreen()).Run()
	if err != nil {
		if strings.Contains(err.Error(), "TTY") {
			// Headless / piped stdin: surface a human explanation, not the
			// raw ioctl error.
			return fmt.Errorf("%s", cat.Get("errors.no_tty"))
		}
		return err
	}
	return nil
}
