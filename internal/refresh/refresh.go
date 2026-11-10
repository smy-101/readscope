// Package refresh orchestrates a refresh cycle for one source: every
// feed is fetched, snapshotted into the store, and reported (new /
// total counts). A feed that fails — network, parse, store — never
// stops the others; its error is reported in place. This is M2's
// tracer bullet: config → fetch → SQLite, fully testable without the
// TUI.
package refresh

import (
	"context"
	"fmt"
	"io"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/source"
	"github.com/smy-101/readscope/internal/store"
)

// Result is one feed's refresh outcome; Err != nil means the feed was
// skipped (fetch or save failed) and New/Total carry no snapshot info.
type Result struct {
	Source string
	Feed   string // key
	Label  string
	New    int
	Total  int
	Err    error
}

// Source refreshes every feed of the named source in feed-key order
// and returns one Result per feed. With no feeds configured it prints
// the no-feeds hint naming sources.toml and returns nil.
func Source(ctx context.Context, st *store.Store, srcs *core.Sources, sourceID string) ([]Result, error) {
	src, err := source.New(sourceID, srcs)
	if err != nil {
		return nil, err
	}
	feeds := src.Feeds()
	if len(feeds) == 0 {
		return nil, nil
	}
	results := make([]Result, 0, len(feeds))
	for _, f := range feeds {
		res := Result{Source: sourceID, Feed: f.Key, Label: f.Label}
		items, err := src.Fetch(ctx, f)
		if err != nil {
			res.Err = err
			results = append(results, res)
			continue
		}
		newCount, total, err := st.SaveFeedItems(ctx, sourceID, f.Key, items)
		if err != nil {
			res.Err = fmt.Errorf("store %s: %w", f.Key, err)
			results = append(results, res)
			continue
		}
		res.New, res.Total = newCount, total
		results = append(results, res)
	}
	return results, nil
}

// Report writes the per-feed lines and a summary to out, localized via
// cat: "阮一峰周刊: 3 new / 40 total", "label: error: …", then one
// summary line. It returns true when every feed succeeded.
func Report(out io.Writer, results []Result, cat *i18n.Catalog) bool {
	if len(results) == 0 {
		if path, err := core.SourcesPath(); err == nil {
			fmt.Fprintln(out, cat.Get("rss.no_feeds", path))
		}
		return true
	}
	ok := 0
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintln(out, cat.Get("rss.feed_error", r.Label, r.Err))
			continue
		}
		fmt.Fprintln(out, cat.Get("rss.feed_line", r.Label, r.New, r.Total))
		ok++
	}
	fmt.Fprintln(out, cat.Get("rss.summary", len(results), ok, len(results)-ok))
	return ok == len(results)
}
