package refresh

// Ticket 03 tests: the Detail composition — source seam (DetailSource
// probe) tied to the store seam (comments DAO) the same way Source()
// ties Fetch to SaveFeedItems. Fake Algolia on loopback, real SQLite;
// the offline half closes the server to prove the cache stands alone.

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/store"

	_ "github.com/smy-101/readscope/internal/source/hn"  // registry: hn
	_ "github.com/smy-101/readscope/internal/source/rss" // registry: rss
)

// storyTreeFixture: one floor with one reply, enough shape to prove
// the tree survives the fetch → save → read round trip.
const storyTreeFixture = `{
  "id": 1001, "type": "story", "author": "alice", "title": "Why link posts win",
  "url": "https://example.com/article", "points": 120, "created_at_i": 1700000000,
  "children": [
    {"id": 10, "type": "comment", "author": "bob", "text": "root comment",
     "created_at_i": 1700000100, "children": [
       {"id": 11, "type": "comment", "author": "carol", "text": "reply",
        "created_at_i": 1700000200, "children": []}
     ]}
  ]
}`

// detailServer serves the fixture for story 1001.
func detailServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/items/1001") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(storyTreeFixture))
			return
		}
		http.Error(w, "no fixture", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// detailStore is a migrated store holding the list-layer snapshot of
// story 1001 (Detail's cache references the items row).
func detailStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "detail.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, _, err := st.SaveFeedItems(t.Context(), "hn", "front", []core.Item{{
		ID: "hn:algolia:1001", Source: "hn", Feed: "front",
		Title: "Why link posts win", URL: "https://example.com/article",
		Author: "alice", FetchedAt: nowStamp(), Body: "https://example.com/article",
	}}); err != nil {
		t.Fatalf("save story: %v", err)
	}
	return st
}

func hnStoryItem() core.Item {
	return core.Item{ID: "hn:algolia:1001", Source: "hn", Feed: "front", Title: "Why link posts win"}
}

func nowStamp() (t time.Time) { return time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC) }

// The tracer bullet: open post → fetch whole tree → cache it. What
// Detail returns is what the cache holds, and a second open (repeat
// Detail) replaces cleanly — the whole-tree contract rides through
// the composition.
func TestDetailCachesTree(t *testing.T) {
	srv := detailServer(t)
	t.Setenv("READSCOPE_HN_BASE", srv.URL+"/api/v1")
	st := detailStore(t)

	c, err := Detail(t.Context(), st, core.DefaultSources(), hnStoryItem())
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(c.Comments) != 2 || c.Comments[0].ID != "10" || c.Comments[1].ParentID != "10" {
		t.Fatalf("returned content = %+v", c.Comments)
	}
	cached, err := st.CommentsForItem(t.Context(), "hn:algolia:1001")
	if err != nil {
		t.Fatalf("CommentsForItem: %v", err)
	}
	if len(cached) != 2 || cached[1].ParentID != "10" {
		t.Fatalf("cached tree = %+v", cached)
	}

	if _, err := Detail(t.Context(), st, core.DefaultSources(), hnStoryItem()); err != nil {
		t.Fatalf("repeat Detail: %v", err)
	}
	if cached, _ := st.CommentsForItem(t.Context(), "hn:algolia:1001"); len(cached) != 2 {
		t.Fatalf("repeat Detail duplicated rows: %d", len(cached))
	}
}

// Offline reading: after one successful open, the server can be gone
// (network down, later session) and the cached tree still reads in
// full. A failed refresh never damages the cache it came from.
func TestDetailOfflineFromCache(t *testing.T) {
	srv := detailServer(t)
	t.Setenv("READSCOPE_HN_BASE", srv.URL+"/api/v1")
	st := detailStore(t)

	if _, err := Detail(t.Context(), st, core.DefaultSources(), hnStoryItem()); err != nil {
		t.Fatalf("online Detail: %v", err)
	}
	srv.Close() // network goes away

	if _, err := Detail(t.Context(), st, core.DefaultSources(), hnStoryItem()); err == nil {
		t.Fatal("offline Detail must fail (nothing to fetch)")
	}
	cached, err := st.CommentsForItem(t.Context(), "hn:algolia:1001")
	if err != nil {
		t.Fatalf("offline CommentsForItem: %v", err)
	}
	if len(cached) != 2 || cached[0].Author != "bob" || cached[1].ParentID != "10" {
		t.Fatalf("cache damaged by failed refresh: %+v", cached)
	}
}

// rss has no detail layer — its body already rides the list snapshot.
// Probing it through the same entry point is a clean named error, not
// a crash and not a registry change: sources without the optional
// extension are unaffected.
func TestDetailRejectsNonDetailSource(t *testing.T) {
	st := detailStore(t)
	it := core.Item{ID: "rss:ex:1", Source: "rss", Feed: "ex", Title: "rss post"}
	_, err := Detail(t.Context(), st, core.DefaultSources(), it)
	if err == nil {
		t.Fatal("want error for source without detail layer")
	}
	if !strings.Contains(err.Error(), "rss") {
		t.Fatalf("error must name the source: %v", err)
	}
}

// The star snapshot boundary: starring an hn post with a cached
// comment tree snapshots the post (title/url/body — the link-post
// body is its one-line article link), never the comments. The two
// caches coexist without contaminating each other: the favorite
// stays readable without dragging its (large, refreshable)
// discussion along.
func TestStarSnapshotExcludesComments(t *testing.T) {
	srv := detailServer(t)
	t.Setenv("READSCOPE_HN_BASE", srv.URL+"/api/v1")
	st := detailStore(t)

	if _, err := Detail(t.Context(), st, core.DefaultSources(), hnStoryItem()); err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if cached, _ := st.CommentsForItem(t.Context(), "hn:algolia:1001"); len(cached) != 2 {
		t.Fatalf("precondition: comment cache must hold the tree, got %d", len(cached))
	}
	if err := st.SetStarred(t.Context(), hnStoryItem(), true); err != nil {
		t.Fatalf("star: %v", err)
	}
	rows, err := st.StarredItems(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("starred rows = %v, %v", rows, err)
	}
	if got := rows[0].Item.Body; got != "https://example.com/article" {
		t.Fatalf("star body must be the story's own snapshot, got %q", got)
	}
	if strings.Contains(rows[0].Item.Title+rows[0].Item.Body, "root comment") {
		t.Fatal("comment text leaked into the star snapshot")
	}
}
