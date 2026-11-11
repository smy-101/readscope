package store

import (
	"testing"
	"time"

	"github.com/smy-101/readscope/internal/core"
)

func nowUTC() time.Time { return time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC) }

func item(id, title, body string) core.Item {
	return core.Item{
		ID: id, Source: "rss", Feed: "ex",
		Title: title, URL: "https://example.com/" + id,
		PublishedAt: ptrTime(nowUTC()), FetchedAt: nowUTC(), Body: body,
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func migrated(t *testing.T) *Store {
	t.Helper()
	s := openTemp(t)
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s
}

func TestSaveFeedItemsInsertsAndCounts(t *testing.T) {
	s := migrated(t)
	items := []core.Item{
		item("rss:ex:1", "One", "<p>first body</p>"),
		item("rss:ex:2", "Two", "<p>second body</p>"),
	}
	newCount, total, err := s.SaveFeedItems(t.Context(), "rss", "ex", items)
	if err != nil {
		t.Fatalf("SaveFeedItems: %v", err)
	}
	if newCount != 2 || total != 2 {
		t.Fatalf("new/total = %d/%d, want 2/2", newCount, total)
	}
}

func TestSaveFeedItemsIsIdempotent(t *testing.T) {
	s := migrated(t)
	first := []core.Item{item("rss:ex:1", "One", "<p>v1</p>")}
	if _, _, err := s.SaveFeedItems(t.Context(), "rss", "ex", first); err != nil {
		t.Fatalf("first save: %v", err)
	}
	// Same deterministic id, changed content: update, not a new row.
	second := []core.Item{item("rss:ex:1", "One edited", "<p>v2</p>")}
	newCount, total, err := s.SaveFeedItems(t.Context(), "rss", "ex", second)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if newCount != 0 || total != 1 {
		t.Fatalf("new/total = %d/%d, want 0/1 (no duplicate rows)", newCount, total)
	}
	var title, body string
	if err := s.db.QueryRow(`SELECT title, body FROM items WHERE id = 'rss:ex:1'`).Scan(&title, &body); err != nil {
		t.Fatalf("query: %v", err)
	}
	if title != "One edited" || body != "<p>v2</p>" {
		t.Fatalf("snapshot not updated: %q %q", title, body)
	}
}

func TestSaveFeedItemsPreservesReadState(t *testing.T) {
	s := migrated(t)
	if _, _, err := s.SaveFeedItems(t.Context(), "rss", "ex",
		[]core.Item{item("rss:ex:1", "One", "b")}); err != nil {
		t.Fatalf("save: %v", err)
	}
	readAt := nowUTC()
	if _, err := s.db.Exec(`INSERT INTO read_state (item_id, read_at) VALUES ('rss:ex:1', ?)`, readAt); err != nil {
		t.Fatalf("seed read_state: %v", err)
	}
	// Driver encoding of timestamps is not our contract here — snapshot
	// the stored value as-is and prove refresh leaves it bit-identical.
	var before string
	if err := s.db.QueryRow(`SELECT read_at FROM read_state WHERE item_id = 'rss:ex:1'`).Scan(&before); err != nil {
		t.Fatalf("seed read: %v", err)
	}
	// Refresh with changed content; read state must survive untouched.
	if _, _, err := s.SaveFeedItems(t.Context(), "rss", "ex",
		[]core.Item{item("rss:ex:1", "One edited", "b2")}); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	var n int
	var after string
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM read_state`).Scan(&n); err != nil {
		t.Fatalf("count read_state: %v", err)
	}
	if err := s.db.QueryRow(`SELECT read_at FROM read_state WHERE item_id = 'rss:ex:1'`).Scan(&after); err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if n != 1 || before == "" || before != after {
		t.Fatalf("read_state clobbered: n=%d read_at %q → %q", n, before, after)
	}
}

func TestSaveFeedItemsSyncsFTSWithPlainText(t *testing.T) {
	s := migrated(t)
	items := []core.Item{
		item("rss:ex:1", "Kubernetes basics", `<div class="x"><p>Learn orchestration <script>var a=1;</script>today</p></div>`),
		item("rss:ex:2", "Another", "<p>plain words</p>"),
	}
	if _, _, err := s.SaveFeedItems(t.Context(), "rss", "ex", items); err != nil {
		t.Fatalf("save: %v", err)
	}
	ids, err := s.SearchFTS(t.Context(), "orchestration")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(ids) != 1 || ids[0] != "rss:ex:1" {
		t.Fatalf("body text not searchable: %v", ids)
	}
	// Markup and script bodies must not leak into the index.
	for _, word := range []string{"div", "script", "class"} {
		if ids, err := s.SearchFTS(t.Context(), word); err != nil || len(ids) != 0 {
			t.Fatalf("fts matched markup %q: %v (%v)", word, ids, err)
		}
	}
	// A changed body re-indexes: old words gone, new words found.
	if _, _, err := s.SaveFeedItems(t.Context(), "rss", "ex",
		[]core.Item{item("rss:ex:2", "Another", "<p>rewritten words</p>")}); err != nil {
		t.Fatalf("resave: %v", err)
	}
	if ids, _ := s.SearchFTS(t.Context(), "plain"); len(ids) != 0 {
		t.Fatalf("stale fts entry survived: %v", ids)
	}
	if ids, _ := s.SearchFTS(t.Context(), "rewritten"); len(ids) != 1 {
		t.Fatalf("rewritten body not indexed: %v", ids)
	}
}

func TestSaveFeedItemsDedupesWithinBatch(t *testing.T) {
	s := migrated(t)
	dup := []core.Item{item("rss:ex:1", "First", "a"), item("rss:ex:1", "Second", "b")}
	newCount, total, err := s.SaveFeedItems(t.Context(), "rss", "ex", dup)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if newCount != 1 || total != 1 {
		t.Fatalf("new/total = %d/%d, want 1/1", newCount, total)
	}
}

func TestSaveFeedItemsRejectsForeignFeed(t *testing.T) {
	s := migrated(t)
	other := item("rss:other:1", "X", "b")
	other.Feed = "other"
	if _, _, err := s.SaveFeedItems(t.Context(), "rss", "ex", []core.Item{other}); err == nil {
		t.Fatal("expected error for item outside the declared feed")
	}
}

func TestFtsTextStripsMarkup(t *testing.T) {
	got := ftsText("<p>Hello <b>world</b></p><p>Second block</p>")
	if got != "Hello world Second block" {
		t.Fatalf("ftsText = %q", got)
	}
	if got := ftsText("plain &amp; simple"); got != "plain & simple" {
		t.Fatalf("entity decode = %q", got)
	}
	if got := ftsText("  already\n  plain\t"); got != "already plain" {
		t.Fatalf("collapse = %q", got)
	}
	if got := ftsText(""); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestFeedItemsNewestFirstWithReadFlag(t *testing.T) {
	s := migrated(t)
	mk := func(id string, age time.Duration) core.Item {
		it := item(id, id, "body of "+id)
		it.PublishedAt = ptrTime(nowUTC().Add(-age))
		return it
	}
	noDate := item("rss:ex:nodate", "nodate", "no date body")
	noDate.PublishedAt = nil
	if _, _, err := s.SaveFeedItems(t.Context(), "rss", "ex", []core.Item{
		mk("rss:ex:old", 48*time.Hour),
		mk("rss:ex:new", time.Hour),
		noDate,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Read state is ticket 04's DAO; seed it raw for now.
	if _, err := s.db.Exec(`INSERT INTO read_state (item_id, read_at) VALUES ('rss:ex:old', ?)`, nowUTC()); err != nil {
		t.Fatalf("seed read_state: %v", err)
	}

	rows, err := s.FeedItems(t.Context(), "rss", "ex", false)
	if err != nil {
		t.Fatalf("FeedItems: %v", err)
	}
	// Newest first; items without a publish date slot by fetch time
	// (just now, so first). Read flag comes from the read_state join.
	want := []struct {
		id   string
		read bool
	}{
		{"rss:ex:nodate", false},
		{"rss:ex:new", false},
		{"rss:ex:old", true},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i, w := range want {
		if rows[i].Item.ID != w.id || rows[i].Read != w.read {
			t.Fatalf("row %d = %s read=%v, want %s read=%v", i, rows[i].Item.ID, rows[i].Read, w.id, w.read)
		}
	}
	// The snapshot columns travel with the row (offline reading).
	if rows[1].Item.Title != "rss:ex:new" || rows[1].Item.Body != "body of rss:ex:new" || rows[1].Item.Source != "rss" || rows[1].Item.Feed != "ex" {
		t.Fatalf("snapshot columns lost: %+v", rows[1].Item)
	}
	if rows[2].Item.PublishedAt == nil || rows[0].Item.PublishedAt != nil {
		t.Fatalf("published_at roundtrip broken: %+v / %+v", rows[2].Item.PublishedAt, rows[0].Item.PublishedAt)
	}
}

func TestFeedItemsEmptyFeedIsEmpty(t *testing.T) {
	s := migrated(t)
	rows, err := s.FeedItems(t.Context(), "rss", "nothing", false)
	if err != nil {
		t.Fatalf("FeedItems on empty feed: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %+v, want none", rows)
	}
}

func TestFeedCountsTotalsAndUnread(t *testing.T) {
	s := migrated(t)
	mk := func(feed, id string) core.Item {
		it := item(id, id, "body")
		it.Feed = feed
		return it
	}
	if _, _, err := s.SaveFeedItems(t.Context(), "rss", "a", []core.Item{
		mk("a", "rss:a:1"), mk("a", "rss:a:2"), mk("a", "rss:a:3"),
	}); err != nil {
		t.Fatalf("save a: %v", err)
	}
	if _, _, err := s.SaveFeedItems(t.Context(), "rss", "b", []core.Item{
		mk("b", "rss:b:1"),
	}); err != nil {
		t.Fatalf("save b: %v", err)
	}
	for _, id := range []string{"rss:a:1", "rss:b:1"} {
		if _, err := s.db.Exec(`INSERT INTO read_state (item_id, read_at) VALUES (?, ?)`, id, nowUTC()); err != nil {
			t.Fatalf("seed read %s: %v", id, err)
		}
	}

	counts, err := s.FeedCounts(t.Context(), "rss")
	if err != nil {
		t.Fatalf("FeedCounts: %v", err)
	}
	want := map[string]FeedCount{"a": {Total: 3, Unread: 2}, "b": {Total: 1, Unread: 0}}
	if len(counts) != len(want) {
		t.Fatalf("counts = %+v, want %v", counts, want)
	}
	for feed, w := range want {
		if counts[feed] != w {
			t.Fatalf("counts[%s] = %+v, want %+v", feed, counts[feed], w)
		}
	}
}

func TestFeedCountsEmptySource(t *testing.T) {
	s := migrated(t)
	counts, err := s.FeedCounts(t.Context(), "rss")
	if err != nil {
		t.Fatalf("FeedCounts: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("counts = %+v, want empty", counts)
	}
}

func TestCountFeed(t *testing.T) {
	s := migrated(t)
	if _, _, err := s.SaveFeedItems(t.Context(), "rss", "ex",
		[]core.Item{item("rss:ex:1", "One", "b")}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if n, err := s.CountFeed(t.Context(), "rss", "ex"); err != nil || n != 1 {
		t.Fatalf("CountFeed = %d, %v", n, err)
	}
	if n, err := s.CountFeed(t.Context(), "rss", "none"); err != nil || n != 0 {
		t.Fatalf("empty feed count = %d, %v", n, err)
	}
}

// HN story ids are global per source ("hn:algolia:<storyID>"), so the
// same story can arrive from two feeds — Front and New both list a
// story that just hit the front page. The row re-homes to the feed
// that last saw it: one row, not a PK violation, and read state (keyed
// by item id) survives the move untouched.
func TestSaveFeedItemsRehomesCrossFeedID(t *testing.T) {
	s := migrated(t)
	hnStory := func(feed, title string) core.Item {
		return core.Item{
			ID: "hn:algolia:1001", Source: "hn", Feed: feed,
			Title: title, FetchedAt: nowUTC(),
		}
	}
	newCount, total, err := s.SaveFeedItems(t.Context(), "hn", "front",
		[]core.Item{hnStory("front", "Hot story")})
	if err != nil || newCount != 1 || total != 1 {
		t.Fatalf("first save = %d/%d, %v", newCount, total, err)
	}
	if err := s.SetRead(t.Context(), "hn:algolia:1001", true); err != nil {
		t.Fatalf("SetRead: %v", err)
	}

	// Same story id served by the new feed: re-homed, not a new row.
	newCount, total, err = s.SaveFeedItems(t.Context(), "hn", "new",
		[]core.Item{hnStory("new", "Hot story")})
	if err != nil {
		t.Fatalf("cross-feed save: %v", err)
	}
	if newCount != 0 {
		t.Fatalf("re-homing is not new: new = %d, want 0", newCount)
	}
	if total != 1 {
		t.Fatalf("new feed total = %d, want 1", total)
	}
	if n, err := s.CountFeed(t.Context(), "hn", "front"); err != nil || n != 0 {
		t.Fatalf("front should be empty after re-home, got %d, %v", n, err)
	}
	rows, err := s.FeedItems(t.Context(), "hn", "new", false)
	if err != nil || len(rows) != 1 {
		t.Fatalf("FeedItems(new) = %d rows, %v", len(rows), err)
	}
	if !rows[0].Read {
		t.Fatal("read state must survive the re-home")
	}
}
