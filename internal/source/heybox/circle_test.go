package heybox

// circle_test.go — the circle feeds' list layer (ticket 02) against
// the same fake heybox the recommend tests ride: feed listing in
// declaration order, the topic feed's measured request fingerprint,
// rows landing under the circle's feed key, the hand-edit validation
// gate, and the unknown-key refusal. The refresh-layer e2e (independent
// paging depths, re-homing) lives in internal/refresh
// (TestRefreshHeyboxCirclesTracerBullet).

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/core"
)

// circleSrcAt lands a fresh source subscribed to the given circles
// against one fake server serving body.
func circleSrcAt(t *testing.T, body []byte, circles ...core.HeyboxCircle) (*Source, *fakeServe) {
	t.Helper()
	src, fake := heyboxSrcAt(t, body)
	if err := src.loadCircles(&core.Sources{
		Heybox: &core.HeyboxConfig{Circles: circles},
	}); err != nil {
		t.Fatalf("loadCircles: %v", err)
	}
	return src, fake
}

func TestFeedsListCirclesInDeclarationOrder(t *testing.T) {
	src, _ := circleSrcAt(t, recommendFixture(t),
		core.HeyboxCircle{Name: "黑神话：悟空", TopicID: 568547},
		core.HeyboxCircle{Name: "塞尔达传说", TopicID: 42},
	)
	feeds := src.Feeds()
	if len(feeds) != 3 {
		t.Fatalf("got %d feeds, want 1 recommend + 2 circles", len(feeds))
	}
	if feeds[0].Key != RecommendFeedKey {
		t.Fatalf("feed[0] = %q, want the recommend feed first", feeds[0].Key)
	}
	if feeds[1].Key != "circle/568547" || feeds[1].Label != "黑神话：悟空" {
		t.Errorf("circle[0] = %q/%q, want circle/568547 labeled 黑神话：悟空", feeds[1].Key, feeds[1].Label)
	}
	if feeds[2].Key != "circle/42" || feeds[2].Label != "塞尔达传说" {
		t.Errorf("circle[1] = %q/%q, want circle/42 labeled 塞尔达传说 (declaration order)", feeds[2].Key, feeds[2].Label)
	}
	// The TOML name is the sidebar label as-is: no i18n key, the
	// user's own words render.
	if feeds[1].LabelKey != "" {
		t.Errorf("circle label key = %q, want none — the TOML name is the label", feeds[1].LabelKey)
	}
	// Feeds hands out a copy: mutating it must not leak into the source.
	feeds[1].Label = "mutated"
	if src.Feeds()[1].Label != "黑神话：悟空" {
		t.Error("Feeds must return a copy, not the internal slice")
	}
}

func TestFetchCirclePageRequestFingerprint(t *testing.T) {
	src, fake := circleSrcAt(t, recommendFixture(t),
		core.HeyboxCircle{Name: "黑神话：悟空", TopicID: 568547},
	)
	feed := src.Feeds()[1]
	if _, err := src.FetchPage(t.Context(), feed, 3); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if len(fake.got) != 1 {
		t.Fatalf("fake saw %d requests, want 1", len(fake.got))
	}
	q := fake.got[0]
	for _, c := range [][2]string{
		{"topic_id", "568547"},
		{"offset", "3"}, // the page number rides as the offset
		{"limit", "10"},
		{"dw", "720"},
	} {
		if got := q.Get(c[0]); got != c[1] {
			t.Errorf("%s param = %q, want %q (the smoke's measured shape)", c[0], got, c[1])
		}
	}
	// The empty lastval rides too — the measured request carried it.
	if got, ok := q["lastval"]; !ok || got[0] != "" {
		t.Errorf("lastval param = %v, want the explicit empty value", q["lastval"])
	}
}

func TestFetchCircleRowsCarryCircleFeedKey(t *testing.T) {
	src, _ := circleSrcAt(t, recommendFixture(t),
		core.HeyboxCircle{Name: "黑神话：悟空", TopicID: 568547},
	)
	feed := src.Feeds()[1]
	items, err := src.Fetch(t.Context(), feed)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// The fixture's 5 cards drop to 3 rows here too (ad + page repeat)
	// — the circle endpoint serves the same card shape.
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	for _, it := range items {
		if it.Source != ID || it.Feed != "circle/568547" {
			t.Fatalf("row %q carries %s/%s, want heybox/circle/568547 — the store's ownership check rides this", it.ID, it.Source, it.Feed)
		}
	}
	if items[0].ID != "hb:link/236172cefddd" {
		t.Errorf("id = %q, want the share_url's hex link_id", items[0].ID)
	}
}

func TestFetchCircleEmptyPageIsTheEnd(t *testing.T) {
	src, _ := circleSrcAt(t, []byte(`{"msg":"","result":{"links":[]},"status":"ok"}`),
		core.HeyboxCircle{Name: "空圈", TopicID: 7},
	)
	items, err := src.FetchPage(t.Context(), src.Feeds()[1], 5)
	if err != nil {
		t.Fatalf("FetchPage on an empty circle page: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("got %d items, want 0 — the interface's end signal", len(items))
	}
}

func TestLoadCirclesValidatesHandEdits(t *testing.T) {
	src := newSourceAt("http://unused.example", WithMinGap(0))
	bad := []struct {
		name    string
		circles []core.HeyboxCircle
		wantErr string
	}{
		{"missing name", []core.HeyboxCircle{{TopicID: 568547}}, "name is required"},
		{"blank name", []core.HeyboxCircle{{Name: "  ", TopicID: 568547}}, "name is required"},
		{"zero topic_id", []core.HeyboxCircle{{Name: "无名圈"}}, "topic_id must be positive"},
		{"negative topic_id", []core.HeyboxCircle{{Name: "负圈", TopicID: -1}}, "topic_id must be positive"},
		{"duplicate topic", []core.HeyboxCircle{
			{Name: "黑神话：悟空", TopicID: 568547},
			{Name: "黑神话副本", TopicID: 568547},
		}, "already subscribed"},
	}
	for _, c := range bad {
		fresh := newSourceAt("http://unused.example", WithMinGap(0))
		err := fresh.loadCircles(&core.Sources{Heybox: &core.HeyboxConfig{Circles: c.circles}})
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: loadCircles err = %v, want it to name %q", c.name, err, c.wantErr)
		}
	}
	// A nil [heybox] table (fresh install, no circles) is a valid
	// state: recommend-only, no error.
	if err := src.loadCircles(core.DefaultSources()); err != nil {
		t.Fatalf("nil heybox table must not error: %v", err)
	}
	if len(src.Feeds()) != 1 {
		t.Fatalf("got %d feeds, want the recommend feed alone", len(src.Feeds()))
	}
}

// TestNewGatesOnBrokenCircleTable: construction (not just the
// internal loader) refuses a broken [heybox] table — the F5 path
// builds its source through New, so a bad hand edit must fail there
// before any dial.
func TestNewGatesOnBrokenCircleTable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a broken circle table must fail before any dial, got %s", r.URL)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("READSCOPE_HEYBOX_BASE", srv.URL)
	t.Setenv("READSCOPE_HEYBOX_MINGAP", "0")
	t.Setenv("READSCOPE_HEYBOX_DEVICE", "0123456789abcdef0123456789abcdef")

	srcs := core.DefaultSources()
	srcs.Heybox = &core.HeyboxConfig{Circles: []core.HeyboxCircle{{Name: "坏圈"}}} // no topic_id
	if _, err := New(srcs); err == nil || !strings.Contains(err.Error(), "topic_id") {
		t.Fatalf("New on a broken table = %v, want a topic_id error", err)
	}
}

// topicFixture loads the trimmed captured circle page — a cut of
// the smoke's own 08 sample (video post, titled post, text post,
// cube markers all aboard), pinning the topic endpoint's real field
// shape against drift the synthetic fixtures could hide.
func topicFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/topic-page.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

// TestFetchCircleRealSampleMapsToRows: the captured 黑神话 page walks
// the same card pipeline — the titleless video post's degraded
// excerpt as title, heat snapshots, hex ids — under a circle feed
// key.
func TestFetchCircleRealSampleMapsToRows(t *testing.T) {
	src, _ := circleSrcAt(t, topicFixture(t),
		core.HeyboxCircle{Name: "黑神话：悟空", TopicID: 568547},
	)
	items, err := src.Fetch(t.Context(), src.Feeds()[1])
	if err != nil {
		t.Fatalf("Fetch on the captured page: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("got %d items, want the captured page's 4 cards", len(items))
	}
	first := items[0]
	if !strings.Contains(first.Title, "滑稽") || strings.Contains(first.Title, "cube_") {
		t.Errorf("video-post title = %q, want the degraded excerpt", first.Title)
	}
	if first.ID != "hb:link/c8dc72cd5cf1" {
		t.Errorf("id = %q, want the captured share_url's hex id", first.ID)
	}
	if first.Points == nil || *first.Points != 49 || first.NumComments == nil || *first.NumComments != 18 {
		t.Errorf("heat = %v/%v, want the captured 49/18", first.Points, first.NumComments)
	}
	if !strings.Contains(items[1].Title, "小黄龙") {
		t.Errorf("titled card = %q", items[1].Title)
	}
	if strings.Contains(items[3].Title, "[cube_") {
		t.Errorf("cube marker survived: %q", items[3].Title)
	}
}
