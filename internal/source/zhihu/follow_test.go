package zhihu

// Ticket 05 tests: the follow feed (GLOSSARY: 关注流) as the cursor
// variant of PagedSource — heterogeneous moments cards filtered down
// to question cards (answer cards folded into their question with the
// answerer in the author column, everything else dropped), pages
// addressed by the server's opaque paging.next cursor, the follow
// Referer + cookie fingerprint, and the gated preflight refusing
// without a session. The cursor walk's persistence semantics (advance
// on hit, hold at the server's end, restart resume) live in
// internal/refresh (TestFollowFeedCursorWalk).

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
)

// followCardsJSON is one deliberately messy moments page: an answer
// card (question id past 2^53 — the id must survive exact through
// json.Number), the same question again as a direct question card
// (later in the page, so the answer card's author wins the row), an
// unrelated question card, and the card kinds the spec drops
// (article/pin/zvideo/feed_group). paging.next addresses page two.
const followCardsJSON = `{"data":[
 {"target":{"type":"answer","id":1912345678901234567,"author":{"name":"李四"},
  "updated_time":1731400000,
  "question":{"id":18984420158198241699,"title":"关注的问题"}}},
 {"target":{"type":"question","id":18984420158198241699,"title":"关注的问题",
  "author":{"name":"王五"},"created_time":1731300000}},
 {"target":{"type":"question","id":100001,"title":"关注的新提问","author":{"name":"赵六"},
  "created_time":1731200000}},
 {"target":{"type":"article","id":200002,"title":"被丢弃的文章","author":{"name":"作者"}}},
 {"target":{"type":"pin","id":300003}},
 {"target":{"type":"zvideo","id":400004,"title":"被丢弃的视频"}},
 {"target":{"type":"feed_group","id":500005}},
 {"target":{"type":"ad","id":600006,"title":"被丢弃的广告卡"}}
],"paging":{"next":"https://www.zhihu.com/api/v3/moments?after_id=1912345678901234567","is_end":false}}`

// followSrcAt lands a logged-in Source against the fake zhihu; the
// caller keeps the fake for fingerprint asserts (it owns the recorded
// hits) and may compute cursor URLs from srv.URL.
func followSrcAt(t *testing.T, fake *fakeZhihu) (*Source, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return newSource(Hosts{API: srv.URL, Daily: srv.URL, Web: srv.URL}, "z_c0=session; d_c0=dev"), srv
}

func followFeed(src *Source) core.Feed {
	for _, f := range src.Feeds() {
		if f.Key == FollowFeedKey {
			return f
		}
	}
	panic("no follow feed")
}

func TestFeedsAreHotThenFollow(t *testing.T) { // order itself pinned in recommend_test (ticket 08)
	fake := &fakeZhihu{}
	src, _ := followSrcAt(t, fake) // server unused: Feeds dials nothing
	feeds := src.Feeds()
	if len(feeds) != 4 {
		t.Fatalf("got %d feeds, want 4 (hot, recommend, follow, daily — ticket 08)", len(feeds))
	}
	hot, follow := feeds[0], feeds[2]
	if hot.Key != HotFeedKey || hot.ListSort != core.ListSortPoints {
		t.Errorf("hot feed = %+v", hot)
	}
	if follow.Key != FollowFeedKey {
		t.Errorf("follow key = %q, want %q", follow.Key, FollowFeedKey)
	}
	if follow.LabelKey != "zhihu.follow" {
		t.Errorf("follow labelKey = %q, want zhihu.follow", follow.LabelKey)
	}
	if !strings.HasSuffix(follow.URL, "/api/v3/moments?limit=10&desktop=true") {
		t.Errorf("follow url = %q, want the moments first page", follow.URL)
	}
	if follow.ListSort != core.ListSortTime {
		t.Errorf("follow ListSort = %q, want time (the stream is chronological)", follow.ListSort)
	}
	// The seam: follow pages by cursor, the board does not page at all.
	if !src.CursorPaged(follow) {
		t.Error("CursorPaged(follow) = false, want true")
	}
	if src.CursorPaged(hot) {
		t.Error("CursorPaged(hot) = true, want false (fixed window)")
	}
}

func TestFetchCursorPageFiltersToQuestionCards(t *testing.T) {
	src, _ := followSrcAt(t, &fakeZhihu{momentsOverride: followCardsJSON})
	items, next, err := src.FetchCursorPage(t.Context(), followFeed(src), "")
	if err != nil {
		t.Fatalf("FetchCursorPage: %v", err)
	}
	if len(items) != 2 { // the answer card and its duplicate question card are one row
		t.Fatalf("got %d items, want 2 (answer folds into its question; article/pin/zvideo/feed_group drop)", len(items))
	}

	top := items[0]
	// The answer card owns the row (first card wins): qid = its
	// question's, exact through json.Number past 2^53, and the author
	// column carries the answerer, not the asker.
	if top.ID != "zh:question/18984420158198241699" {
		t.Errorf("id = %q, want the answer card's question id, digit-exact", top.ID)
	}
	if top.Author != "李四" {
		t.Errorf("author = %q, want the answerer (the answer card was first)", top.Author)
	}
	if top.Title != "关注的问题" || top.Source != ID || top.Feed != FollowFeedKey {
		t.Errorf("row mapping = %+v", top)
	}
	if top.URL != "https://www.zhihu.com/question/18984420158198241699" {
		t.Errorf("url = %q, want the canonical question page", top.URL)
	}
	// The card's own clock orders the chronological stream — updated
	// (answer re-surfaced) preferred over created.
	if top.PublishedAt == nil || top.PublishedAt.Unix() != 1731400000 {
		t.Errorf("publishedAt = %v, want the answer card's updated_time", top.PublishedAt)
	}

	asked := items[1]
	if asked.ID != "zh:question/100001" || asked.Author != "赵六" {
		t.Errorf("question card mapping = %+v", asked)
	}
	if asked.PublishedAt == nil || asked.PublishedAt.Unix() != 1731200000 {
		t.Errorf("question card publishedAt = %v, want its created_time fallback", asked.PublishedAt)
	}
	// The walk continues at the server's own address for page two.
	if next != "https://www.zhihu.com/api/v3/moments?after_id=1912345678901234567" {
		t.Errorf("next cursor = %q, want paging.next verbatim", next)
	}
}

// A page whose cards all drop (a pin streak) is NOT the walk's end:
// zero rows, but the served cursor still advances the walk.
func TestFetchCursorPageFilteredEmptyStillServesCursor(t *testing.T) {
	body := `{"data":[
	 {"target":{"type":"pin","id":1}},{"target":{"type":"article","id":2,"title":"文"}}
	],"paging":{"next":"https://www.zhihu.com/api/v3/moments?after_id=2","is_end":false}}`
	src, _ := followSrcAt(t, &fakeZhihu{momentsOverride: body})
	items, next, err := src.FetchCursorPage(t.Context(), followFeed(src), "")
	if err != nil {
		t.Fatalf("FetchCursorPage: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("got %d items, want 0 (nothing survives the filter)", len(items))
	}
	if next == "" {
		t.Fatal("next cursor must still be served — an empty filtered page is not the end")
	}
}

// The server's end: no next cursor served, is_end true. The source
// reports it as next == ""; whether items landed is the caller's
// business.
func TestFetchCursorPageServerEndHasEmptyNext(t *testing.T) {
	body := `{"data":[
	 {"target":{"type":"question","id":777,"title":"最后一页的提问"}}
	],"paging":{"next":"","is_end":true}}`
	src, _ := followSrcAt(t, &fakeZhihu{momentsOverride: body})
	items, next, err := src.FetchCursorPage(t.Context(), followFeed(src), "")
	if err != nil {
		t.Fatalf("FetchCursorPage: %v", err)
	}
	if len(items) != 1 || next != "" {
		t.Fatalf("end page = (%d items, next %q), want the row and no cursor", len(items), next)
	}
}

// The cursor rides verbatim — the server, not the client, spells its
// page addresses — wearing the same follow-page fingerprint as page
// one.
func TestFetchCursorPageGetsCursorVerbatimWithFollowFingerprint(t *testing.T) {
	fake := &fakeZhihu{momentsOverride: followCardsJSON}
	src, srv := followSrcAt(t, fake)
	cursor := srv.URL + "/api/v3/moments?after_id=1912345678901234567"
	if _, _, err := src.FetchCursorPage(t.Context(), followFeed(src), cursor); err != nil {
		t.Fatalf("FetchCursorPage: %v", err)
	}
	hit := fake.hit("/api/v3/moments")
	if hit == nil {
		t.Fatal("moments never hit")
	}
	if !strings.Contains(hit.query, "after_id=1912345678901234567") {
		t.Errorf("query = %q, want the cursor page's own address", hit.query)
	}
	if hit.referer != "https://www.zhihu.com/follow" {
		t.Errorf("Referer = %q, want the follow page", hit.referer)
	}
	if !hit.cookie {
		t.Error("the cookie must ride the moments request")
	}
	if hit.ua != DesktopUA || hit.xrw != "fetch" {
		t.Errorf("fingerprint drifted: ua=%q xrw=%q", hit.ua, hit.xrw)
	}
	if hit.zse93 {
		t.Error("moments must not wear the version header (list family)")
	}
}

// The gated preflight (ticket 04's semantics): without a cookie the
// follow feed answers ErrLoginRequired and never dials — the UI turns
// that into the Ctrl+U guidance, and the hot board keeps working.
func TestFetchCursorPageWithoutCookieIsLoginRequiredZeroDial(t *testing.T) {
	fake := &fakeZhihu{momentsOverride: followCardsJSON}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	src := newSource(Hosts{API: srv.URL, Daily: srv.URL, Web: srv.URL}, "")
	items, next, err := src.FetchCursorPage(t.Context(), core.Feed{Key: FollowFeedKey}, "")
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("err = %v, want ErrLoginRequired (the Ctrl+U guidance)", err)
	}
	if items != nil || next != "" {
		t.Errorf("leak on refusal: %d items, next %q", len(items), next)
	}
	if n := fake.hitCount("/api/v3/moments"); n != 0 {
		t.Fatalf("moments dialed %d times without a cookie — the preflight must refuse first", n)
	}
	// The anonymous board is untouched by the missing session.
	if _, err := src.Fetch(t.Context(), core.Feed{Key: HotFeedKey}); err != nil {
		t.Fatalf("hot board must not need the session: %v", err)
	}
}

// Fetch must refuse the follow feed loudly: its pages carry the
// cursor the caller has to persist, which an items-only Fetch would
// silently drop — landing there is a wiring bug.
func TestFetchRefusesTheFollowFeed(t *testing.T) {
	src, _ := followSrcAt(t, &fakeZhihu{})
	_, err := src.Fetch(t.Context(), core.Feed{Key: FollowFeedKey})
	if err == nil || !strings.Contains(err.Error(), "FetchCursorPage") {
		t.Fatalf("want the loud cursor-path refusal, got: %v", err)
	}
	_, _, err = src.FetchCursorPage(t.Context(), core.Feed{Key: HotFeedKey}, "")
	if err == nil || !strings.Contains(err.Error(), "hot") {
		t.Fatalf("cursor-fetching the board should error naming it, got: %v", err)
	}
}

// The compile-time pin, restated at run time: zhihu carries the
// cursor variant, and only that — the plain PagedSource probe must
// keep reporting "no pages" so Ctrl+N on the board says 没有更多了.
func TestSourceIsCursorPagedOnly(t *testing.T) {
	src, _ := followSrcAt(t, &fakeZhihu{})
	var asAny any = src
	if _, ok := asAny.(source.PagedSource); ok {
		t.Fatal("zhihu must not implement PagedSource — the board is a fixed window")
	}
	cps, ok := asAny.(source.CursorPagedSource)
	if !ok || !cps.CursorPaged(core.Feed{Key: FollowFeedKey}) {
		t.Fatal("zhihu must implement CursorPagedSource for the follow feed")
	}
}
