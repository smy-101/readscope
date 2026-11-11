package store

import (
	"testing"

	"github.com/smy-101/readscope/internal/core"
)

func seedItems(t *testing.T, s *Store, items ...core.Item) {
	t.Helper()
	if _, _, err := s.SaveFeedItems(t.Context(), "rss", items[0].Feed, items); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestSetReadTogglesAndFilters(t *testing.T) {
	s := migrated(t)
	seedItems(t, s,
		item("rss:ex:1", "One", "b1"),
		item("rss:ex:2", "Two", "b2"),
	)

	// Fresh items are unread and unstarred.
	rows, err := s.FeedItems(t.Context(), "rss", "ex", false)
	if err != nil {
		t.Fatalf("FeedItems: %v", err)
	}
	if len(rows) != 2 || rows[0].Read || rows[0].Starred {
		t.Fatalf("fresh rows = %+v, want unread+unstarred", rows)
	}

	// Mark read: the row flips, the unread filter drops it, counts follow.
	if err := s.SetRead(t.Context(), "rss:ex:1", true); err != nil {
		t.Fatalf("SetRead: %v", err)
	}
	rows, err = s.FeedItems(t.Context(), "rss", "ex", false)
	if err != nil || len(rows) != 2 {
		t.Fatalf("FeedItems all: %v %d", err, len(rows))
	}
	if !rows[0].Read {
		t.Fatalf("item 1 should read: %+v", rows[0])
	}
	unread, err := s.FeedItems(t.Context(), "rss", "ex", true)
	if err != nil {
		t.Fatalf("FeedItems unread: %v", err)
	}
	if len(unread) != 1 || unread[0].Item.ID != "rss:ex:2" {
		t.Fatalf("unread filter = %+v, want only rss:ex:2", unread)
	}
	counts, err := s.FeedCounts(t.Context(), "rss")
	if err != nil {
		t.Fatalf("FeedCounts: %v", err)
	}
	if counts["ex"] != (FeedCount{Total: 2, Unread: 1}) {
		t.Fatalf("counts = %+v, want total 2 unread 1", counts["ex"])
	}

	// Toggle back to unread: the filter picks it up again.
	if err := s.SetRead(t.Context(), "rss:ex:1", false); err != nil {
		t.Fatalf("SetRead unread: %v", err)
	}
	unread, err = s.FeedItems(t.Context(), "rss", "ex", true)
	if err != nil || len(unread) != 2 {
		t.Fatalf("unread filter after toggle back: %v %d", err, len(unread))
	}
}

func TestSetStarredSnapshotsAndSurvives(t *testing.T) {
	s := migrated(t)
	starred := item("rss:ex:1", "One", "<p>body one</p>")
	seedItems(t, s, starred, item("rss:ex:2", "Two", "b2"))

	if err := s.SetStarred(t.Context(), starred, true); err != nil {
		t.Fatalf("SetStarred: %v", err)
	}
	rows, err := s.StarredItems(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("StarredItems: %v %d, want 1", err, len(rows))
	}
	got := rows[0]
	if !got.Starred || got.Read || got.Item.Title != "One" || got.Item.Body != "<p>body one</p>" {
		t.Fatalf("starred row = %+v", got)
	}

	// Star is orthogonal to read: marking read keeps the star.
	if err := s.SetRead(t.Context(), starred.ID, true); err != nil {
		t.Fatalf("SetRead: %v", err)
	}
	rows, _ = s.StarredItems(t.Context())
	if len(rows) != 1 || !rows[0].Read {
		t.Fatalf("read+starred row = %+v", rows)
	}

	// The favorite survives the items row disappearing: the snapshot
	// fills in what the LEFT JOIN misses.
	if _, err := s.db.Exec(`DELETE FROM items WHERE id = ?`, starred.ID); err != nil {
		t.Fatalf("delete item: %v", err)
	}
	rows, err = s.StarredItems(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("star must survive item deletion: %v %d", err, len(rows))
	}
	if rows[0].Item.ID != starred.ID || rows[0].Item.Title != "One" || rows[0].Item.Body != "<p>body one</p>" {
		t.Fatalf("snapshot-restored item = %+v", rows[0].Item)
	}

	// Unstar drops the favorite and its snapshot.
	if err := s.SetStarred(t.Context(), starred, false); err != nil {
		t.Fatalf("unstar: %v", err)
	}
	if rows, _ = s.StarredItems(t.Context()); len(rows) != 0 {
		t.Fatalf("unstarred feed still lists favorites: %+v", rows)
	}
	all, _ := s.FeedItems(t.Context(), "rss", "ex", false)
	if len(all) != 1 { // only item 2 remains in the cache
		t.Fatalf("cache after eviction: %d rows", len(all))
	}
}

func TestMarkFeedReadMarksOnlyThatFeed(t *testing.T) {
	s := migrated(t)
	seedItems(t, s, item("rss:ex:1", "One", "b"), item("rss:ex:2", "Two", "b"))
	other := item("rss:other:1", "Elsewhere", "b")
	other.Feed = "other"
	seedItems(t, s, other)
	if err := s.SetStarred(t.Context(), item("rss:ex:1", "One", "b"), true); err != nil {
		t.Fatalf("star: %v", err)
	}

	if err := s.MarkFeedRead(t.Context(), "rss", "ex"); err != nil {
		t.Fatalf("MarkFeedRead: %v", err)
	}
	rows, _ := s.FeedItems(t.Context(), "rss", "ex", true)
	if len(rows) != 0 {
		t.Fatalf("ex should be all read, unread rows = %+v", rows)
	}
	otherRows, _ := s.FeedItems(t.Context(), "rss", "other", false)
	if len(otherRows) != 1 || otherRows[0].Read {
		t.Fatalf("other feed must be untouched: %+v", otherRows)
	}
	// The star survives a bulk mark-read.
	if rows, _ := s.StarredItems(t.Context()); len(rows) != 1 {
		t.Fatalf("star lost to MarkFeedRead: %+v", rows)
	}
}
