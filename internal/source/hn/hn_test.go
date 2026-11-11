package hn

// Ticket 01 tests: the hn source package against a fake Algolia
// server (fixture JSON, offline) — registration, the fixed four
// feeds, hit→snapshot mapping, per-feed endpoints, error naming and
// missing-field robustness. The end-to-end refresh tracer lives in
// internal/refresh (TestRefreshHNTracerBullet).

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
)

// frontFixture is one Algolia search response: a link post, a text
// post (Ask-style, story_text), a bare hit with only title+objectID,
// and a ghost hit with no objectID at all (skipped, never a crash).
const frontFixture = `{
  "hits": [
    {"objectID": "1001", "title": "Why link posts win", "url": "https://example.com/article",
     "author": "alice", "points": 120, "num_comments": 45, "created_at_i": 1700000000},
    {"objectID": "1002", "title": "Ask HN: what do you read?", "author": "bob",
     "points": 3, "num_comments": 2, "created_at_i": 1700000600,
     "story_text": "<p>What is everyone reading?</p>"},
    {"objectID": "1003", "title": "Bare hit"},
    {"title": "Ghost hit"}
  ],
  "nbHits": 4, "page": 0, "nbPages": 1, "hitsPerPage": 20
}`

// fakeAlgolia serves fixture JSON keyed by the request's tags param
// and records every request line ("path?query") for endpoint asserts.
type fakeAlgolia struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
}

func fakeAlgoliaFor(t *testing.T, byTags map[string]string) *fakeAlgolia {
	t.Helper()
	f := &fakeAlgolia{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.URL.Path+"?"+r.URL.RawQuery)
		f.mu.Unlock()
		body, ok := byTags[r.URL.Query().Get("tags")]
		if !ok {
			http.Error(w, "no fixture for tags", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAlgolia) requested() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.requests))
	copy(out, f.requests)
	return out
}

// srcAt builds a source pointed at a fake base URL.
func srcAt(t *testing.T, base string) *Source {
	t.Helper()
	return newSource(base)
}

func TestRegisteredUnderHNID(t *testing.T) {
	// Self-registration is the "add a source = add a package" contract.
	if _, err := source.New(ID, core.DefaultSources()); err != nil {
		t.Fatalf("registry lookup: %v", err)
	}
}

func TestFeedsAreTheFixedFour(t *testing.T) {
	src := srcAt(t, "https://hn.algolia.com/api/v1")
	feeds := src.Feeds()
	wantKeys := []string{"front", "new", "ask", "show"}
	wantLabelKeys := []string{"hn.front", "hn.new", "hn.ask", "hn.show"}
	wantURLs := []string{
		"https://hn.algolia.com/api/v1/search?tags=front_page",
		"https://hn.algolia.com/api/v1/search_by_date?tags=story",
		"https://hn.algolia.com/api/v1/search?tags=ask_hn",
		"https://hn.algolia.com/api/v1/search?tags=show_hn",
	}
	if len(feeds) != len(wantKeys) {
		t.Fatalf("got %d feeds, want %d", len(feeds), len(wantKeys))
	}
	for i, f := range feeds {
		if f.Key != wantKeys[i] {
			t.Errorf("feed %d key = %q, want %q", i, f.Key, wantKeys[i])
		}
		if f.LabelKey != wantLabelKeys[i] {
			t.Errorf("feed %d labelKey = %q, want %q", i, f.LabelKey, wantLabelKeys[i])
		}
		if f.URL != wantURLs[i] {
			t.Errorf("feed %d url = %q, want %q", i, f.URL, wantURLs[i])
		}
	}
}

func TestFetchMapsHitsToSnapshotItems(t *testing.T) {
	srv := fakeAlgoliaFor(t, map[string]string{"front_page": frontFixture})
	src := srcAt(t, srv.URL+"/api/v1")
	items, err := src.Fetch(t.Context(), src.Feeds()[0])
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(items) != 3 { // the objectID-less ghost hit is skipped
		t.Fatalf("got %d items, want 3", len(items))
	}

	link := items[0]
	if link.ID != "hn:algolia:1001" || link.Source != "hn" || link.Feed != "front" {
		t.Fatalf("id/source/feed = %q/%q/%q", link.ID, link.Source, link.Feed)
	}
	if link.Title != "Why link posts win" || link.URL != "https://example.com/article" || link.Author != "alice" {
		t.Fatalf("mapping = %+v", link)
	}
	// Heat snapshot: points and comment count travel onto the item as
	// optionals, so "missing" stays distinct from a real zero.
	if link.Points == nil || *link.Points != 120 || link.NumComments == nil || *link.NumComments != 45 {
		t.Fatalf("heat snapshot = %v/%v, want 120/45", link.Points, link.NumComments)
	}
	// Link post: the body is the one-line article link.
	if link.Body != "https://example.com/article" {
		t.Fatalf("link post body = %q, want the article url", link.Body)
	}
	if link.PublishedAt == nil || !link.PublishedAt.Equal(time.Unix(1700000000, 0).UTC()) {
		t.Fatalf("PublishedAt = %v, want created_at_i", link.PublishedAt)
	}

	text := items[1]
	// Text post: the body is story_text (HTML, for the render pipeline).
	if text.Body != "<p>What is everyone reading?</p>" {
		t.Fatalf("text post body = %q, want story_text", text.Body)
	}
	if text.URL != "" {
		t.Fatalf("text post should carry no url, got %q", text.URL)
	}

	bare := items[2]
	// Degenerate hit: missing fields map to empty, never a crash.
	if bare.URL != "" || bare.Author != "" || bare.Body != "" || bare.PublishedAt != nil {
		t.Fatalf("bare hit should map to zero optionals: %+v", bare)
	}
	// A hit without points/comments maps to nil heat — the list drops
	// those meta segments rather than rendering "0 pts".
	if bare.Points != nil || bare.NumComments != nil {
		t.Fatalf("bare hit must keep nil heat: %v/%v", bare.Points, bare.NumComments)
	}
}

func TestFetchHitsEachFeedEndpoint(t *testing.T) {
	every := map[string]string{
		"front_page": frontFixture,
		"story":      frontFixture,
		"ask_hn":     frontFixture,
		"show_hn":    frontFixture,
	}
	srv := fakeAlgoliaFor(t, every)
	src := srcAt(t, srv.URL+"/api/v1")
	want := map[string]string{
		"front": "/api/v1/search?tags=front_page",
		"new":   "/api/v1/search_by_date?tags=story",
		"ask":   "/api/v1/search?tags=ask_hn",
		"show":  "/api/v1/search?tags=show_hn",
	}
	for _, f := range src.Feeds() {
		if _, err := src.Fetch(t.Context(), f); err != nil {
			t.Fatalf("Fetch %s: %v", f.Key, err)
		}
	}
	got := srv.requested()
	if len(got) != 4 {
		t.Fatalf("requests = %v, want 4", got)
	}
	for i, f := range src.Feeds() {
		if got[i] != want[f.Key] {
			t.Errorf("feed %s hit %q, want %q", f.Key, got[i], want[f.Key])
		}
	}
}

func TestFeedsDeclareRankedListSort(t *testing.T) {
	src := srcAt(t, "https://hn.algolia.com/api/v1")
	// The popularity endpoints rank by points (their return order),
	// New stays chronological.
	want := map[string]core.ListSort{
		"front": core.ListSortPoints,
		"new":   core.ListSortTime,
		"ask":   core.ListSortPoints,
		"show":  core.ListSortPoints,
	}
	for _, f := range src.Feeds() {
		if f.ListSort != want[f.Key] {
			t.Errorf("feed %s ListSort = %q, want %q", f.Key, f.ListSort, want[f.Key])
		}
	}
}

func TestFetchHTTPFailureNamesFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	src := srcAt(t, srv.URL+"/api/v1")
	_, err := src.Fetch(t.Context(), src.Feeds()[0])
	if err == nil || !strings.Contains(err.Error(), "front") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want error naming feed and status, got: %v", err)
	}
}

func TestFetchGarbageIsParseError(t *testing.T) {
	srv := fakeAlgoliaFor(t, map[string]string{"front_page": "<html>not json</html>"})
	src := srcAt(t, srv.URL+"/api/v1")
	_, err := src.Fetch(t.Context(), src.Feeds()[0])
	if err == nil || !strings.Contains(err.Error(), "parse front") {
		t.Fatalf("want parse error naming feed, got: %v", err)
	}
}

func TestFetchUnknownFeedKeyIsError(t *testing.T) {
	src := srcAt(t, "https://hn.algolia.com/api/v1")
	_, err := src.Fetch(t.Context(), core.Feed{Key: "best"})
	if err == nil || !strings.Contains(err.Error(), "best") {
		t.Fatalf("unknown feed key should error naming it, got: %v", err)
	}
}

// hitFixture builds a one-hit response with distinct ids per feed, so
// the refresh-level test in internal/refresh can assert per-feed
// counts without ids colliding across feeds.
func hitFixture(storyID, title string) string {
	return fmt.Sprintf(`{"hits":[{"objectID":%q,"title":%q,"url":"https://example.com/s",`+
		`"author":"u","created_at_i":1700000000}],"nbHits":1,"page":0,"nbPages":1,"hitsPerPage":20}`,
		storyID, title)
}
