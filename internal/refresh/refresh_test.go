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
