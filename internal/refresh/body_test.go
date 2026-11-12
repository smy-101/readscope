package refresh

// body_test.go — the refresh half of the lazy-body flow (ticket 03):
// refresh.Body fetches one item's full text through the source's
// BodySource extension and persists it into the body snapshot, from
// where re-opens render offline with zero requests.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/store"

	_ "github.com/smy-101/readscope/internal/source/zhihu" // register "zhihu"
)

// storyServer serves the daily story door for one story id.
func storyServer(t *testing.T, storyID, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/4/story/"+storyID {
			fmt.Fprintf(w, `{"title":"t","body":%q}`, body)
			return
		}
		http.Error(w, "no fixture", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBodyFetchesAndPersistsSnapshot(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", t.TempDir())
	t.Setenv("READSCOPE_ZHIHU_BASE", storyServer(t, "9792937", "<p>full story words</p>").URL)

	st, err := store.Open(filepath.Join(t.TempDir(), "body.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	it := core.Item{
		ID: "zh:daily/9792937", Source: "zhihu", Feed: "daily",
		Title: "机器知道它在读什么吗", URL: "https://daily.zhihu.com/story/9792937",
	}
	if _, _, err := st.SaveFeedItems(context.Background(), "zhihu", "daily", []core.Item{it}); err != nil {
		t.Fatalf("seed list row: %v", err)
	}

	got, err := Body(context.Background(), st, core.DefaultSources(), it)
	if err != nil {
		t.Fatalf("Body: %v", err)
	}
	if !strings.Contains(got, "full story words") {
		t.Fatalf("body = %q", got)
	}
	// Persisted: the snapshot and the FTS index both carry the text.
	rows, err := st.FeedItems(context.Background(), "zhihu", "daily", false)
	if err != nil || len(rows) != 1 {
		t.Fatalf("FeedItems = %d rows, %v", len(rows), err)
	}
	if rows[0].Item.Body == "" {
		t.Fatal("body not persisted into the snapshot")
	}
	if ids, _ := st.SearchFTS(context.Background(), "story"); len(ids) != 1 {
		t.Fatalf("fetched body not indexed: %v", ids)
	}
}

func TestBodyNamesNonBodySources(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "body.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	// rss rides its body in the list snapshot: a clean named error, the
	// same shape as Detail's refusal.
	_, err = Body(context.Background(), st, core.DefaultSources(),
		core.Item{ID: "rss:ex:1", Source: "rss", Feed: "ex"})
	if err == nil || !strings.Contains(err.Error(), "no on-demand body") {
		t.Fatalf("want named refusal, got: %v", err)
	}
}

// A failed fetch leaves the snapshot untouched — the offline-reading
// contract: a previously fetched body keeps reading even when the
// story door dies (and a first-open failure leaves a clean empty).
func TestBodyFailureLeavesCacheUntouched(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("READSCOPE_ZHIHU_BASE", srv.URL)

	st, err := store.Open(filepath.Join(t.TempDir(), "body.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Seed the row WITH a cached body (the second-open scenario).
	cached := core.Item{
		ID: "zh:daily/9792937", Source: "zhihu", Feed: "daily",
		Title: "t", Body: "<p>cached story</p>",
	}
	if _, _, err := st.SaveFeedItems(context.Background(), "zhihu", "daily", []core.Item{cached}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err = Body(context.Background(), st, core.DefaultSources(), cached)
	if err == nil {
		t.Fatal("fetch should fail against the 500 door")
	}
	rows, _ := st.FeedItems(context.Background(), "zhihu", "daily", false)
	if len(rows) != 1 || rows[0].Item.Body != "<p>cached story</p>" {
		t.Fatalf("cached body must survive a failed fetch: %+v", rows)
	}
	// The error carries the HTTP status — a fetch failure, not a
	// store failure.
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should carry the http status: %v", err)
	}
}
