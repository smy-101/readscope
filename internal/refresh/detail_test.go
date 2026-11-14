package refresh

// Ticket 03 tests: the Detail composition — source seam (DetailSource
// probe) tied to the store seam (comments DAO) the same way Source()
// ties Fetch to SaveFeedItems. Fake Algolia on loopback, real SQLite;
// the offline half closes the server to prove the cache stands alone.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/store"

	_ "github.com/smy-101/readscope/internal/source/heybox" // registry: heybox
	_ "github.com/smy-101/readscope/internal/source/hn"     // registry: hn
	_ "github.com/smy-101/readscope/internal/source/rss"    // registry: rss
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

// Ticket 03 (heybox): the detail compositions over the real heybox
// source — Detail caches body + floors with completed 楼中楼 and
// marks the post opened, DetailFloorsPage appends a later page's
// floors WITH their inline previews, DetailCommentsPage folds a
// floor's walked subs under it, and a re-open's merge keeps every
// paged-in floor (the merge rules' heybox halves are pinned in
// merge_test; this is the composition).
func TestHeyboxDetailCompositions(t *testing.T) {
	tree := func(offset string, totalPage int, floorIDs ...int) string {
		groups := make([]string, 0, len(floorIDs))
		for _, id := range floorIDs {
			groups = append(groups, fmt.Sprintf(`{"comment":[
			 {"commentid":%d,"userid":7,"create_at":1794386229,"text":"楼 %d","child_num":1,"up":%d,
			  "floor_num":%d,"user":{"username":"楼长%d"}},
			 {"commentid":%d,"userid":8,"create_at":1794386229,"text":"楼中楼 %d","child_num":0,"up":0,
			  "replyid":%d,"user":{"username":"层客%d"}}]}`,
				id, id, id%5, id, id, id+5000, id, id, id))
		}
		return fmt.Sprintf(`{"msg":"","status":"ok","result":{"link":{"text":"帖子正文[cube_滑稽]",
		 "share_url":"https://api.xiaoheihe.cn/v3/bbs/app/api/web/share?link_id=aaaa00000001"},
		 "comments":[%s],"total_page":%d}}`, strings.Join(groups, ","), totalPage)
	}
	subsFor := func(root int) string {
		return fmt.Sprintf(`{"msg":"","status":"ok","result":{"has_more":false,"lastval":0,"comments":[
		 {"commentid":%d,"userid":9,"create_at":1794386229,"text":"补全的楼中楼","up":0,
		  "replyid":%d,"user":{"username":"补全客"}}]}}`, root+9000, root)
	}
	pages := map[string]string{
		"0": tree("0", 2, 101, 102),
		"1": tree("1", 2, 103),
	}
	subs := map[string]string{"101": subsFor(101), "103": subsFor(103)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		switch r.URL.Path {
		case "/bbs/app/link/tree":
			fmt.Fprint(w, pages[q.Get("offset")])
		case "/bbs/app/comment/sub/comments":
			body, ok := subs[q.Get("root_comment_id")]
			if !ok {
				body = `{"msg":"","status":"ok","result":{"has_more":false,"lastval":0,"comments":[]}}`
			}
			fmt.Fprint(w, body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("READSCOPE_HEYBOX_BASE", srv.URL)
	t.Setenv("READSCOPE_HEYBOX_MINGAP", "0")
	t.Setenv("READSCOPE_HEYBOX_DEVICE", "0123456789abcdef0123456789abcdef")

	st, err := store.Open(filepath.Join(t.TempDir(), "heybox-detail.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	srcs := core.DefaultSources()
	it := core.Item{
		ID: "hb:link/aaaa00000001", Source: "heybox", Feed: "recommend",
		Title: "帖子", URL: "https://api.xiaoheihe.cn/v3/bbs/app/api/web/share?link_id=aaaa00000001",
	}
	// The list layer's row must exist before the detail can cache its
	// tree (comments reference items) — in the real flow F5 landed it.
	if _, _, err := st.SaveFeedItems(context.Background(), "heybox", "recommend", []core.Item{it}); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	// The open: body + two floors with their inline previews. Both
	// floors' child_num is served by their preview (gap 0), so this
	// composition dials once; the gap walk itself is pinned in the
	// heybox package tests.
	c, err := Detail(context.Background(), st, srcs, it)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if !strings.Contains(c.Body, "帖子正文[滑稽]") {
		t.Errorf("body = %q, want the degraded text", c.Body)
	}
	if len(c.Comments) != 4 { // 2 floors + 2 previews
		t.Fatalf("comments = %d nodes, want 4: %+v", len(c.Comments), c.Comments)
	}
	if c.Next != "1" {
		t.Errorf("Next = %q, want 1 (total_page 2)", c.Next)
	}
	// The cache: whole tree + the opened marker (offline replay's key).
	cached, err := st.CommentsForItem(context.Background(), it.ID)
	if err != nil || len(cached) != 4 {
		t.Fatalf("cached = %d nodes (err %v), want 4", len(cached), err)
	}
	opened, err := st.DetailFetched(context.Background(), it.ID)
	if err != nil || !opened {
		t.Fatalf("opened marker = %v (err %v), want true", opened, err)
	}

	// Ctrl+N on the floors layer: page 2's floor 103 lands WITH its
	// preview — the append that carries subtrees.
	merged, next, err := DetailFloorsPage(context.Background(), st, srcs, it, "1")
	if err != nil {
		t.Fatalf("DetailFloorsPage: %v", err)
	}
	if next != "" {
		t.Errorf("next = %q, want \"\" (page 2 of 2)", next)
	}
	var ids []string
	for _, cm := range merged {
		ids = append(ids, cm.ID)
	}
	if strings.Join(ids, ",") != "101,5101,102,5102,103,5103" {
		t.Fatalf("merged ids = %v, want page 2's floor+preview appended", ids)
	}

	// The reply layer's walk: floor 103's sub page folds under it.
	merged, next, err = DetailCommentsPage(context.Background(), st, srcs, it, "103", "")
	if err != nil {
		t.Fatalf("DetailCommentsPage: %v", err)
	}
	if next != "" {
		t.Errorf("comments next = %q, want \"\"", next)
	}
	ids = ids[:0]
	for _, cm := range merged {
		ids = append(ids, cm.ID)
	}
	if strings.Join(ids, ",") != "101,5101,102,5102,103,5103,9103" {
		t.Fatalf("walked ids = %v, want the new sub spliced under floor 103", ids)
	}
	if last := merged[len(merged)-1]; last.ParentID != "103" {
		t.Errorf("walked sub ParentID = %q, want 103", last.ParentID)
	}

	// The re-open merge: a fresh page-1 fetch must keep the paged-in
	// floor 103 and its walked sub at the tail.
	c, err = Detail(context.Background(), st, srcs, it)
	if err != nil {
		t.Fatalf("re-open Detail: %v", err)
	}
	ids = ids[:0]
	for _, cm := range c.Comments {
		ids = append(ids, cm.ID)
	}
	if strings.Join(ids, ",") != "101,5101,102,5102,103,5103,9103" {
		t.Fatalf("re-open ids = %v, want the paged floor and walked sub kept", ids)
	}
}
