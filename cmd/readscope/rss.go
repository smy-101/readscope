package main

import (
	"context"
	"fmt"
	"os"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/refresh"
	"github.com/smy-101/readscope/internal/source/rss"
	"github.com/smy-101/readscope/internal/store"
)

const rssUsage = "usage: readscope rss refresh"

// runRss dispatches `readscope rss <subcommand>`. refresh is the M2
// tracer bullet; subscriptions land in ticket 06 (OPML import/export).
func runRss(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, rssUsage)
		return 2
	}
	switch args[0] {
	case "refresh":
		return runRssRefresh(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "readscope: unknown rss subcommand %q\n%s\n", args[0], rssUsage)
		return 2
	}
}

// runRssRefresh implements `readscope rss refresh`: pull every RSS
// feed in sources.toml into the offline cache, printing per-feed
// new/total. Exit 0 when every feed succeeded (including the no-feeds
// case), 1 when any feed failed or config/storage broke.
func runRssRefresh(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, rssUsage)
		return 2
	}

	cfg, err := core.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
		return 1
	}
	cat, err := i18n.Load(cfg.Language)
	if err != nil {
		cat, _ = i18n.Load(i18n.Default)
	}

	srcs, err := core.LoadSources()
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
		return 1
	}

	dbPath, err := store.DefaultPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
		return 1
	}
	st, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "readscope:", cat.Get("errors.db_open", err))
		return 1
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		fmt.Fprintln(os.Stderr, "readscope:", cat.Get("errors.db_migrate", err))
		return 1
	}

	results, err := refresh.Source(context.Background(), st, srcs, rss.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
		return 1
	}
	if !refresh.Report(os.Stdout, results, cat) {
		return 1
	}
	return 0
}
