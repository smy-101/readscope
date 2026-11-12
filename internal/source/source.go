// Package source defines readscope's deepest seam: every content origin
// (RSS now, Hacker News in M3) implements Source and registers its
// constructor from package init, keyed by the short id used in
// sources.toml's enabled list. Adding a source is adding a package.
package source

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/smy-101/readscope/internal/core"
)

// Source is one content origin (PLAN §4). Feeds come from the user's
// subscriptions; Fetch returns list-layer items with full-body
// snapshots, persisted by the caller.
type Source interface {
	// ID is the stable short id used in sources.toml ("rss", "hn").
	ID() string
	// Feeds lists this source's feeds for the sidebar.
	Feeds() []core.Feed
	// Fetch retrieves the current items of one feed.
	Fetch(ctx context.Context, f core.Feed) ([]core.Item, error)
}

// ConditionalSource is the optional conditional-fetch extension (the
// DetailSource pattern): a source that can replay the validators a
// server sent last time as If-None-Match / If-Modified-Since. A 304
// comes back as changed=false with no items and no body transfer —
// the caller keeps the stored snapshot untouched and re-saves the
// returned state (validators kept, last_fetched bumped). Sources that
// don't implement it always fetch in full.
type ConditionalSource interface {
	Source
	FetchConditional(ctx context.Context, f core.Feed, prev core.FetchState) (
		items []core.Item, next core.FetchState, changed bool, err error)
}

// DetailSource is the optional detail extension, the same
// probe-don't-register pattern as ConditionalSource: a source whose
// items have a second layer behind the list row. For hn that is the
// story's whole comment tree, fetched in one request; the caller
// snapshots it through the store's comments DAO. Sources that don't
// implement it (rss: the body already rides the list snapshot) are
// unaffected — callers probe with a type assertion.
type DetailSource interface {
	Source
	// DetailOnDemand reports whether this item carries a detail layer
	// behind its list row — the per-item probe the same-named
	// BodySource method is (one source may mix shapes: zhihu's
	// question cards have the three layers, its daily stories are
	// lazy bodies). Callers bind the reader's detail machinery only
	// where this answers true.
	DetailOnDemand(it core.Item) bool
	// Detail fetches one item's detail layer (the whole comment tree
	// for hn, one request; the two-request question open for zhihu).
	// The caller snapshots it through the store's comments DAO.
	Detail(ctx context.Context, it core.Item) (core.Content, error)
	// CommentURL is the web page behind one comment of the detail
	// layer — Ctrl+O/Ctrl+Y target it when the reader sits on a floor
	// or reply. Implementations accept both the bare comment id (what
	// the cache stores) and their own item ids (the story page).
	CommentURL(id string) string
}

// DetailPager is the optional detail-layer paging extension (the
// same probe-don't-register pattern as DetailSource): a detail source
// whose layers page on the network. A zhihu question is the shape —
// the question layer's answer floors walk the feeds cursor and the
// comment layer walks root_comment's offset, so Ctrl+N there is a
// fetch (with in-memory paging state; the comments cache replays
// whatever was loaded). hn's single request carries the whole tree,
// so it does not implement this and its reader Ctrl+N stays the
// in-memory hop.
type DetailPager interface {
	DetailSource
	// FetchFloorsPage returns the answer floors after cursor — "" the
	// first page — plus the cursor of the page after it ("" = none
	// served: the walk's end). Floors arrive as top-level comments
	// (ParentID ""), the same shape Detail's Comments carry.
	FetchFloorsPage(ctx context.Context, it core.Item, cursor string) (floors []core.Comment, next string, err error)
	// FetchAnswerComments returns one answer's comments at the root
	// walk's cursor position: "" the first page, else the previous
	// call's next (the server's paging.next VERBATIM — the real cursor
	// is a composite string the client must not parse). The roots
	// served there arrive with their child gaps completed (the
	// hot-preview children deduped against the fetched pages), tree
	// order, roots' ParentID the answer id. next is the following
	// page's cursor; "" = the answer's comments are exhausted.
	FetchAnswerComments(ctx context.Context, it core.Item, answerID string, cursor string) (comments []core.Comment, next string, err error)
	// AnswerURL is the Ctrl+O/Ctrl+Y target for one answer floor of
	// this item — the question page anchored at the answer. A bare
	// comment id cannot pair with its question alone, which is why
	// this rides here and not on CommentURL.
	AnswerURL(it core.Item, answerID string) string
}

// BodySource is the optional lazy-body extension, the same
// probe-don't-register pattern as DetailSource: a source whose items'
// full texts do NOT ride the list snapshot but are fetched on first
// open and cached into the body column, from where re-opens render
// offline with zero requests (zhihu's daily digest: the list
// endpoint serves no body). Sources whose bodies ride the list
// snapshot (rss, hn's selftexts) don't implement it — opening them
// is always offline. BodyOnDemand is the per-item probe: one source
// may carry both shapes (zhihu's question cards are detail-layer
// items, its daily stories lazy-body items).
type BodySource interface {
	Source
	// BodyOnDemand reports whether its full text is fetched on first
	// open rather than riding the list snapshot.
	BodyOnDemand(it core.Item) bool
	// FetchBody fetches one item's full text, preprocessed for the
	// render pipeline. First open only — the caller persists the
	// return through the store's body write and re-opens read the
	// snapshot instead of calling this again.
	FetchBody(ctx context.Context, it core.Item) (string, error)
}

// PagedSource is the optional list-paging extension, the same
// probe-don't-register pattern as ConditionalSource and DetailSource:
// a source whose list feeds can be walked forward page by page. hn
// implements it on Algolia's page parameter; rss feeds are full-sync
// snapshots with nothing behind the first page and don't. Callers
// probe with a type assertion — Ctrl+N on the list layers pages only
// where the source can.
type PagedSource interface {
	Source
	// FetchPage returns one page of the feed, page 0 the first. An
	// empty (non-error) page means the feed is exhausted: the caller
	// reports the end instead of advancing its paging depth.
	FetchPage(ctx context.Context, f core.Feed, page int) ([]core.Item, error)
}

// CursorPagedSource is the cursor variant of PagedSource, the same
// probe-don't-register pattern: a source whose list pages are
// addressed by an opaque server cursor instead of a page number
// (zhihu's moments feed: paging.next). A source picks the shape per
// feed — zhihu's hot board is a fixed window while its follow feed
// pages by cursor — so callers probe CursorPagedSource first, then
// PagedSource, landing on ErrNotPaged only when neither fits.
type CursorPagedSource interface {
	Source
	// CursorPaged reports whether f pages by cursor.
	CursorPaged(f core.Feed) bool
	// FetchCursorPage returns the page after cursor — "" the first
	// page — plus the cursor the server handed back for the page
	// after it ("" = none served: the walk's end). Unlike FetchPage,
	// an empty items slice is NOT exhaustion here: a page may filter
	// to nothing (a pin streak) while the server still serves a next
	// cursor; only next == "" ends the walk.
	FetchCursorPage(ctx context.Context, f core.Feed, cursor string) (items []core.Item, next string, err error)
}

// UserAgent identifies readscope to the content hosts that don't
// care about fingerprints — every host except zhihu's risk-controlled
// APIs, which carry their own DesktopUA (internal/source/zhihu). One
// client identity per host policy, so hosts and the doctor's probe
// all see the same reader. The version suffix is not wired in yet
// (cmd owns Version); worth revisiting if hosts care.
const UserAgent = "readscope (+https://github.com/smy-101/readscope)"

// Constructor builds one source from the user's subscription config;
// each implementation picks the tables it needs from Sources.
type Constructor func(srcs *core.Sources) (Source, error)

var (
	mu       sync.RWMutex
	registry = map[string]Constructor{}
)

// Register installs a source constructor; implementations call it from
// their package init. Registering a duplicate id panics: a collision is
// a programming error, not a runtime condition.
func Register(id string, ctor Constructor) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[id]; dup {
		panic("source: duplicate registration " + id)
	}
	registry[id] = ctor
}

// New builds the named source from the subscription config.
func New(id string, srcs *core.Sources) (Source, error) {
	mu.RLock()
	ctor, ok := registry[id]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown source %q (registered: %v)", id, IDs())
	}
	return ctor(srcs)
}

// IDs lists the registered source ids, sorted.
func IDs() []string {
	mu.RLock()
	defer mu.RUnlock()
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Enabled builds every source listed in sources.toml's enabled list,
// in that order (the TUI source rail order). An enabled id with no
// registered package is a config error naming the culprit.
func Enabled(srcs *core.Sources) ([]Source, error) {
	out := make([]Source, 0, len(srcs.Enabled))
	for _, id := range srcs.Enabled {
		src, err := New(id, srcs)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, nil
}
