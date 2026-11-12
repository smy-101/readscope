// Package refresh orchestrates a refresh cycle for one source: every
// feed is fetched, snapshotted into the store, and reported (new /
// total counts). A feed that fails — network, parse, store — never
// stops the others; its error is reported in place. This is M2's
// tracer bullet: config → fetch → SQLite, fully testable without the
// TUI.
package refresh

import (
	"context"
	"errors"
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
		results = append(results, refreshOneFeed(ctx, st, src, f))
	}
	return results, nil
}

// findFeed resolves one feed key against a source's feed list —
// the shared lookup of the feed-level pull (Feed) and the page
// append (More), both of which need the core.Feed itself, not just
// its key.
func findFeed(src source.Source, feedKey string) (*core.Feed, bool) {
	feeds := src.Feeds()
	for i := range feeds {
		if feeds[i].Key == feedKey {
			return &feeds[i], true
		}
	}
	return nil, false
}

// Feed refreshes one feed of the named source — F5's feed-level pull
// (M3 ticket 05: re-fetch the current feed's first page instead of
// the whole source, so the other feeds are never dialed) — through
// the same per-feed path Source walks, and returns its single
// Result. A feed key the source doesn't list is an error naming it.
func Feed(ctx context.Context, st *store.Store, srcs *core.Sources, sourceID, feedKey string) ([]Result, error) {
	src, err := source.New(sourceID, srcs)
	if err != nil {
		return nil, err
	}
	f, ok := findFeed(src, feedKey)
	if !ok {
		return nil, fmt.Errorf("refresh %s: no feed %q", sourceID, feedKey)
	}
	return []Result{refreshOneFeed(ctx, st, src, *f)}, nil
}

// refreshOneFeed runs one feed's refresh cycle — fetch (conditional
// where the source can), snapshot, report. The error lands on the
// Result, never in a return: one bad feed must not stop the others.
// A successful cycle also floors a paged source's depth at 1 (see
// bumpPagingFloor). Cursor-paged feeds (zhihu's follow) fork to their
// own path before any of the below: their first page carries the
// next-page cursor the caller must persist alongside the snapshot,
// which neither the conditional nor the plain items-only shape can.
func refreshOneFeed(ctx context.Context, st *store.Store, src source.Source, f core.Feed) Result {
	if cps, ok := src.(source.CursorPagedSource); ok && cps.CursorPaged(f) {
		return refreshCursorFeed(ctx, st, cps, f)
	}
	res := Result{Source: src.ID(), Feed: f.Key, Label: f.Label}
	// Conditional path: replay stored validators; a 304 skips parse
	// and item writes entirely. Sources without the extension (hn,
	// unconditional by design) fetch in full.
	if cond, ok := src.(source.ConditionalSource); ok {
		if err := refreshConditional(ctx, st, cond, f, &res); err != nil {
			res.Err = err
			return res
		}
		bumpPagingFloor(ctx, st, src, f)
		return res
	}
	items, err := src.Fetch(ctx, f)
	if err != nil {
		res.Err = err
		return res
	}
	newCount, total, err := st.SaveFeedItems(ctx, src.ID(), f.Key, items)
	if err != nil {
		res.Err = fmt.Errorf("store %s: %w", f.Key, err)
		return res
	}
	res.New, res.Total = newCount, total
	bumpPagingFloor(ctx, st, src, f)
	return res
}

// bumpPagingFloor is the paged-source side effect of a successful
// refresh: the feed's first page is in the cache now (or confirmed
// current), so its paging depth is at least 1 — Ctrl+N walks on from
// page 1 instead of re-fetching page 0. Unpaged sources (rss) are
// untouched; a depth write that fails is deliberately silent — the
// depth is advisory, and the worst case is one redundant page fetch.
func bumpPagingFloor(ctx context.Context, st *store.Store, src source.Source, f core.Feed) {
	if _, ok := src.(source.PagedSource); !ok {
		return
	}
	depth, err := st.LoadPagesFetched(ctx, src.ID(), f.Key)
	if err != nil || depth >= 1 {
		return
	}
	_ = st.SavePagesFetched(ctx, src.ID(), f.Key, 1)
}

// refreshCursorFeed is the F5 path of a cursor-paged feed: the first
// page in, and the walk re-anchored to it. The stored cursor becomes
// the fresh first page's paging.next whenever the server serves one
// — the walk continues right after the page on screen (freshness
// first: deep-kept cursors would resume a stale window), and the
// re-anchor self-heals a cursor the server expired under us. A served
// next of "" (the stream's end) keeps the old position, never
// regressing to "re-fetch page 1 forever". If 07's real-net smoke
// shows cursors are long-lived and deep reading matters more than
// freshness, flipping this write to a floor (only-if-empty) is the
// one-line rollback.
func refreshCursorFeed(ctx context.Context, st *store.Store, cps source.CursorPagedSource, f core.Feed) Result {
	res := Result{Source: cps.ID(), Feed: f.Key, Label: f.Label}
	items, next, err := cps.FetchCursorPage(ctx, f, "")
	if err != nil {
		res.Err = err
		return res
	}
	newCount, total, err := st.SaveFeedItems(ctx, cps.ID(), f.Key, items)
	if err != nil {
		res.Err = fmt.Errorf("store %s: %w", f.Key, err)
		return res
	}
	if next != "" {
		if err := st.SaveCursor(ctx, cps.ID(), f.Key, next); err != nil {
			res.Err = fmt.Errorf("save cursor %s: %w", f.Key, err)
			return res
		}
	}
	res.New, res.Total = newCount, total
	return res
}

// ErrNotPaged reports a Ctrl+N against a source that has no pages
// behind its first (rss: the list is a full-sync snapshot). Callers
// turn it into the "no more items" hint rather than an error line.
var ErrNotPaged = errors.New("source does not page its lists")

// More appends the next page of one feed — Ctrl+N on the list (M3
// ticket 05). The page number is the feed's stored paging depth (0 =
// never fetched, so More bootstraps a cold feed by fetching page 0);
// items merge through SaveFeedItems' upsert — no duplicate rows,
// read/star state untouched — and the depth advances only when the
// page had hits. end=true means the index served an empty page: the
// depth stays put, so a later Ctrl+N retries the same page once the
// index grows. Cursor-paged feeds (zhihu's follow, M4 ticket 05) fork
// to moreCursor: the walk's position is the stored opaque cursor, and
// the end signal is the server serving no next cursor.
func More(ctx context.Context, st *store.Store, srcs *core.Sources, sourceID, feedKey string) (res Result, end bool, err error) {
	src, err := source.New(sourceID, srcs)
	if err != nil {
		return Result{}, false, err
	}
	// The unpaged verdict is about the source, not the feed key — it
	// wins even for an unknown key (rss says "no pages" before "no
	// such feed").
	cursorSrc, isCursorSrc := src.(source.CursorPagedSource)
	if !isCursorSrc {
		if _, ok := src.(source.PagedSource); !ok {
			return Result{}, false, fmt.Errorf("more %s: %w", sourceID, ErrNotPaged)
		}
	}
	feed, ok := findFeed(src, feedKey)
	if !ok {
		return Result{}, false, fmt.Errorf("more %s: no feed %q", sourceID, feedKey)
	}
	if isCursorSrc && cursorSrc.CursorPaged(*feed) {
		return moreCursor(ctx, st, cursorSrc, *feed)
	}
	paged, ok := src.(source.PagedSource)
	if !ok {
		// A cursor source reaches here on its non-cursor feeds (zhihu's
		// board is a fixed window): no pages behind the first.
		return Result{}, false, fmt.Errorf("more %s: %w", sourceID, ErrNotPaged)
	}
	pageNo, err := st.LoadPagesFetched(ctx, sourceID, feedKey)
	if err != nil {
		return Result{}, false, fmt.Errorf("load depth %s: %w", feedKey, err)
	}
	items, err := paged.FetchPage(ctx, *feed, pageNo)
	if err != nil {
		return Result{}, false, err
	}
	res = Result{Source: sourceID, Feed: feedKey, Label: feed.Label}
	if len(items) == 0 { // the index's end: report it, keep the depth
		total, err := st.CountFeed(ctx, sourceID, feedKey)
		if err != nil {
			return Result{}, false, fmt.Errorf("count %s: %w", feedKey, err)
		}
		res.Total = total
		return res, true, nil
	}
	newCount, total, err := st.SaveFeedItems(ctx, sourceID, feedKey, items)
	if err != nil {
		return Result{}, false, fmt.Errorf("store %s: %w", feedKey, err)
	}
	if err := st.SavePagesFetched(ctx, sourceID, feedKey, pageNo+1); err != nil {
		return Result{}, false, fmt.Errorf("save depth %s: %w", feedKey, err)
	}
	res.New, res.Total = newCount, total
	return res, false, nil
}

// moreCursor is More's cursor twin (Ctrl+N on a cursor-paged feed).
// The walk's position is the stored opaque cursor — "" on a cold
// feed, so the first Ctrl+N bootstraps by fetching the first page,
// exactly like depth 0 does for paged feeds. The server's next
// cursor is the only end signal: served → the walk advances (even
// when the page filtered to empty — a pin streak is not the end, and
// holding the cursor there would brick Ctrl+N on it); absent → end,
// and the stored cursor stays so the next Ctrl+N re-reports the end
// instead of re-walking from the first page.
func moreCursor(ctx context.Context, st *store.Store, cps source.CursorPagedSource, f core.Feed) (Result, bool, error) {
	cursor, err := st.LoadCursor(ctx, cps.ID(), f.Key)
	if err != nil {
		return Result{}, false, fmt.Errorf("load cursor %s: %w", f.Key, err)
	}
	items, next, err := cps.FetchCursorPage(ctx, f, cursor)
	if err != nil {
		return Result{}, false, err
	}
	res := Result{Source: cps.ID(), Feed: f.Key, Label: f.Label}
	if len(items) == 0 {
		total, err := st.CountFeed(ctx, cps.ID(), f.Key)
		if err != nil {
			return Result{}, false, fmt.Errorf("count %s: %w", f.Key, err)
		}
		res.Total = total
	} else {
		newCount, total, err := st.SaveFeedItems(ctx, cps.ID(), f.Key, items)
		if err != nil {
			return Result{}, false, fmt.Errorf("store %s: %w", f.Key, err)
		}
		res.New, res.Total = newCount, total
	}
	if next == "" { // the server's end: report it, keep the position
		return res, true, nil
	}
	if err := st.SaveCursor(ctx, cps.ID(), f.Key, next); err != nil {
		return Result{}, false, fmt.Errorf("save cursor %s: %w", f.Key, err)
	}
	return res, false, nil
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
