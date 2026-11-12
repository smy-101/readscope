package core

import "time"

// Feed is one subscription inside a source: a sidebar entry with a
// stable key (the sources.toml slug, reused verbatim in item ids), a
// display label and the origin the items are fetched from. For RSS the
// URL is the feed's own address; other sources may use it differently.
type Feed struct {
	Key      string   // stable slug, e.g. "ruanyq" (sources.toml key)
	Label    string   // display name, defaults to Key
	URL      string   // fetch origin
	LabelKey string   // i18n key; when set the UI resolves the label through the language pack (hn's fixed feeds)
	ListSort ListSort // declared list ordering; zero value = snapshot time (the default for chronological feeds)
}

// ListSort is the ordering a source declares for one feed's list:
// by snapshot recency (ListSortTime, the zero value — every
// chronological feed) or by the points heat snapshot (ListSortPoints —
// hn's popularity feeds, whose Algolia return order is points order).
// The UI applies it when rows load; the store always serves a stable
// time order underneath.
type ListSort string

const (
	ListSortTime   ListSort = ""
	ListSortPoints ListSort = "points"
)

// Item is one list entry from any source, persisted as an offline
// snapshot (columns map 1:1 onto the items table). Body carries the
// full content as fetched — HTML for RSS — so reading and starring keep
// working after the original disappears.
type Item struct {
	ID          string // deterministic per-source id, e.g. "rss:ruanyq:<guid>"
	Source      string // "rss" | "hn" | ...
	Feed        string // Feed.Key within the source
	Title       string
	URL         string
	Author      string
	PublishedAt *time.Time // NULL allowed: feeds without dates
	FetchedAt   time.Time  // snapshot time, set by the fetcher
	Body        string     // full-text snapshot, offline-readable

	// Heat snapshots (hn): story points and comment count as of
	// FetchedAt, updated by every refresh. Pointers keep "missing"
	// distinct from zero — a degenerate record with no points renders
	// without that segment instead of claiming "0 pts". nil for sources
	// without heat semantics (rss).
	Points      *int
	NumComments *int

	// Meta is the source's own preformatted line for the list's meta
	// column, carried verbatim (zhihu daily's hint "作者 · 2 分钟阅读"
	// mixes author and reading time in one string the source will not
	// decompose). Sources that compose meta from semantic fields (hn,
	// the hot board, the follow feed) leave it empty; the UI routes
	// per feed, so this is only for atoms the source serves whole.
	Meta string
}

// Comment is one node of an item's comment tree (columns map 1:1
// onto the comments table). ParentID is "" for a top-level floor;
// Text is the HTML (or plain) snapshot as fetched; Score stays nil
// for sources whose API gives no per-comment points (Algolia's tree
// endpoint never does).
type Comment struct {
	ID        string
	ParentID  string
	Author    string
	Text      string
	Score     *int
	CreatedAt *time.Time
}

// Content is the detail layer behind one list item: what a
// DetailSource returns when the reader opens a post. For hn it is the
// whole comment tree in tree order — floors in payload order, each
// followed by its subtree, parents strictly before children.
type Content struct {
	Comments []Comment
}

// FetchState is one feed's conditional-request memory: the validators
// (ETag / Last-Modified) the server sent last time, so the next fetch
// can ask "anything new?" and a 304 answer costs no body transfer.
// LastFetched tracks when we last asked, independent of changed or
// not. The zero value means "never fetched": the request goes out
// unconditional. A server that sends no validators stores back the
// zero validators — which degrades to a plain refresh, by design.
type FetchState struct {
	ETag         string // response ETag verbatim, "" when the server sent none
	LastModified string // response Last-Modified verbatim
	LastFetched  time.Time
}
