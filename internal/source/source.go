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
	Detail(ctx context.Context, it core.Item) (core.Content, error)
	// CommentURL is the web page behind one comment of the detail
	// layer — Ctrl+O/Ctrl+Y target it when the reader sits on a floor
	// or reply. Implementations accept both the bare comment id (what
	// the cache stores) and their own item ids (the story page).
	CommentURL(id string) string
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
