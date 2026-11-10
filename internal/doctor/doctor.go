// Package doctor is readscope's environment self-check: the same checks
// FeedScope's doctor runs (config/state locations, database, language
// packs, themes, terminal), phrased so a broken environment is diagnosable
// from one command.
package doctor

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/store"
	"github.com/smy-101/readscope/internal/theme"
)

type check struct {
	label string
	err   error
}

// Run executes all checks, prints a report to w, and reports whether
// everything passed (the CLI turns false into exit code 1).
func Run(w io.Writer, cfg *core.Config, version string) bool {
	cat := loadCatalog(cfg)
	th, _ := theme.Load(cfg.Theme)

	fmt.Fprintln(w, th.Accent.Render(cat.Get("doctor.title")))
	fmt.Fprintln(w, th.Dim.Render(fmt.Sprintf("%s: %s", cat.Get("doctor.version"), version)))
	fmt.Fprintln(w)

	checks := []check{
		run(cat, "doctor.config_dir", func() error { _, err := core.Path(); return err }),
		run(cat, "doctor.state_dir", func() error { _, err := store.DefaultPath(); return err }),
		run(cat, "doctor.db_open", func() error { return checkDB("db_open") }),
		run(cat, "doctor.db_migrate", func() error { return checkDB("db_migrate") }),
		run(cat, "doctor.i18n_load", checkI18n),
		run(cat, "doctor.theme_load", checkThemes),
	}

	okCount := 0
	for _, c := range checks {
		if c.err != nil {
			fmt.Fprintf(w, "%s %s: %v\n", th.Error.Render("✗"), c.label, c.err)
			continue
		}
		okCount++
		fmt.Fprintf(w, "%s %s\n", th.Success.Render("✓"), c.label)
	}

	// Terminal size: informational, never a failure (piped output is fine).
	fmt.Fprintln(w)
	if fd := int(os.Stdout.Fd()); term.IsTerminal(fd) {
		width, height, _ := term.GetSize(fd)
		fmt.Fprintf(w, "%s %s: %d×%d\n", th.Success.Render("✓"), cat.Get("doctor.terminal"), width, height)
	} else {
		fmt.Fprintf(w, "%s %s\n", th.Dim.Render("-"), cat.Get("doctor.terminal"))
	}
	fmt.Fprintln(w, th.Dim.Render(cat.Get("doctor.hint_env")))

	fmt.Fprintln(w)
	fmt.Fprintln(w, th.StatusAccent.Render(cat.Get("doctor.summary", len(checks), okCount, len(checks)-okCount)))
	return okCount == len(checks)
}

func loadCatalog(cfg *core.Config) *i18n.Catalog {
	cat, err := i18n.Load(cfg.Language)
	if err != nil {
		// Language packs are embedded; failing here means the binary is
		// broken. Fall back to English rather than crashing doctor.
		cat, _ = i18n.Load(i18n.Default)
	}
	return cat
}

func run(cat *i18n.Catalog, key string, fn func() error) check {
	return check{label: cat.Get(key), err: fn()}
}

func checkDB(mode string) error {
	path, err := store.DefaultPath()
	if err != nil {
		return err
	}
	s, err := store.Open(path)
	if err != nil {
		return err
	}
	defer s.Close()
	if mode == "db_migrate" {
		return s.Migrate()
	}
	return s.Ping()
}

func checkI18n() error {
	infos, err := i18n.Available()
	if err != nil {
		return err
	}
	for _, info := range infos {
		if _, err := i18n.Load(info.Code); err != nil {
			return err
		}
	}
	return nil
}

func checkThemes() error {
	for _, name := range theme.Available() {
		if _, err := theme.Load(name); err != nil {
			return err
		}
	}
	return nil
}
