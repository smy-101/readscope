package heybox

// recommend_test.go — the recommendation feed's list layer (ticket 01)
// against one fake heybox on loopback, serving the smoke's own sample
// shape (testdata/recommend-page.json — a trimmed cut of the captured
// P14 page): registration, the F5 fetch's request fingerprint, the
// card→row mapping with ad-drop / hex ids / heat snapshots / cube
// degradation / titleless text posts, page-level dedup, and the
// offset walk's shape. The refresh-layer e2e (cache + restart-resume
// paging) lives in internal/refresh (TestRefreshHeyboxTracerBullet).

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
)

// recommendFixture loads the trimmed captured page.
func recommendFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/recommend-page.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

// fakeServe answers every leg with one fixed body and records the
// query each request carried.
type fakeServe struct {
	body  []byte
	got   []url.Values
	calls int
}

func (f *fakeServe) handler(w http.ResponseWriter, r *http.Request) {
	// Server-style signature verification, the transport tests'
	// fakeHeybox logic in miniature: a bad triple answers the real
	// host's refusal, so these tests cannot pass on an unsigned or
	// crossed request.
	tsParam := r.URL.Query().Get("_time")
	hkey := r.URL.Query().Get("hkey")
	nonce := r.URL.Query().Get("nonce")
	var ts int64
	for _, c := range []byte(tsParam) {
		ts = ts*10 + int64(c-'0')
	}
	if hkey == "" || nonce == "" || ts == 0 || signA(r.URL.Path, ts, nonce) != hkey {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"msg":"缺少必要参数[_time]","result":{},"status":"failed"}`))
		return
	}
	f.calls++
	f.got = append(f.got, r.URL.Query())
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(f.body)
}

// heyboxSrcAt lands a fresh-paged source against one fake server.
func heyboxSrcAt(t *testing.T, body []byte) (*Source, *fakeServe) {
	t.Helper()
	fake := &fakeServe{body: body}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)
	return newSourceAt(srv.URL, WithMinGap(0)), fake
}

func TestRegisteredUnderHeyboxID(t *testing.T) {
	// Self-registration is the "add a source = adding a package"
	// contract — sources.toml's enabled list addresses this id. The
	// config dir points at a tempdir: construction loads-or-mints the
	// device fingerprint, and a test must never touch the real one.
	t.Setenv("READSCOPE_CONFIG", t.TempDir())
	if _, err := source.New(ID, core.DefaultSources()); err != nil {
		t.Fatalf("registry lookup: %v", err)
	}
}

func TestFetchMapsCardsToPostRows(t *testing.T) {
	src, _ := heyboxSrcAt(t, recommendFixture(t))
	items, err := src.Fetch(t.Context(), src.Feeds()[0])
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// 5 cards served, 3 rows land: the ad card (no linkid, no
	// share_url) and the page's own duplicate drop.
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3 (ad + dup dropped)", len(items))
	}

	top := items[0]
	if top.ID != "hb:link/236172cefddd" {
		t.Errorf("id = %q, want the share_url's hex link_id", top.ID)
	}
	if top.Source != ID || top.Feed != RecommendFeedKey {
		t.Errorf("source/feed = %q/%q, want heybox/recommend", top.Source, top.Feed)
	}
	if top.Title != "背刺拉满！Steam新一轮史低疯跌，闭眼入不亏" {
		t.Errorf("title = %q", top.Title)
	}
	if top.Author != "知许。" {
		t.Errorf("author = %q, want the card user's username", top.Author)
	}
	if top.URL == "" || top.URL != "https://api.xiaoheihe.cn/v3/bbs/app/api/web/share?h_camp=link&h_src=YXBwX3NoYXJl&link_id=236172cefddd" {
		t.Errorf("url = %q, want the share_url verbatim (the Ctrl+O target)", top.URL)
	}
	// The heat snapshot: award count (up serves 0 on web — the
	// fixtures pin link_award_num as the live signal) and comment
	// count, both as-of FetchedAt.
	if top.Points == nil || *top.Points != 2108 {
		t.Errorf("points = %v, want the award count 2108", top.Points)
	}
	if top.NumComments == nil || *top.NumComments != 102 {
		t.Errorf("num_comments = %v, want 102", top.NumComments)
	}
	// The card's own clock orders the feed: modify_at wins.
	want := time.Unix(1794546228, 0).UTC()
	if top.PublishedAt == nil || !top.PublishedAt.Equal(want) {
		t.Errorf("published_at = %v, want the card's modify_at %v", top.PublishedAt, want)
	}
	// The body snapshot carries the degraded description — offline
	// reading before the detail layer (ticket 03) lands.
	if top.Body == "" {
		t.Error("body snapshot is empty — the description must ride the row")
	}

	// Card 2 is a titleless text post: the description squeezes into
	// the title (excerpt), the full text rides the body.
	textPost := items[1]
	if textPost.Title == "" || textPost.Title == top.Title {
		t.Errorf("titleless card title = %q, want the description excerpt", textPost.Title)
	}
	if len([]rune(textPost.Title)) > titleExcerptRunes+1 { // excerpt + ellipsis
		t.Errorf("excerpt title = %d runes, want <= %d+1", len([]rune(textPost.Title)), titleExcerptRunes)
	}

	// Card 3's title carries two [cube_喜欢] emoji markers — degraded.
	if items[2].Title != "老大这次出了雪之下雪乃[喜欢][喜欢]" {
		t.Errorf("cube-degraded title = %q", items[2].Title)
	}
}

func TestDegradeStripsCubePrefix(t *testing.T) {
	for _, c := range [][2]string{
		{"[cube_滑稽]", "[滑稽]"},
		{"看这个[cube_笑c]哈哈哈", "看这个[笑c]哈哈哈"},
		{"没有标记", "没有标记"},
	} {
		if got := Degrade(c[0]); got != c[1] {
			t.Errorf("Degrade(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

func TestFetchPageWalksByOffset(t *testing.T) {
	src, fake := heyboxSrcAt(t, recommendFixture(t))
	feed := src.Feeds()[0]
	if _, err := src.FetchPage(t.Context(), feed, 2); err != nil {
		t.Fatalf("FetchPage(2): %v", err)
	}
	if len(fake.got) != 1 {
		t.Fatalf("fake saw %d requests, want 1", len(fake.got))
	}
	if got := fake.got[0].Get("offset"); got != "2" {
		t.Errorf("offset param = %q, want the page number 2", got)
	}
	// The feed's own measured params ride verbatim.
	if got := fake.got[0].Get("pull"); got != "0" {
		t.Errorf("pull param = %q, want 0", got)
	}
	if got := fake.got[0].Get("dw"); got != "800" {
		t.Errorf("dw param = %q, want 800", got)
	}
}

func TestFetchPageEmptyLinksIsTheEnd(t *testing.T) {
	src, _ := heyboxSrcAt(t, []byte(`{"msg":"","result":{"links":[]},"status":"ok"}`))
	items, err := src.FetchPage(t.Context(), src.Feeds()[0], 9)
	if err != nil {
		t.Fatalf("FetchPage on an empty page: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("got %d items, want 0 — the interface's end signal", len(items))
	}
}

func TestFetchUnknownFeedIsError(t *testing.T) {
	src, _ := heyboxSrcAt(t, recommendFixture(t))
	if _, err := src.Fetch(t.Context(), core.Feed{Key: "circle"}); err == nil {
		t.Fatal("an unknown feed key must refuse, not dial")
	}
}

// TestNewHonorsEnvOverrides: the construction seam — READSCOPE_HEYBOX_BASE
// points the transport at a fake, READSCOPE_HEYBOX_DEVICE pins the
// fingerprint without touching the config dir (the probe/debug door).
func TestNewHonorsEnvOverrides(t *testing.T) {
	fake := &fakeServe{body: recommendFixture(t)}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)
	t.Setenv("READSCOPE_HEYBOX_BASE", srv.URL)
	t.Setenv("READSCOPE_HEYBOX_DEVICE", "fedcba9876543210fedcba9876543210")

	src, err := New(core.DefaultSources())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	items, err := src.Fetch(t.Context(), src.Feeds()[0])
	if err != nil {
		t.Fatalf("Fetch through New: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	if got := fake.got[0].Get("device_id"); got != "fedcba9876543210fedcba9876543210" {
		t.Errorf("device_id param = %q, want the env-pinned fingerprint", got)
	}
}

// TestNewMintsDeviceFileInConfigDir: without the env override, the
// constructor loads-or-mints the fingerprint in the config dir — the
// install-time identity, one file, never re-minted silently.
func TestNewMintsDeviceFileInConfigDir(t *testing.T) {
	fake := &fakeServe{body: recommendFixture(t)}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)
	t.Setenv("READSCOPE_HEYBOX_BASE", srv.URL)
	t.Setenv("READSCOPE_HEYBOX_DEVICE", "")
	t.Setenv("READSCOPE_CONFIG", t.TempDir())

	src, err := New(core.DefaultSources())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := src.Fetch(t.Context(), src.Feeds()[0]); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	path, err := DevicePath()
	if err != nil {
		t.Fatalf("DevicePath: %v", err)
	}
	if id, err := LoadOrCreateDeviceID(path); err != nil || len(id) != 32 {
		t.Fatalf("device file after New: id=%q err=%v — want the minted 32hex", id, err)
	}
	// A corrupt device file refuses loudly instead of re-minting.
	if err := os.WriteFile(path, []byte("zz"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(core.DefaultSources()); err == nil {
		t.Fatal("a corrupt device file must fail construction loudly")
	}
}
