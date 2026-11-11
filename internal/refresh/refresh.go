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
// Unchanged marks a 304 answer: the snapshot was already current, so
// nothing was parsed or written — New is 0 and Total comes from the
// cache.
type Result struct {
	Source    string
	Feed      string // key
	Label     string
	New       int
	Total     int
	Unchanged bool
	Err       error
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
		// Conditional path (ticket 05): replay stored validators; a 304
		// skips parse and item writes entirely. Sources without the
		// extension (HN in M3, until it grows one) fetch in full.
		if cond, ok := src.(source.ConditionalSource); ok {
			res.Err = refreshConditional(ctx, st, cond, f, &res)
			results = append(results, res)
			continue
		}
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

// refreshConditional runs one feed's conditional fetch cycle and fills
// res. A failed fetch or save returns the error with the stored state
// untouched — the next successful refresh keeps its conditional chance.
func refreshConditional(ctx context.Context, st *store.Store, src source.ConditionalSource, f core.Feed, res *Result) error {
	prev, _, err := st.LoadFetchState(ctx, src.ID(), f.Key)
	if err != nil {
		return fmt.Errorf("load state %s: %w", f.Key, err)
	}
	items, next, changed, err := src.FetchConditional(ctx, f, prev)
	if err != nil {
		return err
	}
	if !changed {
		// 304: the snapshot is already current. Report the cached total,
		// persist the bumped last_fetched (validators carried over).
		total, err := st.CountFeed(ctx, src.ID(), f.Key)
		if err != nil {
			return fmt.Errorf("count %s: %w", f.Key, err)
		}
		if err := st.SaveFetchState(ctx, src.ID(), f.Key, next); err != nil {
			return fmt.Errorf("save state %s: %w", f.Key, err)
		}
		res.Unchanged, res.Total = true, total
		return nil
	}
	newCount, total, err := st.SaveFeedItems(ctx, src.ID(), f.Key, items)
	if err != nil {
		return fmt.Errorf("store %s: %w", f.Key, err)
	}
	if err := st.SaveFetchState(ctx, src.ID(), f.Key, next); err != nil {
		return fmt.Errorf("save state %s: %w", f.Key, err)
	}
	res.New, res.Total = newCount, total
	return nil
}

// NoFeedsHint returns the localized "no feeds yet — add some to %s"
// line naming sources.toml, shared by Report and the TUI status bar.
func NoFeedsHint(cat *i18n.Catalog) string {
	if path, err := core.SourcesPath(); err == nil {
		return cat.Get("rss.no_feeds", path)
	}
	return cat.Get("rss.no_feeds", "sources.toml")
}

// Report writes the per-feed lines and a summary to out, localized via
// cat: "阮一峰周刊: 3 new / 40 total", "label: unchanged", "label:
// error: …", then one summary line. A 304 counts as success — it is
// the refresh working, not failing. It returns true when every feed
// succeeded.
func Report(out io.Writer, results []Result, cat *i18n.Catalog) bool {
	if len(results) == 0 {
		fmt.Fprintln(out, NoFeedsHint(cat))
		return true
	}
	ok := 0
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintln(out, cat.Get("rss.feed_error", r.Label, r.Err))
			continue
		}
		if r.Unchanged {
			fmt.Fprintln(out, cat.Get("rss.feed_unchanged", r.Label, r.Total))
		} else {
			fmt.Fprintln(out, cat.Get("rss.feed_line", r.Label, r.New, r.Total))
		}
		ok++
	}
	fmt.Fprintln(out, cat.Get("rss.summary", len(results), ok, len(results)-ok))
	return ok == len(results)
}
