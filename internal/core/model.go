package core

import "time"

// Feed is one subscription inside a source: a sidebar entry with a
// stable key (the sources.toml slug, reused verbatim in item ids), a
// display label and the origin the items are fetched from. For RSS the
// URL is the feed's own address; other sources may use it differently.
type Feed struct {
	Key      string // stable slug, e.g. "ruanyq" (sources.toml key)
	Label    string // display name, defaults to Key
	URL      string // fetch origin
	LabelKey string // i18n key; when set the UI resolves the label through the language pack (hn's fixed feeds)
}

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
