package refresh

// Ticket 06 tests: the detail-layer merges — the pure folding rules
// (refresh must never unload what the reader paged in or read) and
// the two Ctrl+N compositions over the real zhihu source against a
// fake data plane.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/store"

	_ "github.com/smy-101/readscope/internal/source/zhihu" // registry: zhihu
)

func floors(ids ...string) []core.Comment {
	out := make([]core.Comment, 0, len(ids))
	for _, id := range ids {
		out = append(out, core.Comment{ID: id})
	}
	return out
}

// A floor's comment subtree follows it, parents before children.
func subtree(floorID string, ids ...string) []core.Comment {
	out := make([]core.Comment, 0, len(ids))
	for _, id := range ids {
		parent := floorID
		if i := strings.Index(id, "."); i > 0 {
			parent = id[:i] // "f1.2" replies to "f1"
		}
		out = append(out, core.Comment{ID: id, ParentID: parent})
	}
	return out
}

// The background refresh's merge: fresh page-1 floors lead with
// fresh data, every old floor survives (Ctrl+N's pages, and even a
// floor the fresh page no longer serves — unloading beats lingering
// as the worse miss), and every survivor keeps the comment subtree
// the cache holds.
func TestMergeDetailTreeKeepsPagedAndLoaded(t *testing.T) {
	// Tree order: floors each followed by their subtree.
	old := []core.Comment{}
	old = append(old, floors("f1")...)
	old = append(old, subtree("f1", "f1.1")...)
	old = append(old, floors("f2")...)
	old = append(old, floors("f3")...)
	old = append(old, subtree("f3", "f3.1", "f3.1.1")...)
	old = append(old, floors("f4")...)
	old = append(old, floors("f5")...)
	old = append(old, subtree("f5", "f5.1")...)

	// Fresh page 1: f1 (data changed), f3, f9 (new). f2/f4/f5 sit
	// beyond the page and survive untouched at the tail.
	fresh := floors("f1", "f3", "f9")
	fresh[0].Author = "updated"
	got := mergeDetailTree(old, fresh)

	want := []string{"f1", "f1.1", "f3", "f3.1", "f3.1.1", "f9", "f2", "f4", "f5", "f5.1"}
	if len(got) != len(want) {
		t.Fatalf("merged = %d nodes, want %d:\n%+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("merged[%d] = %s, want %s (whole merge: %+v)", i, got[i].ID, id, got)
		}
	}
	if got[0].Author != "updated" {
		t.Error("the fresh floor copy must lead, not the stale cache's")
	}
}

// A cold cache (first open): the fresh page IS the tree.
func TestMergeDetailTreeColdCache(t *testing.T) {
	got := mergeDetailTree(nil, floors("a", "b"))
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("cold merge = %+v", got)
	}
}

// A floor promoted from beyond the span INTO the fresh page (the
// server re-ranked it): the fresh copy wins, the tail copy must not
// double it.
func TestMergeDetailTreeNoDoubleOnReRank(t *testing.T) {
	old := append([]core.Comment{}, floors("f1", "f2", "f3", "f4")...)
	got := mergeDetailTree(old, floors("f4", "f1", "x"))
	want := "f4,f1,x,f2,f3"
	var ids []string
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	if strings.Join(ids, ",") != want {
		t.Fatalf("re-ranked merge = %v, want %v", ids, want)
	}
}

// The floors append: new ids append at the tail, re-served ids skip
// whole — never a doubled row, never interleaved.
func TestAppendFloorsDedupsAndAppends(t *testing.T) {
	old := append(append([]core.Comment{}, floors("f1", "f2")...), subtree("f1", "f1.1")...)
	page := floors("f2", "f3") // f2 re-served, f3 new
	got := appendFloors(old, page)
	want := []string{"f1", "f2", "f1.1", "f3"}
	if len(got) != len(want) {
		t.Fatalf("append = %+v, want %v", got, want)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("append[%d] = %s, want %s", i, got[i].ID, id)
		}
	}
}

// The comments append: new roots splice right after the answer's
// existing subtree (not the whole tree's tail — a second answer's
// comments must not adopt them), cached ids skip whole, and a new
// child of a cached root still lands next to it.
func TestAppendAnswerCommentsSplicesUnderTheAnswer(t *testing.T) {
	tree := []core.Comment{}                      // tree order
	tree = append(tree, floors("a1")...)          // answer one
	tree = append(tree, subtree("a1", "r1")...)   // a1's loaded root
	tree = append(tree, floors("a2")...)          // answer two
	tree = append(tree, subtree("a2", "r9")...)   // a2's loaded root
	page := []core.Comment{}                      // fetched page for a1
	page = append(page, subtree("a1", "r1")...)   // r1 re-served
	page = append(page, subtree("a1", "r2")...)   // r2 new
	page = append(page, subtree("r2", "r2.1")...) // r2's child

	got, err := appendAnswerComments(tree, "a1", page)
	if err != nil {
		t.Fatalf("appendAnswerComments: %v", err)
	}
	want := []string{"a1", "r1", "r2", "r2.1", "a2", "r9"}
	if len(got) != len(want) {
		t.Fatalf("append = %+v, want %v", got, want)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("append[%d] = %s, want %s (whole: %+v)", i, got[i].ID, id, got)
		}
	}
}

// The answer floor must be in the cache — entering a comment layer
// from a floor on screen guarantees it; anything else fails by name.
func TestAppendAnswerCommentsNamesTheMissingFloor(t *testing.T) {
	if _, err := appendAnswerComments(floors("a1"), "nope", subtree("nope", "x")); err == nil ||
		!strings.Contains(err.Error(), "nope") {
		t.Fatalf("missing floor = %v, want a named error", err)
	}
}

// --- the compositions over the real zhihu source --------------------------

// detailPlane serves one question's whole detail data plane: meta,
// two answers pages by cursor, and one answer's comments at two
// cursor positions (the 2026-12 real contract: first page rides no
// offset, digits-only offsets eat a 200 zombie empty page, the next
// page's cursor is a composite string followed verbatim). The cookie
// gate mirrors the measured matrix loosely — enough that a missing
// cookie fails loudly.
func detailPlane(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	gate := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") == "" {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"error":{"code":100}}`)
			return
		}
	}
	mux.HandleFunc("/api/v4/questions/1001", func(w http.ResponseWriter, r *http.Request) {
		gate(w, r)
		fmt.Fprint(w, `{"title":"问题","author":{"name":"问主"},"answer_count":3,
		 "visit_count":5600000,"detail":"<p>描述</p>"}`)
	})
	mux.HandleFunc("/api/v4/questions/1001/feeds", func(w http.ResponseWriter, r *http.Request) {
		gate(w, r)
		if r.URL.Query().Get("cursor") == "" {
			fmt.Fprint(w, `{"data":[
			 {"target":{"id":9001,"type":"answer","author":{"name":"甲"},"voteup_count":10,
			  "comment_count":2,"content":"<p>答一</p>"}},
			 {"target":{"id":9002,"type":"answer","author":{"name":"乙"},"voteup_count":0,
			  "comment_count":0,"content":"<p>答二</p>"}}],
			 "paging":{"next":"http://`+r.Host+`/api/v4/questions/1001/feeds?cursor=p2","is_end":false}}`)
			return
		}
		fmt.Fprint(w, `{"data":[
		 {"target":{"id":9003,"type":"answer","author":{"name":"丙"},"voteup_count":1,
		  "comment_count":0,"content":"<p>答三</p>"}}],
		 "paging":{"next":"","is_end":true}}`)
	})
	mux.HandleFunc("/api/v4/comment_v5/answers/9001/root_comment", func(w http.ResponseWriter, r *http.Request) {
		gate(w, r)
		const composite = "387770585_11616175634_0"
		off := r.URL.Query().Get("offset")
		if off != "" && off != composite && !strings.Contains(off, "_") {
			// the real API's zombie quirk: digits-only offsets answer
			// 200 with an empty page, uniformly
			fmt.Fprint(w, `{"data":[],"paging":{"next":"http://`+r.Host+`/api/v4/comment_v5/answers/9001/root_comment?limit=10&offset=`+off+`&order_by=score","is_end":false}}`)
			return
		}
		if off == composite {
			fmt.Fprint(w, `{"data":[{"id":703,"author":{"name":"末根"},"like_count":0,"content":"末页"}],
			 "paging":{"next":"","is_end":true}}`)
			return
		}
		fmt.Fprint(w, `{"data":[
		 {"id":701,"author":{"name":"根一"},"like_count":3,"content":"根一",
		  "child_comment_count":1,"child_comments":[]},
		 {"id":702,"author":{"name":"根二"},"like_count":0,"content":"根二"}],
		 "paging":{"next":"http://`+r.Host+`/api/v4/comment_v5/answers/9001/root_comment?order_by=score&limit=20&offset=`+composite+`","is_end":false}}`)
	})
	mux.HandleFunc("/api/v4/comment_v5/comment/701/child_comment", func(w http.ResponseWriter, r *http.Request) {
		gate(w, r)
		fmt.Fprint(w, `{"data":[{"id":801,"author":{"name":"子回复"},"like_count":0,"content":"<p>子</p>"}],
		 "paging":{"next":"","is_end":true}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// questionStore is a migrated store holding the question's list row
// and a valid cookie file (the v4 family is cookie-gated).
func questionStore(t *testing.T, srv *httptest.Server) *store.Store {
	t.Helper()
	t.Setenv("READSCOPE_CONFIG", t.TempDir())
	t.Setenv("READSCOPE_ZHIHU_BASE", srv.URL)
	if err := os.MkdirAll(filepath.Dir(mustCookiePath(t)), 0o755); err != nil {
		t.Fatalf("cookie dir: %v", err)
	}
	if err := os.WriteFile(mustCookiePath(t), []byte("z_c0=live; d_c0=dev"), 0o600); err != nil {
		t.Fatalf("write cookie: %v", err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	it := core.Item{
		ID: "zh:question/1001", Source: "zhihu", Feed: "hot",
		Title: "问题", URL: "https://www.zhihu.com/question/1001", FetchedAt: nowStamp(),
	}
	if _, _, err := st.SaveFeedItems(context.Background(), "zhihu", "hot", []core.Item{it}); err != nil {
		t.Fatalf("seed question: %v", err)
	}
	return st
}

func mustCookiePath(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("READSCOPE_CONFIG")
	if dir == "" {
		t.Fatal("READSCOPE_CONFIG not set")
	}
	return filepath.Join(dir, "zhihu.cookies")
}

func questionItem() core.Item {
	return core.Item{ID: "zh:question/1001", Source: "zhihu", Feed: "hot", Title: "问题"}
}

// The open persists everything the question layer needs offline: the
// description into the body snapshot, the meta triple onto the
// detail_fetch row, the two answer floors into the comments cache —
// and returns the feeds cursor for Ctrl+N.
func TestDetailPersistsBodyQuestionAndFloors(t *testing.T) {
	st := questionStore(t, detailPlane(t))
	c, err := Detail(context.Background(), st, core.DefaultSources(), questionItem())
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if !strings.Contains(c.Body, "描述") || c.Question == nil || c.Question.Author != "问主" {
		t.Fatalf("content = %+q %+v", c.Body, c.Question)
	}
	if len(c.Comments) != 2 || c.Next == "" {
		t.Fatalf("floors = %d, next = %q", len(c.Comments), c.Next)
	}
	// The offline half: body snapshot, meta triple, tree.
	rows, err := st.FeedItems(context.Background(), "zhihu", "hot", false)
	if err != nil || len(rows) != 1 || !strings.Contains(rows[0].Item.Body, "描述") {
		t.Fatalf("body snapshot missing: %v %+v", err, rows)
	}
	if q, ok, _ := st.QuestionInfoForItem(context.Background(), "zh:question/1001"); !ok || q.AnswerCount != 3 {
		t.Fatalf("question info = %+v ok=%v", q, ok)
	}
	if cached, _ := st.CommentsForItem(context.Background(), "zh:question/1001"); len(cached) != 2 {
		t.Fatalf("cached tree = %+v", cached)
	}
}

// The refresh merge, end to end: after Ctrl+N loaded page two and a
// comment subtree, a re-open's background Detail must keep them —
// the cache holds five floors... three floors plus the read comments,
// not just the fresh page's two.
func TestDetailRefreshKeepsPagedFloorsAndComments(t *testing.T) {
	st := questionStore(t, detailPlane(t))
	ctx := context.Background()
	it := questionItem()

	if _, err := Detail(ctx, st, core.DefaultSources(), it); err != nil {
		t.Fatalf("first open: %v", err)
	}
	c := firstDetail(t, st, it)
	if _, _, err := DetailFloorsPage(ctx, st, core.DefaultSources(), it, c.Next); err != nil {
		t.Fatalf("Ctrl+N floors: %v", err)
	}
	if _, _, err := DetailCommentsPage(ctx, st, core.DefaultSources(), it, "9001", ""); err != nil {
		t.Fatalf("comment layer: %v", err)
	}

	// The background refresh of a re-open.
	if _, err := Detail(ctx, st, core.DefaultSources(), it); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got, _ := st.CommentsForItem(ctx, it.ID)
	var ids []string
	for _, cm := range got {
		ids = append(ids, cm.ID)
	}
	joined := strings.Join(ids, ",")
	// f1/f2 fresh from page 1 (with a1's comments), f3 paged in and kept.
	if joined != "9001,701,801,702,9002,9003" {
		t.Fatalf("refresh merge = %v", ids)
	}
}

func firstDetail(t *testing.T, st *store.Store, it core.Item) core.Content {
	t.Helper()
	c, err := Detail(context.Background(), st, core.DefaultSources(), it)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	return c
}

// The two Ctrl+N compositions: floors append (dedup against a
// re-served answer, cursor walks to the end), comments page by
// offset under their answer floor (gap-completed children riding).
func TestDetailPagingCompositions(t *testing.T) {
	st := questionStore(t, detailPlane(t))
	ctx := context.Background()
	it := questionItem()

	c := firstDetail(t, st, it)

	tree, next, err := DetailFloorsPage(ctx, st, core.DefaultSources(), it, c.Next)
	if err != nil {
		t.Fatalf("floors page: %v", err)
	}
	if len(tree) != 3 || next != "" {
		t.Fatalf("floors walk: %d floors, next=%q", len(tree), next)
	}

	tree2, nextCursor, err := DetailCommentsPage(ctx, st, core.DefaultSources(), it, "9001", "")
	if err != nil {
		t.Fatalf("comments page: %v", err)
	}
	var ids []string
	for _, cm := range tree2 {
		ids = append(ids, cm.ID)
	}
	// 9001's roots + the gap-completed child, spliced under 9001,
	// before 9002/9003.
	if strings.Join(ids, ",") != "9001,701,801,702,9002,9003" {
		t.Fatalf("comments merge = %v", ids)
	}
	if !strings.HasSuffix(nextCursor, "offset=387770585_11616175634_0") {
		t.Fatalf("nextCursor = %q, want the composite cursor", nextCursor)
	}

	// The second comments page (Ctrl+N at the reply layer's end).
	tree3, endCursor, err := DetailCommentsPage(ctx, st, core.DefaultSources(), it, "9001", nextCursor)
	if err != nil {
		t.Fatalf("comments page two: %v", err)
	}
	if endCursor != "" {
		t.Fatalf("end cursor = %q, want empty", endCursor)
	}
	if len(tree3) != len(tree2)+1 {
		t.Fatalf("page two appended nothing: %d → %d", len(tree2), len(tree3))
	}
}

// hn is not a DetailPager: the paging compositions refuse it by name
// (the UI probes the same way), and its Detail still replaces whole.
func TestPagingCompositionsRefuseNonPagingSources(t *testing.T) {
	st := detailStore(t)
	it := hnStoryItem()
	if _, _, err := DetailFloorsPage(context.Background(), st, core.DefaultSources(), it, ""); err == nil ||
		!strings.Contains(err.Error(), "pages no detail") {
		t.Fatalf("floors on hn = %v, want the named refusal", err)
	}
	if _, _, err := DetailCommentsPage(context.Background(), st, core.DefaultSources(), it, "10", ""); err == nil {
		t.Fatal("comments on hn must refuse too")
	}
}

// --- the article's detail caching (ticket 09) ------------------------------

// articlePlane serves one article's doors: the zhuanlan body door
// and the comment_v5 articles door (first page + composite-cursor
// page two + the child-gap walk) — cookie-gated like the question
// family.
func articlePlane(t *testing.T) *httptest.Server {
	t.Helper()
	gate := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Cookie") == "" {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"error":{"code":100}}`)
			return false
		}
		return true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/articles/2001", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w, r) {
			return
		}
		fmt.Fprint(w, `{"title":"专栏","author":{"name":"作者"},
		 "content":"<p>正文</p>","comment_count":3,"voteup_count":9,"created":1700000000}`)
	})
	mux.HandleFunc("/api/v4/comment_v5/articles/2001/root_comment", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w, r) {
			return
		}
		const composite = "5_11616058687_0"
		if r.URL.Query().Get("offset") == composite {
			fmt.Fprint(w, `{"data":[{"id":703,"author":{"name":"末根"},"like_count":0,"content":"末页"}],
			 "paging":{"next":"","is_end":true}}`)
			return
		}
		fmt.Fprint(w, `{"data":[
		 {"id":701,"author":{"name":"根一"},"like_count":3,"content":"根一",
		  "child_comment_count":1,"child_comments":[]},
		 {"id":702,"author":{"name":"根二"},"like_count":0,"content":"根二"}],
		 "paging":{"next":"http://`+r.Host+`/api/v4/comment_v5/articles/2001/root_comment?order_by=score&limit=20&offset=`+composite+`","is_end":false}}`)
	})
	mux.HandleFunc("/api/v4/comment_v5/comment/701/child_comment", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w, r) {
			return
		}
		fmt.Fprint(w, `{"data":[{"id":801,"author":{"name":"子回复"},"like_count":0,"content":"<p>子</p>"}],
		 "paging":{"next":"","is_end":true}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// articleStore is questionStore's twin seeding the article's
// recommend-feed row.
func articleStore(t *testing.T, srv *httptest.Server) *store.Store {
	t.Helper()
	t.Setenv("READSCOPE_CONFIG", t.TempDir())
	t.Setenv("READSCOPE_ZHIHU_BASE", srv.URL)
	if err := os.MkdirAll(filepath.Dir(mustCookiePath(t)), 0o755); err != nil {
		t.Fatalf("cookie dir: %v", err)
	}
	if err := os.WriteFile(mustCookiePath(t), []byte("z_c0=live; d_c0=dev"), 0o600); err != nil {
		t.Fatalf("write cookie: %v", err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	it := core.Item{
		ID: "zh:article/2001", Source: "zhihu", Feed: "recommend",
		Title: "专栏", URL: "https://zhuanlan.zhihu.com/p/2001", FetchedAt: nowStamp(),
	}
	if _, _, err := st.SaveFeedItems(context.Background(), "zhihu", "recommend", []core.Item{it}); err != nil {
		t.Fatalf("seed article: %v", err)
	}
	return st
}

func articleItem() core.Item {
	return core.Item{ID: "zh:article/2001", Source: "zhihu", Feed: "recommend", Title: "专栏"}
}

// The article's open persists the body snapshot and the pseudo
// floor; the comment walk splices under it; and a re-open's
// background refresh keeps every walked comment — a refresh must
// never unload the roots Ctrl+N paged in (刷新不抹缓存), and the
// body snapshot keeps the article readable offline.
func TestArticleDetailPersistsAndMergeKeepsWalkedComments(t *testing.T) {
	st := articleStore(t, articlePlane(t))
	ctx := context.Background()
	it := articleItem()

	c, err := Detail(ctx, st, core.DefaultSources(), it)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if !strings.Contains(c.Body, "正文") || len(c.Comments) != 1 || c.Comments[0].ID != "2001" {
		t.Fatalf("content = %q %+v", c.Body, c.Comments)
	}
	if c.Comments[0].NumChildren == nil || *c.Comments[0].NumChildren != 3 {
		t.Fatalf("pseudo floor count = %v, want the server's 3", c.Comments[0].NumChildren)
	}

	// The comment walk: first page (roots + the gap-completed child),
	// then the composite-cursor page.
	tree, next, err := DetailCommentsPage(ctx, st, core.DefaultSources(), it, "2001", "")
	if err != nil {
		t.Fatalf("comments page: %v", err)
	}
	var ids []string
	for _, cm := range tree {
		ids = append(ids, cm.ID)
	}
	if strings.Join(ids, ",") != "2001,701,801,702" {
		t.Fatalf("comments merge = %v", ids)
	}
	if !strings.Contains(next, "offset=5_11616058687_0") {
		t.Fatalf("next = %q, want the composite cursor", next)
	}
	if _, _, err := DetailCommentsPage(ctx, st, core.DefaultSources(), it, "2001", next); err != nil {
		t.Fatalf("comments page two: %v", err)
	}

	// The re-open's background refresh: the pseudo floor leads, every
	// walked comment survives.
	if _, err := Detail(ctx, st, core.DefaultSources(), it); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got, _ := st.CommentsForItem(ctx, it.ID)
	ids = ids[:0]
	for _, cm := range got {
		ids = append(ids, cm.ID)
	}
	if strings.Join(ids, ",") != "2001,701,801,702,703" {
		t.Fatalf("refresh merge = %v (the walked comments must survive)", ids)
	}

	// The body snapshot landed on the row: offline re-opens read it.
	rows, err := st.FeedItems(ctx, "zhihu", "recommend", false)
	if err != nil || len(rows) != 1 || !strings.Contains(rows[0].Item.Body, "正文") {
		t.Fatalf("body snapshot missing: %v %+v", err, rows)
	}
}
