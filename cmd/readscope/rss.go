package main

import (
	"context"
	"fmt"
	"os"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/opml"
	"github.com/smy-101/readscope/internal/refresh"
	"github.com/smy-101/readscope/internal/source/rss"
	"github.com/smy-101/readscope/internal/store"
)

const rssUsage = "usage: readscope rss refresh | import <file> | export <file>"

// runRss dispatches `readscope rss <subcommand>`: refresh pulls feeds
// into the cache (ticket 01), import/export interop with other readers
// via OPML (ticket 06).
func runRss(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, rssUsage)
		return 2
	}
	switch args[0] {
	case "refresh":
		return runRssRefresh(args[1:])
	case "import":
		return runRssImport(args[1:])
	case "export":
		return runRssExport(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "readscope: unknown rss subcommand %q\n%s\n", args[0], rssUsage)
		return 2
	}
}

// loadCatalog opens the user's configured language pack. i18n.Load
// already falls back to the default pack for an unknown code, so an
// error here means the config itself is broken — the caller decides
// whether that is fatal (refresh) or tolerable (import/export).
func loadCatalog() (*i18n.Catalog, error) {
	cfg, err := core.Load()
	if err != nil {
		return nil, err
	}
	return i18n.Load(cfg.Language)
}

// loadSources reads sources.toml, printing and returning nil on
// failure — the three rss subcommands share the same die-and-exit-1
// path for it.
func loadSources() *core.Sources {
	srcs, err := core.LoadSources()
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
		return nil
	}
	return srcs
}

// loadCatalogLenient opens the user's language pack, falling back to
// the default whenever anything is broken. The import/export
// maintenance commands use it: a typo in config.toml must not block
// fixing one's subscriptions. refresh stays strict — a broken config
// is part of what it reports.
func loadCatalogLenient() *i18n.Catalog {
	cat, err := loadCatalog()
	if err != nil {
		cat, _ = i18n.Load(i18n.Default)
	}
	return cat
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

	cat, err := loadCatalog()
	if err != nil {
		fmt.Fprintf(os.Stderr, "readscope: %v\n", err)
		return 1
	}

	srcs := loadSources()
	if srcs == nil {
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

// runRssImport implements `readscope rss import <file>`: read an OPML
// document, merge its feeds into sources.toml (existing subscriptions
// untouched), report added/skipped. Dirty-but-parseable documents are
// a success with warnings; unparseable ones exit 1 having written
// nothing.
func runRssImport(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, rssUsage)
		return 2
	}
	cat := loadCatalogLenient()

	raw, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "readscope:", cat.Get("errors.opml_read", args[0], err))
		return 1
	}
	doc, err := opml.Parse(raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "readscope:", cat.Get("errors.opml_parse", args[0], err))
		return 1
	}
	srcs := loadSources()
	if srcs == nil {
		return 1
	}
	oc := opml.Import(srcs, doc)
	if err := srcs.Save(); err != nil {
		path, _ := core.SourcesPath()
		fmt.Fprintln(os.Stderr, "readscope:", cat.Get("errors.sources_save", path, err))
		return 1
	}
	opml.Report(os.Stdout, oc, cat)
	return 0
}

// runRssExport implements `readscope rss export <file>`: write every
// subscription as a standard OPML 2.0 document other readers import.
func runRssExport(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, rssUsage)
		return 2
	}
	cat := loadCatalogLenient()

	srcs := loadSources()
	if srcs == nil {
		return 1
	}
	if err := os.WriteFile(args[0], opml.Encode(srcs.RSS.Feeds), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "readscope:", cat.Get("errors.opml_write", args[0], err))
		return 1
	}
	fmt.Println(cat.Get("rss.export_summary", len(srcs.RSS.Feeds), args[0]))
	return 0
}
