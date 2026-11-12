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
// endpoint never does). NumChildren is the server-reported count of
// child comments (zhihu: an answer floor's comment_count — the
// floor-head triple's third segment), nil when the source reports
// none (hn: the tree itself carries the replies, so the count is
// len(replies), not a column).
type Comment struct {
	ID          string
	ParentID    string
	Author      string
	Text        string
	Score       *int
	CreatedAt   *time.Time
	NumChildren *int
}

// QuestionInfo is the question layer's meta triple behind one zhihu
// question item: the asker, the answer count and the visit count the
// v4 meta endpoint reports. It is snapshot data carried through
// Content on the detail fetch and cached onto the detail_fetch row
// (JSON), so the question layer's meta line renders offline. The
// formatting (提问人 提问 · N 个回答 · N 万浏览) is the UI's — the
// source folds, the UI unfolds, same as the heat snapshot.
type QuestionInfo struct {
	Author      string `json:"author"`
	AnswerCount int64  `json:"answers"`
	VisitCount  int64  `json:"visits"`
}

// Content is the detail layer behind one list item: what a
// DetailSource returns when the reader opens a post. For hn it is the
// whole comment tree in tree order — floors in payload order, each
// followed by its subtree, parents strictly before children — with
// every optional field zero. For zhihu questions the detail fetch is
// two requests (meta + first answers page): Body carries the cleaned
// question description (persisted into the body snapshot for offline
// re-opens), Question the meta triple, Comments the answer floors
// (each answer one top-level "comment"), and Next the feeds cursor —
// the address of the further answers Ctrl+N walks ("" = none served:
// the walk's end).
type Content struct {
	Comments []Comment
	Body     string
	Question *QuestionInfo
	Next     string
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
