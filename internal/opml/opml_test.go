package opml

import (
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/core"
)

// A folder-grouped OPML in the shape NetNewsWire/Inoreader emit: nested
// outlines, feeds at both folder and top level, text and title both
// present (text is the display string and wins), plus non-feed attrs we
// must tolerate. Feeds come back in document order, folders flattened.
func TestParseWalksFolderHierarchy(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>my subs</title></head>
  <body>
    <outline text="Tech">
      <outline text="Hacker News" title="ignored title" type="rss" xmlUrl="https://hnrss.org/frontpage" htmlUrl="https://news.ycombinator.com"/>
      <outline text="News">
        <outline text="阮一峰周刊" type="rss" xmlUrl="https://www.ruanyifeng.com/blog/atom.xml"/>
      </outline>
    </outline>
    <outline text="Top level" type="rss" xmlUrl="https://example.com/feed.xml"/>
  </body>
</opml>`
	feeds, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []Feed{
		{Title: "Hacker News", URL: "https://hnrss.org/frontpage"},
		{Title: "阮一峰周刊", URL: "https://www.ruanyifeng.com/blog/atom.xml"},
		{Title: "Top level", URL: "https://example.com/feed.xml"},
	}
	if len(feeds) != len(want) {
		t.Fatalf("got %d feeds %+v, want %d", len(feeds), feeds, len(want))
	}
	for i, f := range feeds {
		if f != want[i] {
			t.Fatalf("feed[%d] = %+v, want %+v", i, f, want[i])
		}
	}
}

// Title falls back to the title attribute when text is empty, and an
// outline without xmlUrl (a folder, or an empty category some readers
// export) is not a feed.
func TestParseTitleFallbackAndNonFeedOutlines(t *testing.T) {
	doc := `<?xml version="1.0"?>
<opml version="1.0"><body>
  <outline text="Folder with no feeds"/>
  <outline title="From Title Attr" xmlUrl="https://example.com/t"/>
  <outline text="No URL" type="rss"/>
</body></opml>`
	feeds, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("got %+v, want 1 feed", feeds)
	}
	if feeds[0].Title != "From Title Attr" || feeds[0].URL != "https://example.com/t" {
		t.Fatalf("feed = %+v", feeds[0])
	}
}

// Malformed XML is a hard error — the CLI reports it and writes
// nothing — while an empty body is a valid zero-feed document.
func TestParseMalformedAndEmpty(t *testing.T) {
	if _, err := Parse([]byte("<opml><body>")); err == nil {
		t.Fatal("expected error for malformed XML")
	}
	feeds, err := Parse([]byte(`<?xml version="1.0"?><opml version="2.0"><head/><body/></opml>`))
	if err != nil {
		t.Fatalf("Parse empty body: %v", err)
	}
	if len(feeds) != 0 {
		t.Fatalf("got %+v, want no feeds", feeds)
	}
	if _, err := Parse([]byte(`<notopml><body><outline xmlUrl="https://x"/></body></notopml>`)); err == nil {
		t.Fatal("expected error when root is not opml")
	}
	if !strings.Contains(func() string {
		_, err := Parse([]byte("no xml at all"))
		if err == nil {
			t.Fatal("expected error for non-XML input")
		}
		return err.Error()
	}(), "opml") {
		t.Fatal("error should mention opml")
	}
}

// Import merges OPML feeds into the subscription config: new URLs are
// added with deterministic keys (ASCII titles slug to
// "hacker-news"-style keys; CJK titles keep their host as key and
// their title as label), URLs already subscribed — in the config or
// earlier in the same document — are skipped, and feeds with no title
// are named after their host with a warning.
func TestImportAddsAndSkips(t *testing.T) {
	srcs := core.DefaultSources()
	srcs.RSS.Feeds = map[string]core.RSSFeed{
		"existing": {Label: "Existing", URL: "https://example.com/feed.xml"},
	}
	oc := Import(srcs, []Feed{
		{Title: "Hacker News", URL: "https://hnrss.org/frontpage"},
		{Title: "阮一峰周刊", URL: "https://www.ruanyifeng.com/blog/atom.xml"},
		{Title: "Duplicate", URL: "https://example.com/feed.xml"}, // already subscribed
		{Title: "Same twice", URL: "https://dup.example/feed"},    // dup within the doc
		{Title: "Same twice", URL: "https://dup.example/feed"},
	})
	if len(oc.Added) != 3 || len(oc.Skipped) != 2 {
		t.Fatalf("outcome = %+v, want 3 added / 2 skipped", oc)
	}
	if len(srcs.RSS.Feeds) != 4 {
		t.Fatalf("feeds = %v, want 4", srcs.RSS.Feeds)
	}
	byKey := map[string]core.RSSFeed{}
	for _, a := range oc.Added {
		byKey[a.Key] = core.RSSFeed{Label: a.Label, URL: a.URL}
	}
	if f := byKey["hacker-news"]; f.Label != "Hacker News" || f.URL != "https://hnrss.org/frontpage" {
		t.Fatalf("hacker-news = %+v", f)
	}
	// CJK title: key falls back to the host, label keeps the title.
	if f := byKey["www.ruanyifeng.com"]; f.Label != "阮一峰周刊" {
		t.Fatalf("ruanyifeng feed = %+v", f)
	}
	for _, s := range oc.Skipped {
		if s != "Duplicate" && s != "Same twice" {
			t.Fatalf("skipped = %q", s)
		}
	}

	// Re-importing the same document skips everything, and a titleless
	// duplicate is named by its URL, not an empty label.
	oc = Import(srcs, []Feed{{URL: "https://example.com/feed.xml"}})
	if len(oc.Skipped) != 1 || oc.Skipped[0] != "https://example.com/feed.xml" {
		t.Fatalf("titleless skip = %v, want the URL", oc.Skipped)
	}
}

// Two feeds sharing a title get distinct keys; a feed without a title
// is named after its host and flagged in NoTitle so the report can
// warn.
func TestImportKeyCollisionsAndMissingTitles(t *testing.T) {
	srcs := core.DefaultSources()
	oc := Import(srcs, []Feed{
		{Title: "Same", URL: "https://a.example/feed"},
		{Title: "Same", URL: "https://b.example/feed"},
		{URL: "https://c.example/atom.xml"}, // no title anywhere
		{URL: "not a url"},                  // no title, unparseable URL
	})
	keys := map[string]bool{}
	for _, a := range oc.Added {
		keys[a.Key] = true
	}
	if !keys["same"] || !keys["same-2"] {
		t.Fatalf("keys = %v, want same / same-2", keys)
	}
	if !keys["c.example"] {
		t.Fatalf("keys = %v, want host key c.example", keys)
	}
	if len(oc.NoTitle) != 2 {
		t.Fatalf("NoTitle = %v, want both titleless feeds", oc.NoTitle)
	}
	if f := srcs.RSS.Feeds["c.example"]; f.Label != "c.example" {
		t.Fatalf("titleless label = %+v, want host", f)
	}
}

// Export encodes the subscriptions as OPML 2.0 another reader can
// import: flat outlines (text = label, falling back to the key),
// xmlUrl, type="rss". Output is sorted by key so exports are stable
// regardless of map iteration order.
func TestEncodeEmitsStandardOPML(t *testing.T) {
	feeds := map[string]core.RSSFeed{
		"ruanyq":      {Label: "阮一峰周刊", URL: "https://www.ruanyifeng.com/blog/atom.xml"},
		"hacker-news": {URL: "https://hnrss.org/frontpage"}, // no label → key as text
	}
	raw := Encode(feeds)
	back, err := Parse(raw)
	if err != nil {
		t.Fatalf("re-parse own export: %v\n%s", err, raw)
	}
	if len(back) != 2 {
		t.Fatalf("re-parsed %+v, want 2 feeds", back)
	}
	// Sorted by key: hacker-news before ruanyq.
	if back[0].Title != "hacker-news" || back[0].URL != "https://hnrss.org/frontpage" {
		t.Fatalf("first = %+v, want hacker-news", back[0])
	}
	if back[1].Title != "阮一峰周刊" || back[1].URL != "https://www.ruanyifeng.com/blog/atom.xml" {
		t.Fatalf("second = %+v", back[1])
	}
	for _, want := range []string{
		`<opml version="2.0">`, `type="rss"`, `xmlUrl="https://hnrss.org/frontpage"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("export missing %q:\n%s", want, raw)
		}
	}
}

// The interop contract: what we export re-imports into a fresh config
// as the same subscription set — zero skipped, labels and URLs intact.
func TestRoundtripExportImport(t *testing.T) {
	srcs := core.DefaultSources()
	srcs.RSS.Feeds = map[string]core.RSSFeed{
		"ruanyq":      {Label: "阮一峰周刊", URL: "https://www.ruanyifeng.com/blog/atom.xml"},
		"hacker-news": {Label: "Hacker News", URL: "https://hnrss.org/frontpage"},
		"bare":        {URL: "https://bare.example/atom"}, // label omitted
	}
	back, err := Parse(Encode(srcs.RSS.Feeds))
	if err != nil {
		t.Fatalf("parse export: %v", err)
	}
	fresh := core.DefaultSources()
	oc := Import(fresh, back)
	if len(oc.Added) != 3 || len(oc.Skipped) != 0 {
		t.Fatalf("outcome = %+v, want everything back, nothing skipped", oc)
	}
	got := map[string]string{} // url → label
	for _, a := range oc.Added {
		got[a.URL] = a.Label
	}
	want := map[string]string{
		"https://www.ruanyifeng.com/blog/atom.xml": "阮一峰周刊",
		"https://hnrss.org/frontpage":              "Hacker News",
		"https://bare.example/atom":                "bare", // key became the label on export
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for url, label := range want {
		if got[url] != label {
			t.Fatalf("%s label = %q, want %q", url, got[url], label)
		}
	}
}

// Importing subscriptions implies wanting the RSS source visible: a
// config whose enabled list lacks "rss" gets it appended.
func TestImportEnablesRSS(t *testing.T) {
	srcs := &core.Sources{Enabled: []string{}} // explicit empty, honored by Load
	Import(srcs, []Feed{{Title: "F", URL: "https://f.example/feed"}})
	if len(srcs.Enabled) != 1 || srcs.Enabled[0] != "rss" {
		t.Fatalf("Enabled = %v, want [rss]", srcs.Enabled)
	}

	nilEnabled := &core.Sources{}
	Import(nilEnabled, nil)
	if len(nilEnabled.Enabled) != 1 || nilEnabled.Enabled[0] != "rss" {
		t.Fatalf("Enabled = %v, want [rss]", nilEnabled.Enabled)
	}
}
