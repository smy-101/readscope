package rss

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
)

const rssSample = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel>
  <title>Example</title>
  <item>
    <title>First post</title>
    <guid isPermaLink="false">urn:example:1</guid>
    <link>https://example.com/1</link>
    <author>alice@example.com (Alice)</author>
    <pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate>
    <content:encoded><![CDATA[<p>Full <b>body</b> one</p>]]></content:encoded>
  </item>
  <item>
    <title>Second post</title>
    <guid>https://example.com/2-guid</guid>
    <link>https://example.com/2</link>
    <description>Summary only</description>
  </item>
</channel></rss>`

const atomSample = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Atom Example</title>
  <entry>
    <id>tag:example.org,2005:1</id>
    <title>Atom entry</title>
    <link href="https://example.org/1"/>
    <updated>2006-01-02T15:04:05Z</updated>
    <content type="html">&lt;p&gt;atom body&lt;/p&gt;</content>
  </entry>
</feed>`

func serve(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			http.Error(w, "boom", status)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sourcesWith(feeds map[string]core.RSSFeed) *core.Sources {
	srcs := core.DefaultSources()
	srcs.RSS.Feeds = feeds
	return srcs
}

func TestFetchParsesRSS(t *testing.T) {
	srv := serve(t, rssSample, http.StatusOK)
	src, err := New(sourcesWith(map[string]core.RSSFeed{
		"ex": {Label: "Example", URL: srv.URL + "/feed"},
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	items, err := src.Fetch(t.Context(), src.Feeds()[0])
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	first := items[0]
	if first.ID != "rss:ex:urn:example:1" {
		t.Fatalf("ID = %q, want rss:ex:urn:example:1", first.ID)
	}
	if first.Source != "rss" || first.Feed != "ex" {
		t.Fatalf("source/feed = %s/%s", first.Source, first.Feed)
	}
	if first.Title != "First post" || first.URL != "https://example.com/1" {
		t.Fatalf("title/url = %q/%q", first.Title, first.URL)
	}
	if !strings.Contains(first.Body, "Full") {
		t.Fatalf("Body should prefer content:encoded, got %q", first.Body)
	}
	if first.PublishedAt == nil || first.PublishedAt.UTC().Format(time.DateOnly) != "2006-01-02" {
		t.Fatalf("PublishedAt = %v, want 2006-01-02", first.PublishedAt)
	}
	if second := items[1]; !strings.Contains(second.Body, "Summary only") {
		t.Fatalf("Body should fall back to description, got %q", second.Body)
	}
}

func TestFetchParsesAtom(t *testing.T) {
	srv := serve(t, atomSample, http.StatusOK)
	src, err := New(sourcesWith(map[string]core.RSSFeed{
		"atom": {URL: srv.URL},
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if feeds := src.Feeds(); len(feeds) != 1 || feeds[0].Label != "atom" {
		t.Fatalf("label fallback to key failed: %+v", src.Feeds())
	}
	items, err := src.Fetch(t.Context(), src.Feeds()[0])
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].ID != "rss:atom:tag:example.org,2005:1" {
		t.Fatalf("ID = %q", items[0].ID)
	}
	if items[0].PublishedAt == nil {
		t.Fatal("atom <updated> should fill PublishedAt")
	}
}

func TestItemIDFallbacks(t *testing.T) {
	withGUID := &gofeed.Item{GUID: "g", Link: "l"}
	if got := ItemID("k", withGUID); got != "rss:k:g" {
		t.Fatalf("guid fallback = %q", got)
	}
	noGUID := &gofeed.Item{Link: "l"}
	if got := ItemID("k", noGUID); got != "rss:k:l" {
		t.Fatalf("link fallback = %q", got)
	}
	bare := &gofeed.Item{Title: "t", Published: "p"}
	a, b := ItemID("k", bare), ItemID("k", bare)
	if a != b || !strings.HasPrefix(a, "rss:k:sha1:") {
		t.Fatalf("hash fallback not deterministic: %q vs %q", a, b)
	}
}

func TestFetchHTTPFailureIsClearError(t *testing.T) {
	srv := serve(t, "", http.StatusInternalServerError)
	src, err := New(sourcesWith(map[string]core.RSSFeed{"ex": {URL: srv.URL}}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = src.Fetch(t.Context(), src.Feeds()[0])
	if err == nil || !strings.Contains(err.Error(), "ex") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want clear per-feed error with key and status, got: %v", err)
	}
}

func TestFetchGarbageIsParseError(t *testing.T) {
	srv := serve(t, "<html><body>not a feed</body></html>", http.StatusOK)
	src, _ := New(sourcesWith(map[string]core.RSSFeed{"ex": {URL: srv.URL}}))
	_, err := src.Fetch(t.Context(), src.Feeds()[0])
	if err == nil || !strings.Contains(err.Error(), "parse ex") {
		t.Fatalf("want parse error naming feed, got: %v", err)
	}
}

func TestNewRequiresFeedURL(t *testing.T) {
	if _, err := New(sourcesWith(map[string]core.RSSFeed{"broken": {Label: "x"}})); err == nil {
		t.Fatal("expected error for feed without url")
	}
}

func TestFeedsSortedByKey(t *testing.T) {
	src, err := New(sourcesWith(map[string]core.RSSFeed{
		"zeta":  {URL: "https://z.example"},
		"alpha": {URL: "https://a.example"},
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	feeds := src.Feeds()
	if feeds[0].Key != "alpha" || feeds[1].Key != "zeta" {
		t.Fatalf("feeds not sorted: %v", feeds)
	}
}

// etagServer serves one body under one ETag; a request carrying
// If-None-Match for the current ETag gets 304. Swapping body+etag mid-test
// mimics a real feed whose content changed.
type etagServer struct {
	*httptest.Server
	body         string
	etag         string
	lastModified string
	hits         int
	condHits     int
}

func newEtagServer(t *testing.T, body, etag string) *etagServer {
	t.Helper()
	es := &etagServer{body: body, etag: etag}
	es.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		es.hits++
		if es.etag != "" && r.Header.Get("If-None-Match") == es.etag {
			es.condHits++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if es.etag != "" {
			w.Header().Set("ETag", es.etag)
		}
		if es.lastModified != "" {
			w.Header().Set("Last-Modified", es.lastModified)
		}
		w.Write([]byte(es.body))
	}))
	t.Cleanup(es.Close)
	return es
}

// srcFor builds a one-feed source pointed at url and returns it with
// its single feed, ready for conditional-fetch tests.
func srcFor(t *testing.T, url string) (*Source, core.Feed) {
	t.Helper()
	iface, err := New(sourcesWith(map[string]core.RSSFeed{"ex": {URL: url}}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	src := iface.(*Source)
	return src, src.Feeds()[0]
}

func TestFetchConditional304SkipsBody(t *testing.T) {
	es := newEtagServer(t, rssSample, `"v1"`)
	src, f := srcFor(t, es.URL)

	// First fetch: unconditional, returns items plus the validators.
	items, next, changed, err := src.FetchConditional(t.Context(), f, core.FetchState{})
	if err != nil || !changed || len(items) != 2 {
		t.Fatalf("first fetch = (%d items, changed=%v, err=%v)", len(items), changed, err)
	}
	if next.ETag != `"v1"` || next.LastFetched.IsZero() {
		t.Fatalf("first state = %+v", next)
	}

	// Second fetch with the stored validators: 304, no items, unchanged.
	items2, next2, changed2, err := src.FetchConditional(t.Context(), f, next)
	if err != nil {
		t.Fatalf("conditional fetch: %v", err)
	}
	if changed2 || len(items2) != 0 {
		t.Fatalf("304 should be unchanged with no items, got (changed=%v, %d items)", changed2, len(items2))
	}
	if es.condHits != 1 {
		t.Fatalf("server saw %d conditional hits, want 1", es.condHits)
	}
	if next2.ETag != `"v1"` {
		t.Fatalf("304 must keep validators for next time: %+v", next2)
	}
}

func TestFetchConditionalETagChangeRefetches(t *testing.T) {
	es := newEtagServer(t, rssSample, `"v1"`)
	src, f := srcFor(t, es.URL)
	_, state, _, err := src.FetchConditional(t.Context(), f, core.FetchState{})
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}

	es.body = strings.Replace(rssSample, "First post", "First post (edited)", 1)
	es.etag = `"v2"`
	items, next, changed, err := src.FetchConditional(t.Context(), f, state)
	if err != nil || !changed {
		t.Fatalf("changed etag should refetch: changed=%v err=%v", changed, err)
	}
	if len(items) != 2 || items[0].Title != "First post (edited)" {
		t.Fatalf("items after etag change = %+v", items)
	}
	if next.ETag != `"v2"` {
		t.Fatalf("state should carry new etag: %+v", next)
	}
}

func TestFetchConditionalWithoutValidatorsDegrades(t *testing.T) {
	// Server never sends ETag or Last-Modified: state comes back with
	// empty validators, and the second request must carry no conditional
	// headers at all — a plain refresh with no side effects.
	es := newEtagServer(t, rssSample, "")
	src, f := srcFor(t, es.URL)

	items, next, changed, err := src.FetchConditional(t.Context(), f, core.FetchState{})
	if err != nil || !changed || next.ETag != "" || next.LastModified != "" {
		t.Fatalf("no-validator server: changed=%v state=%+v err=%v", changed, next, err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}

	// Second pass with the (empty) state: still a full 200 refresh.
	items2, _, changed2, err := src.FetchConditional(t.Context(), f, next)
	if err != nil || !changed2 || len(items2) != 2 {
		t.Fatalf("degraded refresh should behave like a plain one: changed=%v err=%v", changed2, err)
	}
	if es.hits != 2 {
		t.Fatalf("hits = %d, want 2", es.hits)
	}
}

func TestFetchConditionalBogus304IsError(t *testing.T) {
	// RFC 9110 §15.4.5: a server must not answer an unconditional
	// request with 304. If one does, that is a broken server — an error,
	// not a silent "snapshot current" for a feed we never fetched.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	t.Cleanup(srv.Close)
	src, f := srcFor(t, srv.URL)
	_, _, _, err := src.FetchConditional(t.Context(), f, core.FetchState{})
	if err == nil || !strings.Contains(err.Error(), "304") {
		t.Fatalf("unconditional 304 should be an error, got %v", err)
	}
}

func TestFetchConditionalLastModifiedOnly(t *testing.T) {
	// Feeds without ETag can still be conditional via Last-Modified.
	es := newEtagServer(t, rssSample, "")
	es.lastModified = "Mon, 02 Jan 2006 15:04:05 GMT"
	es.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-Modified-Since") == es.lastModified {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Last-Modified", es.lastModified)
		w.Write([]byte(es.body))
	})
	src, f := srcFor(t, es.URL)

	_, state, _, err := src.FetchConditional(t.Context(), f, core.FetchState{})
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if state.LastModified == "" {
		t.Fatalf("state should record Last-Modified: %+v", state)
	}

	_, _, changed, err := src.FetchConditional(t.Context(), f, state)
	if err != nil || changed {
		t.Fatalf("if-modified-since match should be unchanged: changed=%v err=%v", changed, err)
	}
}

// Self-registration is the "add a source = add a package" contract: the
// rss package's init must have installed the constructor.
func TestRegisteredUnderRSSID(t *testing.T) {
	if _, err := source.New(ID, core.DefaultSources()); err != nil {
		t.Fatalf("registry lookup: %v", err)
	}
}
