package zhihu

// Ticket 08 tests: the recommend feed (GLOSSARY: 推荐流) — the
// sidebar shape (order 热榜→推荐→关注→日报, the spec's revision-2
// decision), the cursor walk against a fake recommend door
// (question-card filtering with the article card dropped until
// ticket 09), the verbatim cursor, and the cookie gate. All offline;
// the real-network verdicts live in samples/ (probe 2026-11-12:
// GREEN, cards field-compatible with moments).

import (
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/core"
)

// TestFeedsOrderHotRecommendFollowDaily pins the sidebar order the
// spec fixes (热榜→推荐→关注→日报) and the recommend feed's shape:
// key recommend, its own i18n label, the desktop home-page door.
func TestFeedsOrderHotRecommendFollowDaily(t *testing.T) {
	src := newSource(Hosts{
		API: "https://api.zhihu.com", Daily: "https://news-at.zhihu.com", Web: "https://www.zhihu.com",
	}, "")
	var keys []string
	for _, f := range src.Feeds() {
		keys = append(keys, f.Key)
	}
	if want := []string{"hot", "recommend", "follow", "daily"}; !slices.Equal(keys, want) {
		t.Fatalf("feed order = %v, want %v", keys, want)
	}
	for _, f := range src.Feeds() {
		if f.Key != RecommendFeedKey {
			continue
		}
		if f.LabelKey != "zhihu.recommend" {
			t.Errorf("recommend labelKey = %q, want zhihu.recommend", f.LabelKey)
		}
		if !strings.HasSuffix(f.URL, "/api/v3/feed/topstory/recommend?desktop=true") {
			t.Errorf("recommend url = %q, want the desktop home-page door", f.URL)
		}
	}
}

// recommendSrcAt lands a logged-in Source against the fake zhihu and
// returns its recommend feed plus the fake's base URL — the
// recommend twin of followSrcAt (cursor pages need the fake's own
// address).
func recommendSrcAt(t *testing.T, fake *fakeZhihu) (*Source, core.Feed, string) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	src := newSource(Hosts{API: srv.URL, Daily: srv.URL, Web: srv.URL}, cookieText)
	for _, f := range src.Feeds() {
		if f.Key == RecommendFeedKey {
			return src, f, srv.URL
		}
	}
	t.Fatal("no recommend feed in Feeds()")
	return nil, core.Feed{}, ""
}

// TestRecommendCursorPageKeepsQuestionAndArticleCards: the walk keeps
// answer cards (folded into their question) and question cards, keeps
// the article card as its own row (ticket 09), drops pin/video/ad,
// and dedupes the same question within a page.
func TestRecommendCursorPageKeepsQuestionAndArticleCards(t *testing.T) {
	body := `{"data":[
	 {"target":{"type":"answer","id":1912345678901234568,"author":{"name":"赵六"},
	  "question":{"id":18984420158198241698,"title":"推荐的问题"}}},
	 {"target":{"type":"question","id":18984420158198241698,"title":"同题重复卡","author":{"name":"重复"}}},
	 {"target":{"type":"article","id":2085374500649096302,"title":"推荐流的文章","author":{"name":"专栏作者"},
	  "created":1731500000,"updated":1731500500}},
	 {"target":{"type":"pin","id":300010}},
	 {"target":{"type":"video","id":400011}},
	 {"target":{"type":"ad","id":500012,"title":"广告卡不进列表"}}
	],"paging":{"next":"https://www.zhihu.com/api/v3/feed/topstory/recommend?after_id=5&desktop=true","is_end":false}}`
	src, f, _ := recommendSrcAt(t, &fakeZhihu{t: t, recommendOverride: body})

	items, next, err := src.FetchCursorPage(t.Context(), f, "")
	if err != nil {
		t.Fatalf("FetchCursorPage: %v", err)
	}
	if len(items) != 2 { // the answer folds into its question; the duplicate question card merges; the article keeps its own row
		t.Fatalf("got %d items, want 2 (answer folds; same-question dedupe; article row; pin/video/ad drop)", len(items))
	}
	it := items[0]
	if it.ID != ItemID("18984420158198241698") || it.Feed != RecommendFeedKey || it.Title != "推荐的问题" {
		t.Errorf("row = {%s %s %q}, want the folded question-card row on the recommend feed", it.ID, it.Feed, it.Title)
	}
	if it.Author != "赵六" {
		t.Errorf("author = %q, want the answerer (answer cards fold with the answerer on the author column)", it.Author)
	}
	if it.URL != questionURL("18984420158198241698") {
		t.Errorf("url = %q, want the canonical question page", it.URL)
	}
	// The article card's own row (ticket 09): same mapping both cursor
	// feeds share — zh:article id, column page URL, the card's author.
	art := items[1]
	if art.ID != "zh:article/2085374500649096302" || art.Feed != RecommendFeedKey {
		t.Errorf("article row = {%s %s}, want the zh:article row on the recommend feed", art.ID, art.Feed)
	}
	if art.Title != "推荐流的文章" || art.Author != "专栏作者" {
		t.Errorf("article title/author = %q/%q, want the card's own", art.Title, art.Author)
	}
	if art.URL != "https://zhuanlan.zhihu.com/p/2085374500649096302" {
		t.Errorf("article url = %q, want the column page", art.URL)
	}
	if next != "https://www.zhihu.com/api/v3/feed/topstory/recommend?after_id=5&desktop=true" {
		t.Errorf("next cursor = %q, want paging.next verbatim", next)
	}
}

// TestRecommendArticleCardClockCreatedFallback: the article clock
// prefers updated but falls back to created alone (probe 2026-11:
// article cards carry created/updated, no _time suffix) — and a
// card with neither clock orders by the fetch's clock (FetchedAt).
func TestRecommendArticleCardClockCreatedFallback(t *testing.T) {
	body := `{"data":[
	 {"target":{"type":"article","id":111,"title":"只有 created","author":{"name":"甲"},"created":1731400000}},
	 {"target":{"type":"article","id":222,"title":"没有时钟","author":{"name":"乙"}}}
	],"paging":{"is_end":true}}`
	src, f, _ := recommendSrcAt(t, &fakeZhihu{t: t, recommendOverride: body})

	items, _, err := src.FetchCursorPage(t.Context(), f, "")
	if err != nil {
		t.Fatalf("FetchCursorPage: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 article rows", len(items))
	}
	if items[0].PublishedAt == nil || items[0].PublishedAt.Unix() != 1731400000 {
		t.Errorf("created-only card publishedAt = %v, want its created clock", items[0].PublishedAt)
	}
	if items[1].PublishedAt != nil {
		t.Errorf("clock-less card publishedAt = %v, want nil (the fetch clock orders it, via FetchedAt)", items[1].PublishedAt)
	}
}

// TestRecommendCursorRidesVerbatimWithHomePageFingerprint: page two
// rides the server's own address wearing the same recommend-door
// fingerprint (home-page Referer, cookie).
func TestRecommendCursorRidesVerbatimWithHomePageFingerprint(t *testing.T) {
	fake := &fakeZhihu{t: t, recommendOverride: recommendJSON}
	src, f, base := recommendSrcAt(t, fake)
	cursor := base + "/api/v3/feed/topstory/recommend?after_id=5&desktop=true"
	fake.extraPages = map[string]string{"/api/v3/feed/topstory/recommend?after_id=5&desktop=true": recommendJSON}
	if _, _, err := src.FetchCursorPage(t.Context(), f, cursor); err != nil {
		t.Fatalf("FetchCursorPage: %v", err)
	}
	hits := fake.hitCount("/api/v3/feed/topstory/recommend")
	if hits < 1 {
		t.Fatal("recommend door never hit")
	}
	h := fake.hitExact("/api/v3/feed/topstory/recommend")
	if h == nil || h.referer != "https://www.zhihu.com/" || !h.cookie {
		t.Errorf("cursor page fingerprint: referer(home)/cookie contract broken: %+v", h)
	}
}

// TestRecommendWithoutCookieIsLoginRequiredZeroDial: no cookie → the
// gated preflight answers ErrLoginRequired and never dials (the
// Ctrl+U guidance; hot/daily keep working).
func TestRecommendWithoutCookieIsLoginRequiredZeroDial(t *testing.T) {
	fake := &fakeZhihu{t: t, recommendOverride: recommendJSON}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	src := newSource(Hosts{API: srv.URL, Daily: srv.URL, Web: srv.URL}, "")

	items, next, err := src.FetchCursorPage(t.Context(), core.Feed{Key: RecommendFeedKey}, "")
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("err = %v, want ErrLoginRequired (the Ctrl+U guidance)", err)
	}
	if items != nil || next != "" {
		t.Errorf("leak on refusal: %d items, next %q", len(items), next)
	}
	if n := fake.hitCount("/api/v3/feed/topstory/recommend"); n != 0 {
		t.Fatalf("recommend dialed %d times without a cookie — the preflight must refuse first", n)
	}
}

// TestFetchRefusesTheRecommendFeed: Fetch(recommend) is a loud
// refusal — the cursor path is the only path (the follow feed's
// contract, ticket 05).
func TestFetchRefusesTheRecommendFeed(t *testing.T) {
	src, _, _ := recommendSrcAt(t, &fakeZhihu{t: t})
	_, err := src.Fetch(t.Context(), core.Feed{Key: RecommendFeedKey})
	if err == nil || !strings.Contains(err.Error(), "FetchCursorPage") {
		t.Fatalf("want the loud cursor-path refusal, got: %v", err)
	}
	if _, _, err := src.FetchCursorPage(t.Context(), core.Feed{Key: HotFeedKey}, ""); err == nil ||
		!strings.Contains(err.Error(), "hot") {
		t.Fatalf("cursor-fetching the board should error naming it, got: %v", err)
	}
}
