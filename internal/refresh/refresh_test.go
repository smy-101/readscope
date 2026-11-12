package refresh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/store"

	_ "github.com/smy-101/readscope/internal/source/hn"    // registry: hn
	_ "github.com/smy-101/readscope/internal/source/rss"   // registry: rss
	_ "github.com/smy-101/readscope/internal/source/zhihu" // registry: zhihu
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

// M3 ticket 05: F5 is feed-level. Feed refreshes exactly the named
// feed — one Result, other feeds never dialed — through the same
// per-feed path Source walks (conditional replay included), and a
// paged source's depth floors at 1 so Ctrl+N walks on from page 1.
func TestFeedRefreshesOnlyTheNamedFeed(t *testing.T) {
	const alphaXML = `<?xml version="1.0"?><rss version="2.0"><channel><title>A</title>
	<item><title>Alpha one</title><guid>a1</guid><description>alpha body</description></item>
	</channel></rss>`
	const betaXML = `<?xml version="1.0"?><rss version="2.0"><channel><title>B</title>
	<item><title>Beta one</title><guid>b1</guid><description>beta body</description></item>
	</channel></rss>`

	var mu sync.Mutex
	alphaHits, betaHits := 0, 0
	alpha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		alphaHits++
		mu.Unlock()
		w.Write([]byte(alphaXML))
	}))
	t.Cleanup(alpha.Close)
	beta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		betaHits++
		mu.Unlock()
		w.Write([]byte(betaXML))
	}))
	t.Cleanup(beta.Close)

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
	srcs.RSS.Feeds = map[string]core.RSSFeed{
		"alpha": {Label: "Alpha", URL: alpha.URL},
		"beta":  {Label: "Beta", URL: beta.URL},
	}
	ctx := context.Background()

	results, err := Feed(ctx, st, srcs, "rss", "beta")
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want exactly 1", len(results))
	}
	r := results[0]
	if r.Err != nil || r.Feed != "beta" || r.Label != "Beta" || r.New != 1 || r.Total != 1 {
		t.Fatalf("beta result = %+v", r)
	}
	mu.Lock()
	a, b := alphaHits, betaHits
	mu.Unlock()
	if a != 0 || b != 1 {
		t.Fatalf("dial counts = alpha:%d beta:%d, want alpha:0 beta:1 — F5 must not touch the other feeds", a, b)
	}

	// Unknown feed key is an error naming it, not a silent no-op.
	if _, err := Feed(ctx, st, srcs, "rss", "nope"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown feed key should error naming it, got: %v", err)
	}

	// Unknown source id still errors like Source does.
	if _, err := Feed(ctx, st, srcs, "nope", "beta"); err == nil {
		t.Fatal("unknown source should error")
	}
}

// The paging floor: a feed-level refresh of a paged source puts the
// first page in the cache and the depth at 1 — never lower, never
// higher no matter how many times it reruns.
func TestFeedRefreshSetsPagingFloor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"hits":[{"objectID":"1001","title":"Floor story",` +
			`"url":"https://example.com/s","author":"u","created_at_i":1700000000}],` +
			`"nbHits":1,"page":0,"nbPages":1,"hitsPerPage":20}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("READSCOPE_HN_BASE", srv.URL+"/api/v1")

	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg"))
	st, err := store.Open(filepath.Join(t.TempDir(), "hn.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()

	if n, _ := st.LoadPagesFetched(ctx, "hn", "front"); n != 0 {
		t.Fatalf("depth before any refresh = %d, want 0", n)
	}
	if _, err := Feed(ctx, st, core.DefaultSources(), "hn", "front"); err != nil {
		t.Fatalf("Feed(hn/front): %v", err)
	}
	if n, err := st.LoadPagesFetched(ctx, "hn", "front"); err != nil || n != 1 {
		t.Fatalf("depth after first-page refresh = (%d, %v), want 1", n, err)
	}
	if _, err := Feed(ctx, st, core.DefaultSources(), "hn", "front"); err != nil {
		t.Fatalf("second Feed: %v", err)
	}
	if n, err := st.LoadPagesFetched(ctx, "hn", "front"); err != nil || n != 1 {
		t.Fatalf("refresh must not grow the depth: (%d, %v), want 1", n, err)
	}
	// The other feeds were not part of the pull: their depth stays 0.
	if n, _ := st.LoadPagesFetched(ctx, "hn", "new"); n != 0 {
		t.Fatalf("untouched feed depth = %d, want 0", n)
	}
}

// M3 ticket 05: Ctrl+N append. More walks the stored depth: page N is
// fetched and merged through the upsert (no duplicate rows, read
// state survives — Algolia pages shift, so a later page re-serving a
// known id is normal), and the depth advances only when the page had
// hits. An empty page reports end without moving the depth, so a
// later Ctrl+N retries the same page once the index grows.
func TestMoreAppendsNextPage(t *testing.T) {
	hit := func(id, title string) string {
		return fmt.Sprintf(`{"objectID":%q,"title":%q,"url":"https://example.com/s",`+
			`"author":"u","points":5,"created_at_i":1700000000}`, id, title)
	}
	page := func(pageNo int, ids ...string) string {
		hits := make([]string, len(ids))
		for i, id := range ids {
			hits[i] = hit(id, id+" story")
		}
		return fmt.Sprintf(`{"hits":[%s],"nbHits":%d,"page":%d,"nbPages":3,"hitsPerPage":1}`,
			strings.Join(hits, ","), len(ids), pageNo)
	}
	// page 0: two stories; page 1: one old (1002, re-served as pages
	// shift) + one new; page 2: empty until the index "grows".
	page2Grew := false
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		grew := page2Grew
		mu.Unlock()
		p := r.URL.Query().Get("page")
		tags := r.URL.Query().Get("tags")
		if tags != "front_page" {
			http.Error(w, "no fixture", http.StatusNotFound)
			return
		}
		body := page(2) // empty hits: the index's end
		switch p {
		case "", "0":
			body = page(0, "1001", "1002")
		case "1":
			body = page(1, "1002", "1003")
		case "2":
			if grew {
				body = page(2, "1004")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("READSCOPE_HN_BASE", srv.URL+"/api/v1")

	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg"))
	st, err := store.Open(filepath.Join(t.TempDir(), "more.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	srcs := core.DefaultSources()
	ctx := context.Background()

	// First page lands via a feed refresh (F5): depth 1, two rows.
	if _, err := Feed(ctx, st, srcs, "hn", "front"); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if err := st.SetRead(ctx, "hn:algolia:1002", true); err != nil {
		t.Fatalf("SetRead: %v", err)
	}

	// Ctrl+N: page 1 appends — one new row (1003), 1002 re-served but
	// not duplicated, its read state intact.
	res, end, err := More(ctx, st, srcs, "hn", "front")
	if err != nil || end {
		t.Fatalf("first More = (res %+v, end %v, err %v)", res, end, err)
	}
	if res.New != 1 || res.Total != 3 {
		t.Fatalf("first More = %+v, want New 1 / Total 3", res)
	}
	if n, err := st.LoadPagesFetched(ctx, "hn", "front"); err != nil || n != 2 {
		t.Fatalf("depth after append = (%d, %v), want 2", n, err)
	}
	rows, err := st.FeedItems(ctx, "hn", "front", false)
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows after append = (%d, %v), want 3 — no duplicates", len(rows), err)
	}
	read1002 := false
	for _, r := range rows {
		if r.Item.ID == "hn:algolia:1002" {
			read1002 = r.Read
		}
	}
	if !read1002 {
		t.Fatal("the re-served story must stay read — paging never touches read state")
	}

	// Ctrl+N at the index's end: empty page, end reported, depth kept.
	res, end, err = More(ctx, st, srcs, "hn", "front")
	if err != nil || !end {
		t.Fatalf("second More = (res %+v, end %v, err %v), want end", res, end, err)
	}
	if n, _ := st.LoadPagesFetched(ctx, "hn", "front"); n != 2 {
		t.Fatalf("depth after end = %d, want 2 (end must not advance it)", n)
	}

	// The index grows: the same Ctrl+N retries page 2 and loads it.
	mu.Lock()
	page2Grew = true
	mu.Unlock()
	res, end, err = More(ctx, st, srcs, "hn", "front")
	if err != nil || end {
		t.Fatalf("third More = (res %+v, end %v, err %v)", res, end, err)
	}
	if res.New != 1 || res.Total != 4 {
		t.Fatalf("third More = %+v, want New 1 / Total 4", res)
	}
	if n, _ := st.LoadPagesFetched(ctx, "hn", "front"); n != 3 {
		t.Fatalf("depth after retry = %d, want 3", n)
	}
}

// Ctrl+N on a source without pages is a typed miss, not a fetch: the
// UI turns it into the "no more items" hint.
func TestMoreOnUnpagedSourceIsErrNotPaged(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg"))
	st, err := store.Open(filepath.Join(t.TempDir(), "more.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_, _, err = More(context.Background(), st, core.DefaultSources(), "rss", "whatever")
	if !errors.Is(err, ErrNotPaged) {
		t.Fatalf("want ErrNotPaged, got: %v", err)
	}
}

// A feed key the source doesn't know is an error naming it, matching
// Feed's contract.
func TestMoreUnknownFeedKeyIsError(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "cfg"))
	st, err := store.Open(filepath.Join(t.TempDir(), "more.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_, _, err = More(context.Background(), st, core.DefaultSources(), "hn", "best")
	if err == nil || !strings.Contains(err.Error(), "best") {
		t.Fatalf("unknown feed key should error naming it, got: %v", err)
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

// The M3 ticket 01 tracer bullet for HN: one refresh cycle over a fake
// Algolia index — four feeds fetched, snapshotted, counted — then a
// second cycle proving refreshes are idempotent and read state never
// moves. READSCOPE_HN_BASE points the hn source at the fake server;
// no external network anywhere.
func TestRefreshHNTracerBullet(t *testing.T) {
	points := 10 // mutated between cycles: a refresh updates the heat snapshot
	hit := func(id, title string) string {
		return fmt.Sprintf(`{"hits":[{"objectID":%q,"title":%q,"url":"https://example.com/s",`+
			`"author":"u","points":%d,"num_comments":3,"created_at_i":1700000000}],`+
			`"nbHits":1,"page":0,"nbPages":1,"hitsPerPage":20}`,
			id, title, points)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixtures := map[string]string{
			"front_page": hit("1001", "Front story"),
			"story":      hit("2001", "New story"),
			"ask_hn":     hit("3001", "Ask story"),
			"show_hn":    hit("4001", "Show story"),
		}
		body, ok := fixtures[r.URL.Query().Get("tags")]
		if !ok {
			http.Error(w, "no fixture", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("READSCOPE_HN_BASE", srv.URL+"/api/v1")

	st, err := store.Open(filepath.Join(t.TempDir(), "hn.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	srcs := core.DefaultSources()
	results, err := Source(context.Background(), st, srcs, "hn")
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("got %d results, want 4 (one per fixed feed)", len(results))
	}
	byFeed := map[string]Result{}
	for _, r := range results {
		if r.Err != nil || r.New != 1 || r.Total != 1 {
			t.Fatalf("%s = %+v", r.Feed, r)
		}
		byFeed[r.Feed] = r
	}
	if _, ok := byFeed["front"]; !ok {
		t.Fatal("front feed missing from results")
	}

	// Reading a story, then refreshing again (with the story's heat
	// having moved): no new rows, the read dot survives, and the heat
	// snapshot picks up the fetched-at values.
	if err := st.SetRead(context.Background(), "hn:algolia:1001", true); err != nil {
		t.Fatalf("SetRead: %v", err)
	}
	points = 25
	results2, err := Source(context.Background(), st, srcs, "hn")
	if err != nil {
		t.Fatalf("second Source: %v", err)
	}
	for _, r := range results2 {
		if r.Err != nil || r.New != 0 || r.Total != 1 {
			t.Fatalf("second refresh should add nothing: %+v", r)
		}
	}
	rows, err := st.FeedItems(context.Background(), "hn", "front", false)
	if err != nil || len(rows) != 1 {
		t.Fatalf("FeedItems(front) = %d rows, %v", len(rows), err)
	}
	if !rows[0].Read {
		t.Fatal("read state must survive a refresh")
	}
	if rows[0].Item.ID != "hn:algolia:1001" {
		t.Fatalf("item id = %q, want hn:algolia:1001", rows[0].Item.ID)
	}
	if rows[0].Item.Points == nil || *rows[0].Item.Points != 25 ||
		rows[0].Item.NumComments == nil || *rows[0].Item.NumComments != 3 {
		t.Fatalf("heat snapshot not refreshed: %+v", rows[0].Item)
	}
}

// The M4 ticket-02 tracer bullet: zhihu registration → anonymous hot
// fetch → snapshot upsert → cached list, against one fake zhihu on
// loopback. A second refresh with the heat having moved: no new rows,
// read state untouched, the heat snapshot refreshed in place.
func TestRefreshZhihuTracerBullet(t *testing.T) {
	heat := 2045 // 万, mutated between cycles
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/topstory/hot-list" {
			http.Error(w, "no fixture", http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `{"data":[
		 {"detail_text":"%d 万热度","target":{"id":18984420158198241699,"type":"question",
		  "title":"为什么终端阅读器又火了？","url":"https://api.zhihu.com/questions/18984420158198241699"}},
		 {"detail_text":"","target":{"id":100001,"type":"question","title":"置顶卡没有热度列",
		  "url":"https://api.zhihu.com/questions/100001"}}]}`, heat)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("READSCOPE_ZHIHU_BASE", srv.URL)

	st, err := store.Open(filepath.Join(t.TempDir(), "zhihu.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	srcs := core.DefaultSources()
	results, err := Source(context.Background(), st, srcs, "zhihu")
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1 (the hot feed)", len(results))
	}
	r := results[0]
	if r.Err != nil || r.New != 2 || r.Total != 2 || r.Feed != "hot" {
		t.Fatalf("hot result = %+v", r)
	}

	// Reading the top question, then refreshing with its heat moved:
	// same ids → no new rows, the read dot survives, the heat snapshot
	// picks up the new value.
	if err := st.SetRead(context.Background(), "zh:question/18984420158198241699", true); err != nil {
		t.Fatalf("SetRead: %v", err)
	}
	heat = 3000
	results2, err := Source(context.Background(), st, srcs, "zhihu")
	if err != nil {
		t.Fatalf("second Source: %v", err)
	}
	if r := results2[0]; r.Err != nil || r.New != 0 || r.Total != 2 {
		t.Fatalf("second refresh should add nothing: %+v", r)
	}
	rows, err := st.FeedItems(context.Background(), "zhihu", "hot", false)
	if err != nil || len(rows) != 2 {
		t.Fatalf("FeedItems(hot) = %d rows, %v", len(rows), err)
	}
	// The store's fallback order is fetch-time then id — the UI re-ranks
	// by the points snapshot (ListSortPoints), so find the row by id.
	var top store.ItemRow
	for _, row := range rows {
		if row.Item.ID == "zh:question/18984420158198241699" {
			top = row
		}
	}
	if top.Item.ID == "" {
		t.Fatalf("top question missing from cache: %+v", rows)
	}
	if !top.Read {
		t.Fatal("read state must survive a refresh")
	}
	if top.Item.Points == nil || *top.Item.Points != 30000000 {
		t.Fatalf("heat snapshot not refreshed: %+v", top.Item)
	}
}
