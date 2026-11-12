package zhihu

// Ticket 02 tests: the zhihu source against the same fake zhihu front
// the probe tests ride (probe_test.go) — registration, the fixed hot
// feed, row→question-card mapping with url-extracted ids past 2^53,
// the heat fold, the request fingerprint, and error naming. The
// end-to-end refresh tracer lives in internal/refresh
// (TestRefreshZhihuTracerBullet).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
)

// hotRowsJSON is a board slice: a full question card (qid past 2^53 in
// target.id AND target.url — the id must come out of the URL exact),
// a pinned card with no heat column, a video card, and a title-less
// question. Only the two question cards with titles survive.
const hotRowsJSON = `{"data":[
 {"detail_text":"2045 万热度","target":{"id":18984420158198241699,"type":"question",
  "title":"为什么终端阅读器又火了？","url":"https://api.zhihu.com/questions/18984420158198241699"}},
 {"detail_text":"","target":{"id":100001,"type":"question","title":"置顶卡没有热度列",
  "url":"https://api.zhihu.com/questions/100001"}},
 {"detail_text":"12 万热度","target":{"id":200002,"type":"zvideo","title":"热榜上的视频卡",
  "url":"https://www.zhihu.com/zvideo/200002"}},
 {"detail_text":"12 万热度","target":{"id":300003,"type":"question","title":"",
  "url":"https://api.zhihu.com/questions/300003"}}
]}`

// zhihuSrcAt lands a Source against a fake zhihu (one base for all
// three hosts, the way READSCOPE_ZHIHU_BASE overrides them).
func zhihuSrcAt(t *testing.T, fake *fakeZhihu) *Source {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return newSource(Hosts{API: srv.URL, Daily: srv.URL, Web: srv.URL}, "")
}

func TestRegisteredUnderZhihuID(t *testing.T) {
	// Self-registration is the "add a source = adding a package"
	// contract — sources.toml's enabled list addresses this id.
	if _, err := source.New(ID, core.DefaultSources()); err != nil {
		t.Fatalf("registry lookup: %v", err)
	}
}

func TestFeedsAreTheHotFeed(t *testing.T) {
	src := zhihuSrcAt(t, &fakeZhihu{})
	feeds := src.Feeds()
	if len(feeds) != 1 {
		t.Fatalf("got %d feeds, want 1 (hot; daily/follow land in tickets 03/05)", len(feeds))
	}
	hot := feeds[0]
	if hot.Key != HotFeedKey {
		t.Errorf("feed key = %q, want %q", hot.Key, HotFeedKey)
	}
	if hot.LabelKey != "zhihu.hot" {
		t.Errorf("labelKey = %q, want zhihu.hot", hot.LabelKey)
	}
	if !strings.HasSuffix(hot.URL, "/topstory/hot-list") || strings.Contains(hot.URL, "www.") {
		t.Errorf("url = %q, want the anonymous api door", hot.URL)
	}
	// The board's return order is heat order: ListSortPoints like hn's
	// popularity feeds.
	if hot.ListSort != core.ListSortPoints {
		t.Errorf("ListSort = %q, want points", hot.ListSort)
	}
}

func TestFetchMapsHotRowsToQuestionCards(t *testing.T) {
	fake := &fakeZhihu{hotOverride: hotRowsJSON}
	src := zhihuSrcAt(t, fake)
	items, err := src.Fetch(t.Context(), src.Feeds()[0])
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(items) != 2 { // video card and title-less question skipped
		t.Fatalf("got %d items, want 2", len(items))
	}

	top := items[0]
	// The qid comes out of target.url exact — the bare target.id integer
	// (18984420158198241699 > 2^53) would mangle through any float64
	// path; the id digits here are the URL's, digit for digit.
	if top.ID != "zh:question/18984420158198241699" {
		t.Errorf("id = %q, want the url-extracted qid", top.ID)
	}
	if top.Source != ID || top.Feed != HotFeedKey {
		t.Errorf("source/feed = %q/%q, want zhihu/hot", top.Source, top.Feed)
	}
	if top.Title != "为什么终端阅读器又火了？" {
		t.Errorf("title = %q", top.Title)
	}
	// The row URL is the canonical www question page, not the api
	// spelling the payload carries.
	if top.URL != "https://www.zhihu.com/question/18984420158198241699" {
		t.Errorf("url = %q, want the canonical question page", top.URL)
	}
	// A ranked board has no timestamps: nil PublishedAt, and the meta
	// story is the heat, carried by the points snapshot.
	if top.PublishedAt != nil {
		t.Errorf("PublishedAt = %v, want nil (boards carry no dates)", top.PublishedAt)
	}
	if top.Points == nil || *top.Points != 20450000 {
		t.Errorf("heat snapshot = %v, want 20450000 (2045 万 folded)", top.Points)
	}
	if top.FetchedAt.IsZero() {
		t.Error("FetchedAt should carry the snapshot clock")
	}

	pinned := items[1]
	if pinned.ID != "zh:question/100001" || pinned.Title != "置顶卡没有热度列" {
		t.Errorf("pinned card mapping = %+v", pinned)
	}
	// No heat column → nil points, never a zero: the row renders
	// without the segment.
	if pinned.Points != nil {
		t.Errorf("empty detail_text must fold to nil points, got %d", *pinned.Points)
	}
}

// The request fingerprint is part of the data-plane contract (spec:
// 桌面 UA、x-requested-with、按端点 Referer) — the fetch must wear the
// measured browser shape, not the reader's own identity.
func TestFetchWearsTheMeasuredFingerprint(t *testing.T) {
	fake := &fakeZhihu{hotOverride: hotRowsJSON}
	src := zhihuSrcAt(t, fake)
	if _, err := src.Fetch(t.Context(), src.Feeds()[0]); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	hit := fake.hit("/topstory/hot-list")
	if hit == nil {
		t.Fatal("hot door never hit")
	}
	if hit.ua != DesktopUA {
		t.Errorf("User-Agent = %q, want the desktop UA", hit.ua)
	}
	if hit.xrw != "fetch" {
		t.Errorf("x-requested-with = %q, want fetch", hit.xrw)
	}
	if hit.referer != "https://www.zhihu.com/hot" {
		t.Errorf("Referer = %q, want the hot page", hit.referer)
	}
}

func TestFetchHeatFoldToleratesSpelling(t *testing.T) {
	body := `{"data":[
 {"detail_text":"1362万热度","target":{"id":11,"type":"question","title":"无空格写法",
  "url":"https://api.zhihu.com/questions/11"}},
 {"detail_text":"热度飙升","target":{"id":12,"type":"question","title":"非热度文案",
  "url":"https://api.zhihu.com/questions/12"}}
]}`
	src := zhihuSrcAt(t, &fakeZhihu{hotOverride: body})
	items, err := src.Fetch(t.Context(), src.Feeds()[0])
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if items[0].Points == nil || *items[0].Points != 13620000 {
		t.Errorf("spaceless heat = %v, want 13620000", items[0].Points)
	}
	if items[1].Points != nil {
		t.Errorf("non-heat detail_text must fold to nil, got %d", *items[1].Points)
	}
}

func TestFetchHTTPFailureNamesFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	src := newSource(Hosts{API: srv.URL}, "")
	_, err := src.Fetch(t.Context(), core.Feed{Key: HotFeedKey})
	if err == nil || !strings.Contains(err.Error(), "hot") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want error naming feed and status, got: %v", err)
	}
}

func TestFetchGarbageIsParseError(t *testing.T) {
	src := zhihuSrcAt(t, &fakeZhihu{hotOverride: "<html>not json</html>"})
	_, err := src.Fetch(t.Context(), src.Feeds()[0])
	if err == nil || !strings.Contains(err.Error(), "parse hot") {
		t.Fatalf("want parse error naming feed, got: %v", err)
	}
}

func TestFetchUnknownFeedKeyIsError(t *testing.T) {
	src := zhihuSrcAt(t, &fakeZhihu{})
	_, err := src.Fetch(t.Context(), core.Feed{Key: "recommend"})
	if err == nil || !strings.Contains(err.Error(), "recommend") {
		t.Fatalf("unknown feed key should error naming it, got: %v", err)
	}
}
