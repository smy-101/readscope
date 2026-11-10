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

// Self-registration is the "add a source = add a package" contract: the
// rss package's init must have installed the constructor.
func TestRegisteredUnderRSSID(t *testing.T) {
	if _, err := source.New(ID, core.DefaultSources()); err != nil {
		t.Fatalf("registry lookup: %v", err)
	}
}
