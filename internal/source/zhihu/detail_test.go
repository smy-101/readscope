package zhihu

// Ticket 06 tests: the question's detail data plane — the two-request
// open (meta with x-zse-93, feeds without — the measured matrix), the
// Content mapping (cleaned description, meta triple, answer floors
// with the floor-head data, feeds cursor), the floors cursor walk,
// and the comment layer's root page + child-gap completion walk
// (dedup, is_delete, six-page bound, soft stop). All offline against
// the shared fakeZhihu; the real-network run is ticket 07's smoke.

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/core"
)

// detailSrcAt lands a cookie'd zhihu source on one fake server.
func detailSrcAt(t *testing.T, fake *fakeZhihu) (*Source, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return newSource(Hosts{API: srv.URL, Daily: srv.URL, Web: srv.URL}, "z_c0=session; d_c0=dev"), srv
}

func detailItem() core.Item {
	return core.Item{ID: ItemID(fakeQID), Source: ID, Feed: HotFeedKey, Title: "为什么终端阅读器又火了？"}
}

// detailMetaJSON carries the description through the full zhihu HTML
// gauntlet — the open must ride CleanHTML before the pipeline.
const detailMetaJSON = `{"title":"为什么终端阅读器又火了？","author":{"name":"张三"},
 "answer_count":213,"visit_count":12000000,
 "detail":"<p>问题描述</p><noscript><img src=\"https://pic.zhimg.com/80/x.jpg\"></noscript><img data-original=\"https://pic.zhimg.com/50/a.jpg\" src=\"data:image/svg+xml,e\"><img src=\"https://pic.zhimg.com/50/a.jpg\">"}`

// detailFeedsJSON is the first answers page: two floors (one with a
// zero 赞/评论 — both floor-head segments must go nil and drop), one
// stray article card, and a cursor onto page two.
const detailFeedsPage1 = `{"data":[
 {"target":{"id":1912345678901234567,"type":"answer","author":{"name":"李四"},
  "voteup_count":12345,"comment_count":541,"content":"<p>回答全文</p>","created_time":1700000000}},
 {"target":{"id":200001,"type":"article","title":"混进来的文章卡"}},
 {"target":{"id":300002,"type":"answer","author":{"name":"零赞新人"},
  "voteup_count":0,"comment_count":0,"content":"<p>零赞回答</p>","created_time":0}}
],"paging":{"next":"%s/feeds-page-2","is_end":false}}`

// detailFeedsPage2 is the second answers page, no further cursor.
const detailFeedsPage2 = `{"data":[
 {"target":{"id":400003,"type":"answer","author":{"name":"王五"},
  "voteup_count":9,"comment_count":2,"content":"<p>第二页回答</p>","created_time":1700000001}}
],"paging":{"next":"","is_end":true}}`

// detailRootsJSON is one answer's first root page: root 501 with a
// hot preview of one child (h1) and child_comment_count 3 — gap 2,
// the walk must fill c2/c3 and drop the re-served h1 duplicate and
// the deleted row; root 502 with no children at all (gap 0, no walk);
// a deleted root 503 that must not survive at all. paging.next
// carries a COMPOSITE cursor string (the 2026-12 real shape) onto
// page two.
const detailRootsJSON = `{"data":[
 {"id":501,"author":{"name":"赵七"},"like_count":231,"content":"根评论一",
  "child_comment_count":3,"child_comments":[
   {"id":901,"author":{"name":"热门子回复"},"like_count":5,"content":"<p>热门子回复</p>"}]},
 {"id":502,"author":{"name":"钱八"},"like_count":0,"content":"根评论二",
  "child_comment_count":0,"child_comments":[]},
 {"id":503,"author":{"name":"已删"},"like_count":0,"content":"","is_delete":true}
],"paging":{"next":"%ROOTSPAGE2%","is_end":false}}`

// detailRootsPage2 is the composite-cursor page: one more root, the
// walk's end.
const detailRootsPage2 = `{"data":[
 {"id":601,"author":{"name":"末页"},"like_count":1,"content":"游标页根"}
],"paging":{"next":"","is_end":true}}`

// childPage501 is root 501's completion page: the h1 duplicate, two
// new children, one deleted row, end of walk.
const childPage501 = `{"data":[
 {"id":901,"author":{"name":"dup"},"like_count":0,"content":"dup"},
 {"id":902,"author":{"name":"子回复二"},"like_count":0,"content":"<p>子回复二</p>"},
 {"id":903,"author":{"name":"子回复三"},"like_count":0,"content":"子回复三"},
 {"id":904,"author":{"name":"已删子回复"},"like_count":0,"content":"","is_delete":true}
],"paging":{"next":"","is_end":true}}`

func detailFake(t *testing.T) (*Source, *fakeZhihu, string) {
	t.Helper()
	fake := &fakeZhihu{}
	src, srv := detailSrcAt(t, fake)
	fake.questionMetaOverride = detailMetaJSON
	// The roots page's paging.next points at the composite-cursor page
	// served verbatim from extraPages (path?query addressing).
	cursorQuery := "order_by=score&limit=20&offset=387770585_11616175634_0"
	fake.rootOverride = strings.ReplaceAll(detailRootsJSON, "%ROOTSPAGE2%", srv.URL+"/api/v4/comment_v5/answers/"+fakeAID+"/root_comment?"+cursorQuery)
	fake.childPages = map[string][]string{"501": {childPage501}}
	fake.childClicks = map[string]int{}
	fake.extraPages = map[string]string{
		"/api/v4/comment_v5/answers/" + fakeAID + "/root_comment?" + cursorQuery: detailRootsPage2,
	}
	fake.feedsOverride = strings.ReplaceAll(detailFeedsPage1, "%s", srv.URL)
	fake.extraPages["/feeds-page-2"] = detailFeedsPage2
	return src, fake, srv.URL
}

// The open itself: two requests, the measured header matrix on each
// leg, and the Content mapping — cleaned description (noscript gone,
// /50/ proxy stripped, lazy placeholder dropped, duplicate image
// merged), the meta triple, the answer floors with the floor-head
// data (stray cards skipped, zero segments nil), and the cursor.
func TestDetailTwoRequestsMapTheQuestion(t *testing.T) {
	src, fake, _ := detailFake(t)
	c, err := src.Detail(t.Context(), detailItem())
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}

	// Header matrix: meta wears x-zse-93 + the question referer,
	// feeds carries neither version header nor... it does carry the
	// referer. Both ride the cookie.
	metaHit := fake.hitExact("/api/v4/questions/" + fakeQID)
	feedsHit := fake.hitExact("/api/v4/questions/" + fakeQID + "/feeds")
	if metaHit == nil || feedsHit == nil {
		t.Fatalf("both legs must dial: meta=%v feeds=%v", metaHit, feedsHit)
	}
	if !metaHit.zse93 || metaHit.zse93Val != XZse93 {
		t.Errorf("meta leg must wear x-zse-93=%s, got %q", XZse93, metaHit.zse93Val)
	}
	if feedsHit.zse93 {
		t.Error("feeds leg must NOT wear x-zse-93 (cursorless entry rejects it)")
	}
	if want := "https://www.zhihu.com/question/" + fakeQID; metaHit.referer != want || feedsHit.referer != want {
		t.Errorf("referer = %q / %q, want %q on both legs", metaHit.referer, feedsHit.referer, want)
	}
	if !metaHit.cookie || !feedsHit.cookie {
		t.Error("both legs are cookie-gated; the cookie must ride")
	}

	// Body: the cleaned description, not the raw one.
	for _, gone := range []string{"noscript", "data:image", "/50/a.jpg"} {
		if strings.Contains(c.Body, gone) {
			t.Errorf("description still carries %q:\n%s", gone, c.Body)
		}
	}
	if !strings.Contains(c.Body, "pic.zhimg.com/a.jpg") {
		t.Errorf("the merged full-size image should survive:\n%s", c.Body)
	}

	// The meta triple.
	if c.Question == nil || c.Question.Author != "张三" ||
		c.Question.AnswerCount != 213 || c.Question.VisitCount != 12000000 {
		t.Fatalf("question info = %+v", c.Question)
	}

	// Floors: two answers, the stray article card not among them.
	if len(c.Comments) != 2 {
		t.Fatalf("floors = %d, want 2 (stray cards skip):\n%+v", len(c.Comments), c.Comments)
	}
	f0 := c.Comments[0]
	if f0.ID != fakeAID || f0.Author != "李四" || f0.Text != "<p>回答全文</p>" {
		t.Errorf("floor 0 = %+v", f0)
	}
	if f0.Score == nil || *f0.Score != 12345 {
		t.Errorf("floor 0 赞 = %v, want 12345", f0.Score)
	}
	if f0.NumChildren == nil || *f0.NumChildren != 541 {
		t.Errorf("floor 0 comment count = %v, want 541", f0.NumChildren)
	}
	f1 := c.Comments[1]
	if f1.Score != nil || f1.CreatedAt != nil {
		t.Errorf("zero 赞/时间 must be nil (不占位), got %+v", f1)
	}
	// The comment count stays DATA: zero kept verbatim (the comment
	// layer's zero-request fast path reads it), the drop of the
	// zero segment is the display's rule.
	if f1.NumChildren == nil || *f1.NumChildren != 0 {
		t.Errorf("zero comment count = %v, want a verbatim 0 (data, not display)", f1.NumChildren)
	}

	// The cursor rides verbatim — Ctrl+N walks the server's own address.
	if !strings.HasSuffix(c.Next, "/feeds-page-2") {
		t.Errorf("next cursor = %q, want the page-2 address", c.Next)
	}
}

// The floors walk: page two via the cursor, verbatim address, no
// x-zse-93 (cursor pages never needed it), and the end served as
// an empty cursor.
func TestFetchFloorsPageWalksCursor(t *testing.T) {
	src, _, _ := detailFake(t)
	it := detailItem()

	floors, next, err := src.FetchFloorsPage(t.Context(), it, "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(floors) != 2 || next == "" {
		t.Fatalf("first page: %d floors, next=%q", len(floors), next)
	}
	floors2, next2, err := src.FetchFloorsPage(t.Context(), it, next)
	if err != nil {
		t.Fatalf("cursor page: %v", err)
	}
	if len(floors2) != 1 || floors2[0].ID != "400003" {
		t.Fatalf("page two floors = %+v", floors2)
	}
	if next2 != "" {
		t.Errorf("page two is the end, next = %q", next2)
	}
}

// The comment layer's first page: roots in score order with the gap
// walk filling only what is missing — the hot preview child survives
// first, the fetched duplicates and deleted rows drop, tree order
// keeps parents before children, roots' ParentID is the answer, and
// the walk reports the composite cursor from paging.next verbatim.
func TestFetchAnswerCommentsCompletesGaps(t *testing.T) {
	src, fake, srvURL := detailFake(t)
	comments, next, err := src.FetchAnswerComments(t.Context(), detailItem(), fakeAID, "")
	if err != nil {
		t.Fatalf("FetchAnswerComments: %v", err)
	}

	want := []struct {
		id, parent, author string
	}{
		{"501", fakeAID, "赵七"},
		{"901", "501", "热门子回复"},
		{"902", "501", "子回复二"},
		{"903", "501", "子回复三"},
		{"502", fakeAID, "钱八"},
	}
	if len(comments) != len(want) {
		t.Fatalf("comments = %d, want %d (deleted root and deduped/deleted rows gone):\n%+v",
			len(comments), len(want), comments)
	}
	for i, w := range want {
		if comments[i].ID != w.id || comments[i].ParentID != w.parent || comments[i].Author != w.author {
			t.Errorf("comment[%d] = {%s %s %s}, want {%s %s %s}",
				i, comments[i].ID, comments[i].ParentID, comments[i].Author, w.id, w.parent, w.author)
		}
	}
	wantNext := srvURL + "/api/v4/comment_v5/answers/" + fakeAID + "/root_comment?order_by=score&limit=20&offset=387770585_11616175634_0"
	if next != wantNext {
		t.Errorf("next cursor = %q, want paging.next verbatim %q", next, wantNext)
	}
	if n := fake.childHitCount("501"); n != 1 {
		t.Errorf("root 501's walk clicked %d child pages, want 1 (gap 2, one page)", n)
	}
	if n := fake.childHitCount("502"); n != 0 {
		t.Errorf("root 502 has no gap — the walk must not dial, clicked %d", n)
	}
}

// The walk's end: no paging.next (or is_end) reports "" — Ctrl+N has
// nowhere further to go.
func TestFetchAnswerCommentsEndIsEmptyCursor(t *testing.T) {
	endRoots := `{"data":[{"id":601,"author":{"name":"末页"},"like_count":1,"content":"末页根"}],
		"paging":{"next":"","is_end":true}}`
	_, fake, _ := detailFake(t) // separate fake: same page shapes, override below
	fake.rootOverride = endRoots
	src2, _ := detailSrcAt(t, fake)
	_, next, err := src2.FetchAnswerComments(t.Context(), detailItem(), fakeAID, "")
	if err != nil {
		t.Fatalf("end page: %v", err)
	}
	if next != "" {
		t.Errorf("end page next = %q, want \"\"", next)
	}
}

// The six-page bound: a root whose child walk never ends (the server
// keeps serving a next cursor) stops at maxChildPages — the measured
// restraint against runaway walks.
func TestChildWalkStopsAtSixPages(t *testing.T) {
	fake := &fakeZhihu{
		rootOverride: `{"data":[{"id":999,"author":{"name":"无底洞"},"like_count":0,"content":"根",
		"child_comment_count":1000,"child_comments":[]}],"paging":{"next":"","is_end":true}}`,
		childPages:  map[string][]string{},
		childClicks: map[string]int{},
	}
	src, srv := detailSrcAt(t, fake)
	loop := `{"data":[{"id":910,"author":{"name":"循环"},"like_count":0,"content":"页"}],
		"paging":{"next":"` + srv.URL + `/api/v4/comment_v5/comment/999/child_comment?limit=50&offset=50","is_end":false}}`
	fake.childPages["999"] = []string{loop}
	if _, _, err := src.FetchAnswerComments(t.Context(), detailItem(), fakeAID, ""); err != nil {
		t.Fatalf("FetchAnswerComments: %v", err)
	}
	if n := fake.childHitCount("999"); n != maxChildPages {
		t.Errorf("runaway walk clicked %d pages, want the %d-page bound", n, maxChildPages)
	}
}

// A failed child page stops the walk softly: the page's roots and
// preview children still land, the missing gap is simply not filled —
// a partial comment tree beats a failed page.
func TestChildWalkFailsSoft(t *testing.T) {
	src, fake, _ := detailFake(t)
	fake.childPages["501"] = nil // the child door 404s
	comments, _, err := src.FetchAnswerComments(t.Context(), detailItem(), fakeAID, "")
	if err != nil {
		t.Fatalf("a failed completion must not fail the page: %v", err)
	}
	var sawH1 bool
	for _, c := range comments {
		if c.ID == "901" {
			sawH1 = true
		}
	}
	if !sawH1 {
		t.Errorf("the hot preview child must survive a failed walk: %+v", comments)
	}
}

// The detail family's failure semantics on the open itself: 404 is
// ErrContentGone (the friendly 不存在或已删除 translation), 401/403 with a
// cookie is ErrSessionExpired (Ctrl+U guidance), and no cookie is
// ErrLoginRequired with zero dials.
func TestDetailFailureSemantics(t *testing.T) {
	it := detailItem()

	src, fake, _ := detailFake(t)
	fake.goneStatus = 404
	if _, err := src.Detail(t.Context(), it); !errors.Is(err, ErrContentGone) {
		t.Errorf("404 = %v, want ErrContentGone", err)
	}

	src, fake, _ = detailFake(t)
	fake.staleStatus = 403
	if _, err := src.Detail(t.Context(), it); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("403 = %v, want ErrSessionExpired", err)
	}

	noCookie := &fakeZhihu{childPages: map[string][]string{}, childClicks: map[string]int{}}
	_, srv := detailSrcAt(t, noCookie)
	anon := newSource(Hosts{Web: srv.URL}, "")
	if _, err := anon.Detail(t.Context(), it); !errors.Is(err, ErrLoginRequired) {
		t.Errorf("no cookie = %v, want ErrLoginRequired", err)
	}
	if n := noCookie.hitCount("/api/v4/"); n != 0 {
		t.Errorf("gated preflight must refuse before dialing, dialed %d times", n)
	}

	// The comment door answers 404 for a deleted answer with the same
	// translation.
	src, fake, _ = detailFake(t)
	fake.goneStatus = 404
	if _, _, err := src.FetchAnswerComments(t.Context(), it, fakeAID, ""); !errors.Is(err, ErrContentGone) {
		t.Errorf("comments 404 = %v, want ErrContentGone", err)
	}
}

// The 2026-12 real-network cursor contract (07 号票 smoke 偏差回补):
// the first page must NOT carry an offset parameter — the real API
// answers a literal digits-only offset (offset=0 included) with HTTP
// 200 and a zombie empty page, uniformly, on every answer. And the
// next page rides paging.next VERBATIM: the real cursor is a
// composite string ("387770585_11616175634_0"), never an integer —
// the client must not parse it, only carry it.
func TestFetchAnswerCommentsRealCursorContract(t *testing.T) {
	src, fake, srvURL := detailFake(t)

	comments, next, err := src.FetchAnswerComments(t.Context(), detailItem(), fakeAID, "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(comments) == 0 {
		t.Fatal("first page came back empty — the zombie-page quirk: the request must omit the offset parameter")
	}
	hit := fake.hitExact("/api/v4/comment_v5/answers/" + fakeAID + "/root_comment")
	if hit == nil {
		t.Fatal("the root page never dialed")
	}
	if m := offsetParamRe.FindStringSubmatch(hit.query); m != nil && m[1] != "" {
		t.Errorf("first page carried offset=%q — digits-only offsets eat the zombie empty page", m[1])
	}
	wantNext := srvURL + "/api/v4/comment_v5/answers/" + fakeAID + "/root_comment?order_by=score&limit=20&offset=387770585_11616175634_0"
	if next != wantNext {
		t.Fatalf("next = %q, want the composite cursor verbatim", next)
	}

	// Following the cursor: page two serves through the same door, the
	// composite offset riding the request verbatim, and the walk ends
	// with an empty cursor.
	page2, next2, err := src.FetchAnswerComments(t.Context(), detailItem(), fakeAID, next)
	if err != nil {
		t.Fatalf("cursor page: %v", err)
	}
	if len(page2) != 1 || page2[0].ID != "601" || page2[0].Author != "末页" {
		t.Fatalf("page two = %+v, want the composite-cursor page's root", page2)
	}
	if next2 != "" {
		t.Errorf("page two is the walk's end, next = %q", next2)
	}
	// The cursor page's request must carry the composite offset
	// VERBATIM — the server's own spelling, never re-encoded.
	var cursorHit *request
	fake.mu.Lock()
	for i := range fake.hits {
		h := &fake.hits[i]
		if h.path == "/api/v4/comment_v5/answers/"+fakeAID+"/root_comment" &&
			strings.Contains(h.query, "offset=387770585_11616175634_0") {
			cursorHit = h
		}
	}
	fake.mu.Unlock()
	if cursorHit == nil {
		t.Error("the cursor page never dialed with the composite offset verbatim")
	}
}

// The pure halves: gap arithmetic (a deleted root owes nothing) and
// the dedup filter (deleted rows drop, seen ids drop, survivors mark
// seen for the next page).
func TestChildGapArithmetic(t *testing.T) {
	root := commentNode{ID: "1", ChildCommentCount: 3, ChildComments: make([]commentNode, 1)}
	if g := childGap(root); g != 2 {
		t.Errorf("gap = %d, want 2", g)
	}
	deleted := commentNode{ID: "2", IsDelete: true, ChildCommentCount: 9}
	if g := childGap(deleted); g != 0 {
		t.Errorf("deleted root gap = %d, want 0", g)
	}
	if g := childGap(commentNode{ID: "3"}); g != 0 {
		t.Errorf("childless root gap = %d, want 0", g)
	}
}

func TestDedupChildrenFiltersAndMarks(t *testing.T) {
	seen := map[string]bool{"901": true}
	nodes := []commentNode{
		{ID: "901"},                 // the re-served hot preview
		{ID: "902"},                 // new
		{ID: "903", IsDelete: true}, // deleted
		{ID: "902"},                 // same page duplicate
	}
	got := dedupChildren(nodes, seen)
	if len(got) != 1 || got[0].ID != "902" {
		t.Fatalf("dedup = %+v, want just 902", got)
	}
	if !seen["902"] {
		t.Error("the survivor must be marked seen for the next page")
	}
}

// AnswerURL anchors the question page at the answer; CommentURL maps
// the item id to the question page and leaves bare comment ids to
// AnswerURL's routing (they cannot pair with a question alone).
func TestAnswerAndCommentURL(t *testing.T) {
	src, _, _ := detailFake(t)
	it := detailItem()
	if got := src.AnswerURL(it, fakeAID); got != "https://www.zhihu.com/question/"+fakeQID+"/answer/"+fakeAID {
		t.Errorf("AnswerURL = %q", got)
	}
	if got := src.CommentURL(it.ID); got != "https://www.zhihu.com/question/"+fakeQID {
		t.Errorf("CommentURL(item) = %q", got)
	}
	if got := src.CommentURL(fakeAID); got != "" {
		t.Errorf("CommentURL(bare id) = %q, want \"\"", got)
	}
}

// --- the article's two-layer detail (ticket 09) --------------------------

// fakeArticleID mirrors the real network's shape: past 2^53, served
// as a string by the zhuanlan door (json.Number digests both).
const fakeArticleID = "2098551208264914833"

func articleItem() core.Item {
	return core.Item{ID: ArticleItemID(fakeArticleID), Source: ID, Feed: RecommendFeedKey,
		Title: "《HelloGitHub》第 127 期", Author: "HelloGitHub"}
}

// articleDetailJSON is the zhuanlan door's payload: content through
// the HTML gauntlet (noscript copy, /80/ proxy, lazy placeholder),
// the counts, both clocks.
const articleDetailJSON = `{"title":"《HelloGitHub》第 127 期","author":{"name":"HelloGitHub"},
 "content":"<p>正文<b>加粗</b></p><noscript><img src=\"https://pic.zhimg.com/80/x.jpg\"></noscript><img data-original=\"https://pic.zhimg.com/50/a.jpg\" src=\"data:image/svg+xml,e\">",
 "comment_count":88,"voteup_count":1024,"created":1710000000,"updated":1710000100}`

// articleRootsJSON: two roots under the article, one hot child on
// the first — the same comment_v5 shape the answers door serves.
const articleRootsJSON = `{"data":[
 {"id":701,"author":{"name":"评论甲"},"like_count":3,"content":"文章根评论",
  "child_comment_count":1,"child_comments":[
   {"id":801,"author":{"name":"子乙"},"like_count":0,"content":"<p>子回复</p>"}]},
 {"id":702,"author":{"name":"评论丙"},"like_count":0,"content":"根二","child_comment_count":0}
],"paging":{"next":"","is_end":true}}`

// articleFake lands a cookie'd source with the article family's
// fixtures; its own server so the zhuanlan root is wired without
// touching the question fixtures' helper.
func articleFake(t *testing.T) (*Source, *fakeZhihu) {
	t.Helper()
	fake := &fakeZhihu{
		articleMetaOverride: articleDetailJSON,
		articleRootOverride: articleRootsJSON,
	}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return newSource(Hosts{API: srv.URL, Daily: srv.URL, Web: srv.URL, Zhuanlan: srv.URL}, cookieText), fake
}

// The article open: one GET on the zhuanlan door (cookie + the
// article page Referer, NO version header), the body cleaned through
// the same gauntlet, and the tree exactly one pseudo floor — the
// article itself — carrying the server's comment count (the comment
// layer's zero-request fast path) and the voteup/author data every
// floor head reads.
func TestArticleDetailMapsBodyAndPseudoFloor(t *testing.T) {
	src, fake := articleFake(t)
	it := articleItem()

	if !src.DetailOnDemand(it) {
		t.Fatal("DetailOnDemand(article) = false, want the two-layer detail")
	}
	c, err := src.Detail(t.Context(), it)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	h := fake.hitExact("/api/articles/" + fakeArticleID)
	if h == nil {
		t.Fatal("the zhuanlan article door never dialed")
	}
	if !h.cookie || h.zse93 || h.referer != "https://zhuanlan.zhihu.com/p/"+fakeArticleID {
		t.Errorf("article door fingerprint: cookie/no-zse93/referer contract broken: %+v", h)
	}
	if strings.Contains(c.Body, "noscript") || strings.Contains(c.Body, "/80/") || strings.Contains(c.Body, "data:image") {
		t.Errorf("body not cleaned: %q", c.Body)
	}
	if !strings.Contains(c.Body, "加粗") {
		t.Errorf("body lost real content: %q", c.Body)
	}
	if len(c.Comments) != 1 {
		t.Fatalf("tree = %d floors, want exactly the one pseudo floor", len(c.Comments))
	}
	f := c.Comments[0]
	if f.ID != fakeArticleID || f.Author != "HelloGitHub" {
		t.Errorf("pseudo floor = {%s %s}, want the article's own id/author", f.ID, f.Author)
	}
	if f.NumChildren == nil || *f.NumChildren != 88 {
		t.Errorf("pseudo floor NumChildren = %v, want the server's comment_count (88)", f.NumChildren)
	}
	if f.Score == nil || *f.Score != 1024 {
		t.Errorf("pseudo floor Score = %v, want the voteup (1024)", f.Score)
	}
	if c.Next != "" {
		t.Errorf("article detail pages nothing, Next = %q", c.Next)
	}
}

// The article failure semantics ride the detail family's: stale
// z_c0 → re-login guidance, 404 → gone, a well-200 empty content →
// the redesign alarm, anonymous → ErrLoginRequired without dialing.
func TestArticleDetailFailureSemantics(t *testing.T) {
	it := articleItem()

	src, fake := articleFake(t)
	fake.staleStatus = 403
	if _, err := src.Detail(t.Context(), it); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("403 = %v, want ErrSessionExpired", err)
	}

	src, fake = articleFake(t)
	fake.goneStatus = 404
	if _, err := src.Detail(t.Context(), it); !errors.Is(err, ErrContentGone) {
		t.Errorf("404 = %v, want ErrContentGone", err)
	}

	src, fake = articleFake(t)
	fake.articleMetaOverride = `{"title":"空","content":""}`
	_, err := src.Detail(t.Context(), it)
	if err == nil || !strings.Contains(err.Error(), "站点可能改版") {
		t.Errorf("empty content = %v, want the redesign alarm", err)
	}

	anonFake := &fakeZhihu{}
	srv := httptest.NewServer(anonFake)
	t.Cleanup(srv.Close)
	anon := newSource(Hosts{Zhuanlan: srv.URL}, "")
	if _, err := anon.Detail(t.Context(), it); !errors.Is(err, ErrLoginRequired) {
		t.Errorf("no cookie = %v, want ErrLoginRequired", err)
	}
	if n := anonFake.hitCount("/api/articles/"); n != 0 {
		t.Errorf("gated preflight must refuse before dialing, dialed %d times", n)
	}
}

// The article's comment layer rides the same walk as the answers':
// the articles root_comment door, the first page WITHOUT an offset
// parameter (the zombie-page contract), roots parented to the
// article's pseudo floor, and a floor id that is not the article's
// own refused loudly (a wiring bug must not silently fetch).
func TestArticleCommentsRideTheAnswerWalk(t *testing.T) {
	src, fake := articleFake(t)
	it := articleItem()

	comments, next, err := src.FetchAnswerComments(t.Context(), it, fakeArticleID, "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	h := fake.hitExact("/api/v4/comment_v5/articles/" + fakeArticleID + "/root_comment")
	if h == nil {
		t.Fatal("the articles comment door never dialed")
	}
	if m := offsetParamRe.FindStringSubmatch(h.query); m != nil && m[1] != "" {
		t.Errorf("first page carried offset=%q — the zombie-page contract", m[1])
	}
	if next != "" {
		t.Errorf("fixture page is the end, next = %q", next)
	}
	// Tree shape: both roots parented to the article's pseudo floor,
	// the hot child parented to its root — tree order.
	if len(comments) != 3 {
		t.Fatalf("tree = %d nodes, want 2 roots + 1 child", len(comments))
	}
	if comments[0].ID != "701" || comments[0].ParentID != fakeArticleID {
		t.Errorf("root 701 = {%s parent=%s}, want parented to the article floor", comments[0].ID, comments[0].ParentID)
	}
	if comments[1].ID != "801" || comments[1].ParentID != "701" {
		t.Errorf("child 801 = {%s parent=%s}, want parented to its root", comments[1].ID, comments[1].ParentID)
	}
	if comments[2].ID != "702" || comments[2].ParentID != fakeArticleID {
		t.Errorf("root 702 = {%s parent=%s}, want parented to the article floor", comments[2].ID, comments[2].ParentID)
	}

	// A floor id that is not the article's own: a wiring bug, refused
	// by name, nothing dialed.
	before := fake.hitCount("/api/v4/comment_v5/articles/")
	if _, _, err := src.FetchAnswerComments(t.Context(), it, "999", ""); err == nil {
		t.Error("mismatched floor id accepted — the walk must refuse it by name")
	}
	if n := fake.hitCount("/api/v4/comment_v5/articles/"); n != before {
		t.Errorf("mismatched id dialed anyway: %d → %d hits", before, n)
	}
}

// The article's Ctrl+O/Ctrl+Y targets: the column page everywhere
// (body, comment layers) — a bare comment id addresses no article
// page on its own.
func TestArticleURLs(t *testing.T) {
	src, _ := articleFake(t)
	it := articleItem()
	want := "https://zhuanlan.zhihu.com/p/" + fakeArticleID
	if got := src.AnswerURL(it, fakeArticleID); got != want {
		t.Errorf("AnswerURL = %q, want the column page", got)
	}
	if got := src.CommentURL(it.ID); got != want {
		t.Errorf("CommentURL = %q, want the column page", got)
	}
}
