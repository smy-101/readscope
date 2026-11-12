package zhihu

// Ticket 03 tests: the daily digest — the fixed third feed (list from
// news-at.zhihu.com's latest endpoint, hint riding the meta column)
// and the zhihu half of the BodySource extension (FetchBody pulls the
// story body on first open, CleanHTML preprocessing included — the
// DOM-cleaning rules themselves live in clean_test.go).

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
)

// brokenServer answers 500 to everything — the fetch-error legs.
func brokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// dailyRowsJSON: two healthy stories (hint and a hint-less one), a
// title-less one, and an empty id — the degenerate rows skip, never
// crash. The story ids stay small here; the big-integer contract is
// the question family's (ids.go), and daily ids ride json.Number the
// same way.
const dailyRowsJSON = `{"date":"20261208","stories":[
 {"id":9792937,"title":"机器知道它在读什么吗","hint":"AI 新榜单 · 12 分钟阅读"},
 {"id":9792938,"title":"没有 hint 的条目","hint":""},
 {"id":9792939,"title":"","hint":"标题缺失"},
 {"id":0,"title":"id 缺失","hint":"x"}
]}`

func dailyFeed() core.Feed {
	for _, f := range (&Source{}).Feeds() {
		if f.Key == DailyFeedKey {
			return f
		}
	}
	return core.Feed{}
}

func TestDailyFeedSitsThirdInSidebar(t *testing.T) {
	src := zhihuSrcAt(t, &fakeZhihu{})
	feeds := src.Feeds()
	if len(feeds) != 3 {
		t.Fatalf("got %d feeds, want 3 (hot, follow, daily)", len(feeds))
	}
	daily := feeds[2]
	if daily.Key != DailyFeedKey || daily.LabelKey != "zhihu.daily" {
		t.Fatalf("daily feed = %+v", daily)
	}
	if !strings.Contains(daily.URL, "/api/4/news/latest") {
		t.Fatalf("daily feed should point at the latest endpoint: %q", daily.URL)
	}
	// A day's digest is a snapshot, not a walk: no cursor paging here.
	if src.CursorPaged(daily) {
		t.Fatal("daily feed must not page (Ctrl+N says no more)")
	}
}

func TestFetchMapsDailyStories(t *testing.T) {
	fake := &fakeZhihu{dailyLatestOverride: dailyRowsJSON}
	src := zhihuSrcAt(t, fake)
	items, err := src.Fetch(t.Context(), dailyFeed())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (title-less and id-less rows skip)", len(items))
	}
	first := items[0]
	if first.ID != "zh:daily/9792937" || first.Source != ID || first.Feed != DailyFeedKey {
		t.Fatalf("id/source/feed = %q/%q/%q", first.ID, first.Source, first.Feed)
	}
	if first.Title != "机器知道它在读什么吗" {
		t.Errorf("title = %q", first.Title)
	}
	// The row URL is the human-openable daily story page (Ctrl+O/Y
	// target), not the API door the fetch rides.
	if first.URL != "https://daily.zhihu.com/story/9792937" {
		t.Errorf("url = %q, want the story page", first.URL)
	}
	// The hint rides the meta column verbatim — the source serves it
	// as one atomic string ("作者 · N 分钟阅读"), the UI prints it as-is.
	if first.Meta != "AI 新榜单 · 12 分钟阅读" {
		t.Errorf("meta = %q, want the hint", first.Meta)
	}
	if first.Points != nil || first.NumComments != nil {
		t.Errorf("daily rows carry no heat snapshot: %+v", first)
	}
	if second := items[1]; second.Meta != "" {
		t.Errorf("missing hint must render as empty meta, got %q", second.Meta)
	}
	// The digest declares no per-story clock; the snapshot clock is
	// the fetch's, same as the hot board.
	if first.PublishedAt != nil {
		t.Errorf("PublishedAt = %v, want nil (no per-story dates)", first.PublishedAt)
	}
}

func TestFetchDailyErrorsNameTheFeed(t *testing.T) {
	src := zhihuSrcAt(t, &fakeZhihu{dailyLatestOverride: "<html>not json</html>"})
	if _, err := src.Fetch(t.Context(), dailyFeed()); err == nil || !strings.Contains(err.Error(), "parse daily") {
		t.Fatalf("want parse error naming feed, got: %v", err)
	}
	src2 := newSource(Hosts{Daily: brokenServer(t).URL}, "")
	if _, err := src2.Fetch(t.Context(), core.Feed{Key: DailyFeedKey}); err == nil ||
		!strings.Contains(err.Error(), "daily") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want http error naming feed and status, got: %v", err)
	}
	if _, err := src2.Fetch(t.Context(), core.Feed{Key: "recommend"}); err == nil ||
		!strings.Contains(err.Error(), "recommend") {
		t.Fatalf("unknown feed key should error naming it, got: %v", err)
	}
}

func TestBodyOnDemandRoutesDailyItemsOnly(t *testing.T) {
	src := zhihuSrcAt(t, &fakeZhihu{})
	if !src.BodyOnDemand(core.Item{ID: "zh:daily/9792937"}) {
		t.Fatal("daily story bodies are fetched on demand")
	}
	for _, id := range []string{"zh:question/18984420158198241699", "hn:algolia:1", ""} {
		if src.BodyOnDemand(core.Item{ID: id}) {
			t.Fatalf("BodyOnDemand(%q) must be false — question cards are detail-layer items", id)
		}
	}
	// The extension is registered on the source, probeable the same
	// way the TUI does it.
	if _, ok := source.Source(src).(source.BodySource); !ok {
		t.Fatal("zhihu must implement source.BodySource")
	}
}

func TestFetchBodyPullsStoryThroughCleaning(t *testing.T) {
	// The story door serves JSON whose body field IS the daily HTML
	// subset from clean_test.go.
	storyJSON := fmt.Sprintf(`{"title":"机器知道它在读什么吗","body":%s}`, strconv.Quote(dailyStoryBodyHTML))
	fake := &fakeZhihu{dailyStoryOverride: storyJSON}
	src := zhihuSrcAt(t, fake)
	it := core.Item{ID: "zh:daily/9792937", Source: ID, Feed: DailyFeedKey, Title: "机器知道它在读什么吗"}
	body, err := src.FetchBody(t.Context(), it)
	if err != nil {
		t.Fatalf("FetchBody: %v", err)
	}
	// The cleaning rules applied (see clean_test.go): lazy src swapped
	// for data-original, the thumbnail proxy segment stripped.
	if !strings.Contains(body, "https://pic1.zhimg.com/v2-story_hd.jpg") {
		t.Fatalf("body should carry the cleaned image URL:\n%s", body)
	}
	if strings.Contains(body, "data:image") || strings.Contains(body, "/50/") {
		t.Fatalf("lazy placeholder or thumbnail proxy survived:\n%s", body)
	}
	// The story door wears the daily Referer (the shared matrix's row).
	if hit := fake.hit("/api/4/story/"); hit == nil || hit.referer != "https://daily.zhihu.com/" {
		t.Fatalf("story fetch fingerprint wrong: %+v", fake.hit("/api/4/story/"))
	}
}

func TestFetchBodyErrors(t *testing.T) {
	// Not a daily id: a loud refusal, not a bogus request.
	src := zhihuSrcAt(t, &fakeZhihu{})
	if _, err := src.FetchBody(t.Context(), core.Item{ID: "zh:question/1"}); err == nil ||
		!strings.Contains(err.Error(), "not a daily") {
		t.Fatalf("want refusal naming the id shape, got: %v", err)
	}
	// HTTP failure names the story door.
	broken := newSource(Hosts{Daily: brokenServer(t).URL}, "")
	if _, err := broken.FetchBody(t.Context(), core.Item{ID: "zh:daily/1"}); err == nil ||
		!strings.Contains(err.Error(), "story") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want http error naming story, got: %v", err)
	}
	// Garbage body / empty body — the two degenerate documents.
	garbage := zhihuSrcAt(t, &fakeZhihu{dailyStoryOverride: "<html>not json</html>"})
	if _, err := garbage.FetchBody(t.Context(), core.Item{ID: "zh:daily/1"}); err == nil ||
		!strings.Contains(err.Error(), "parse") {
		t.Fatalf("want parse error, got: %v", err)
	}
	empty := zhihuSrcAt(t, &fakeZhihu{dailyStoryOverride: `{"title":"x","body":""}`})
	if _, err := empty.FetchBody(t.Context(), core.Item{ID: "zh:daily/1"}); err == nil ||
		!strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty story body should error (probe BLOCKED semantics), got: %v", err)
	}
}
