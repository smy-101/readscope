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
