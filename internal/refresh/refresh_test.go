package refresh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/store"

	_ "github.com/smy-101/readscope/internal/source/rss" // registry: rss
)

// The M2 tracer bullet, end to end against httptest servers on
// loopback: config → fetch → SQLite → report, with one feed failing so
// isolation is exercised. No external network anywhere.
func TestRefreshTracerBullet(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>Good</title>
		<item><title>Alpha</title><guid>g1</guid><description>alpha body words</description></item>
		<item><title>Beta</title><guid>g2</guid><description>beta body</description></item>
		</channel></rss>`))
	}))
	t.Cleanup(good.Close)
	atom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Atom</title>
		<entry><id>e1</id><title>Entry</title><content>entry body</content></entry>
		</feed>`))
	}))
	t.Cleanup(atom.Close)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusNotFound)
	}))
	t.Cleanup(bad.Close)

	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg")) // Report's no_feeds path
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	srcs := core.DefaultSources()
	srcs.RSS.Feeds = map[string]core.RSSFeed{
		"good": {Label: "Good", URL: good.URL},
		"atom": {Label: "Atom", URL: atom.URL},
		"dead": {Label: "Dead", URL: bad.URL},
	}

	cat, err := i18n.Load("en")
	if err != nil {
		t.Fatalf("i18n: %v", err)
	}

	results, err := Source(context.Background(), st, srcs, "rss")
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	byFeed := map[string]Result{}
	for _, r := range results {
		byFeed[r.Feed] = r
	}
	if r := byFeed["good"]; r.Err != nil || r.New != 2 || r.Total != 2 {
		t.Fatalf("good = %+v", r)
	}
	if r := byFeed["atom"]; r.Err != nil || r.New != 1 || r.Total != 1 {
		t.Fatalf("atom = %+v", r)
	}
	if r := byFeed["dead"]; r.Err == nil || !strings.Contains(r.Err.Error(), "404") {
		t.Fatalf("dead should fail with status, got %+v", r)
	}

	// Report: per-feed lines in feed order, failures inline, summary.
	var out strings.Builder
	if Report(&out, results, cat) {
		t.Fatal("Report should be false when a feed failed")
	}
	text := out.String()
	for _, want := range []string{"Good: 2 new / 2 total", "Atom: 1 new / 1 total", "Dead: error:", "3 feeds: 2 ok, 1 failed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("report missing %q:\n%s", want, text)
		}
	}

	// Re-refresh: deterministic ids → 0 new, same totals.
	results2, err := Source(context.Background(), st, srcs, "rss")
	if err != nil {
		t.Fatalf("second Source: %v", err)
	}
	for _, r := range results2 {
		if r.Feed == "dead" {
			continue
		}
		wantTotal := map[string]int{"good": 2, "atom": 1}[r.Feed]
		if r.New != 0 || r.Total != wantTotal {
			t.Fatalf("second refresh should add nothing: %+v", r)
		}
	}

	// Offline readability: snapshot body searchable via FTS.
	ids, err := st.SearchFTS(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(ids) != 1 || ids[0] != "rss:good:g1" {
		t.Fatalf("fts ids = %v, want rss:good:g1", ids)
	}
}

func TestRefreshNoFeedsReportsHint(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg"))
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	results, err := Source(context.Background(), st, core.DefaultSources(), "rss")
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if results != nil {
		t.Fatalf("results = %v, want nil", results)
	}
	cat, _ := i18n.Load("en")
	var out strings.Builder
	if !Report(&out, results, cat) {
		t.Fatal("no-feeds report should succeed")
	}
	if !strings.Contains(out.String(), "sources.toml") {
		t.Fatalf("hint should name sources.toml: %q", out.String())
	}
}

func TestRefreshUnknownSource(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg"))
	if _, err := Source(context.Background(), nil, core.DefaultSources(), "nope"); err == nil {
		t.Fatal("expected error for unknown source")
	}
}

// The ticket-05 contract end to end: same server, two refreshes. The
// second carries the stored ETag, gets 304, and must skip parsing and
// item writes entirely while still bumping last_fetched. A later ETag
// change goes back to a full 200 update.
func TestRefreshConditionalSecondPassIs304(t *testing.T) {
	const body1 = `<?xml version="1.0"?><rss version="2.0"><channel><title>Cond</title>
	<item><title>One</title><guid>c1</guid><description>cond body</description></item>
	</channel></rss>`
	const body2 = `<?xml version="1.0"?><rss version="2.0"><channel><title>Cond</title>
	<item><title>One</title><guid>c1</guid><description>cond body</description></item>
	<item><title>Two</title><guid>c2</guid><description>second cond body</description></item>
	</channel></rss>`

	etag := `"v1"`
	body := body1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg"))
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	srcs := core.DefaultSources()
	srcs.RSS.Feeds = map[string]core.RSSFeed{"cond": {Label: "Cond", URL: srv.URL}}
	cat, _ := i18n.Load("en")
	ctx := context.Background()

	// First refresh: full fetch, one new item, validators stored.
	results, err := Source(ctx, st, srcs, "rss")
	if err != nil {
		t.Fatalf("first Source: %v", err)
	}
	if r := results[0]; r.Err != nil || r.New != 1 || r.Total != 1 || r.Unchanged {
		t.Fatalf("first refresh = %+v", r)
	}
	state, ok, err := st.LoadFetchState(ctx, "rss", "cond")
	if err != nil || !ok || state.ETag != `"v1"` {
		t.Fatalf("state after first refresh = (%+v, %v, %v)", state, ok, err)
	}
	firstFetched := state.LastFetched

	// Second refresh: 304 → Unchanged, counts straight from the cache,
	// no new writes; state re-saved with a bumped last_fetched.
	before, err := st.FeedItems(ctx, "rss", "cond", false)
	if err != nil || len(before) != 1 {
		t.Fatalf("items before 304 pass = (%d, %v)", len(before), err)
	}
	results2, err := Source(ctx, st, srcs, "rss")
	if err != nil {
		t.Fatalf("second Source: %v", err)
	}
	r2 := results2[0]
	if r2.Err != nil || !r2.Unchanged || r2.New != 0 || r2.Total != 1 {
		t.Fatalf("second refresh should be unchanged: %+v", r2)
	}
	state2, _, err := st.LoadFetchState(ctx, "rss", "cond")
	if err != nil || state2.ETag != `"v1"` {
		t.Fatalf("state after 304 = (%+v, %v)", state2, err)
	}
	if !state2.LastFetched.After(firstFetched) && !state2.LastFetched.Equal(firstFetched) {
		t.Fatalf("last_fetched should not go backwards: %v → %v", firstFetched, state2.LastFetched)
	}
	// DB-level proof of "no new writes": a 304 pass must not touch the
	// items snapshot at all — fetched_at is exactly what SaveFeedItems
	// would have bumped, and New==0 alone can't tell that apart.
	after, err := st.FeedItems(ctx, "rss", "cond", false)
	if err != nil || len(after) != 1 {
		t.Fatalf("items after 304 pass = (%d, %v)", len(after), err)
	}
	if !after[0].Item.FetchedAt.Equal(before[0].Item.FetchedAt) {
		t.Fatalf("304 pass rewrote the snapshot: fetched_at %v → %v",
			before[0].Item.FetchedAt, after[0].Item.FetchedAt)
	}

	// Report distinguishes new / unchanged / failed lines.
	var out strings.Builder
	if !Report(&out, results2, cat) {
		t.Fatal("all-304 report should succeed")
	}
	if text := out.String(); !strings.Contains(text, "Cond: unchanged") {
		t.Fatalf("report should mark the feed unchanged:\n%s", text)
	}

	// ETag change: back to a full 200 refresh with the new item.
	etag = `"v2"`
	body = body2
	results3, err := Source(ctx, st, srcs, "rss")
	if err != nil {
		t.Fatalf("third Source: %v", err)
	}
	if r := results3[0]; r.Err != nil || r.Unchanged || r.New != 1 || r.Total != 2 {
		t.Fatalf("refresh after etag change = %+v", r)
	}
	state3, _, err := st.LoadFetchState(ctx, "rss", "cond")
	if err != nil || state3.ETag != `"v2"` {
		t.Fatalf("state after v2 refresh = (%+v, %v)", state3, err)
	}
}

// A feed whose fetch fails keeps its stored validators: the next
// successful refresh still gets its conditional chance.
func TestRefreshFailureKeepsStoredState(t *testing.T) {
	const body = `<?xml version="1.0"?><rss version="2.0"><channel><title>F</title>
	<item><title>Only</title><guid>f1</guid><description>fail-test body</description></item>
	</channel></rss>`
	etag := `"e1"`
	serve := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !serve {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg"))
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	srcs := core.DefaultSources()
	srcs.RSS.Feeds = map[string]core.RSSFeed{"flaky": {Label: "Flaky", URL: srv.URL}}
	ctx := context.Background()

	if _, err := Source(ctx, st, srcs, "rss"); err != nil {
		t.Fatalf("first Source: %v", err)
	}

	// Server breaks: refresh fails, but the stored validators survive.
	serve = false
	results, err := Source(ctx, st, srcs, "rss")
	if err != nil {
		t.Fatalf("failing Source: %v", err)
	}
	if r := results[0]; r.Err == nil || r.Unchanged {
		t.Fatalf("broken server should be a failed feed: %+v", r)
	}
	state, ok, err := st.LoadFetchState(ctx, "rss", "flaky")
	if err != nil || !ok || state.ETag != `"e1"` {
		t.Fatalf("validators must survive a failed fetch: (%+v, %v, %v)", state, ok, err)
	}

	// Server recovers: the conditional headers still work (304).
	serve = true
	results2, err := Source(ctx, st, srcs, "rss")
	if err != nil {
		t.Fatalf("recovered Source: %v", err)
	}
	if r := results2[0]; r.Err != nil || !r.Unchanged {
		t.Fatalf("recovered refresh should 304: %+v", r)
	}
}
