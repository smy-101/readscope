package ui

// Ticket 05 UI tests: Ctrl+N on the list appends the feed's next page
// (no duplicate rows, read state kept, loading/failure/end feedback in
// the status bar), and F5 is feed-level — it re-pulls the selected
// feed's first page instead of the whole source, falling back to the
// whole source only where no feed is selected (the favorites view).

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/smy-101/readscope/internal/core"
)

// hnPageJSON builds one Algolia search response whose hits carry the
// given story ids, titled "Story <id>".
func hnPageJSON(ids ...string) string {
	hits := make([]string, len(ids))
	for i, id := range ids {
		hits[i] = fmt.Sprintf(`{"objectID":%q,"title":"Story %s","url":"https://example.com/s",`+
			`"author":"u","created_at_i":1700000000}`, id, id)
	}
	return `{"hits":[` + strings.Join(hits, ",") + `],"page":0,"nbPages":1,"hitsPerPage":1}`
}

// fakePagedAlgolia serves per-page fixtures for the front feed
// (page "" = 0) and counts requests per page under mu, so tests can
// assert exactly which page each key asked for.
type fakePagedAlgolia struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
}

func fakePagedAlgoliaFor(t *testing.T, pages map[string]string) *fakePagedAlgolia {
	t.Helper()
	f := &fakePagedAlgolia{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tags") != "front_page" {
			http.Error(w, "no fixture", http.StatusNotFound)
			return
		}
		p := r.URL.Query().Get("page")
		if p == "" {
			p = "0"
		}
		f.mu.Lock()
		f.requests = append(f.requests, p)
		body, ok := pages[p]
		f.mu.Unlock()
		if !ok {
			body = `{"hits":[],"page":99,"nbPages":1,"hitsPerPage":1}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakePagedAlgolia) pages() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int{}
	for _, p := range f.requests {
		out[p]++
	}
	return out
}

// The full ticket-05 tracer on the list layer: F5 pulls the selected
// feed's first page (one request — feed-level, not the whole source),
// Ctrl+N appends page 1 on top (a re-served story neither duplicates
// nor loses its read state), and the status bar narrates each step.
func TestListCtrlNAppendsNextPage(t *testing.T) {
	srv := fakePagedAlgoliaFor(t, map[string]string{
		"0": hnPageJSON("1001", "1002"),
		"1": hnPageJSON("1002", "1003"),
	})
	t.Setenv("READSCOPE_HN_BASE", srv.URL+"/api/v1")

	m := hnSeeded(t, "front", nil) // landed on the (empty) front list

	// F5: exactly one fetch — the selected feed's first page.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF5})
	if got := srv.pages(); got["0"] != 1 || len(got) != 1 {
		t.Fatalf("F5 dialed pages %v, want exactly one page-0 pull", got)
	}
	if !strings.Contains(m.View(), "Story 1001") {
		t.Fatalf("first page should list after F5:\n%s", m.View())
	}
	if err := m.st.SetRead(context.Background(), "hn:algolia:1002", true); err != nil {
		t.Fatalf("SetRead: %v", err)
	}

	// Ctrl+N: page 1 appends — 1003 arrives, 1002 re-serves in place.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN})
	if got := srv.pages(); got["1"] != 1 {
		t.Fatalf("Ctrl+N should pull page 1 once, dialed %v", got)
	}
	if !strings.Contains(m.View(), "Story 1003") {
		t.Fatalf("page 1 story missing after Ctrl+N:\n%s", m.View())
	}
	if len(m.items) != 3 {
		t.Fatalf("list rows = %d, want 3 — a re-served story must not duplicate", len(m.items))
	}
	read1002 := false
	for _, row := range m.items {
		if row.Item.ID == "hn:algolia:1002" {
			read1002 = row.Read
		}
	}
	if !read1002 {
		t.Fatal("the re-served story must stay read through a page append")
	}
	if !strings.Contains(m.status, "loaded 1 more") {
		t.Fatalf("status should report the append: %q", m.status)
	}
	if n, err := m.st.LoadPagesFetched(context.Background(), "hn", "front"); err != nil || n != 2 {
		t.Fatalf("depth after append = (%d, %v), want 2", n, err)
	}
}

// Ctrl+N at the index's end: the empty page reports "no more items"
// and the depth stays put — the retry semantics live in
// refresh.More's tests; here the status bar just has to say it.
func TestListCtrlNAtEndSaysNoMore(t *testing.T) {
	srv := fakePagedAlgoliaFor(t, map[string]string{"0": hnPageJSON("1001")})
	t.Setenv("READSCOPE_HN_BASE", srv.URL+"/api/v1")

	m := hnSeeded(t, "front", nil)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF5})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN}) // page 1: empty hits
	if !strings.Contains(m.status, "no more items") {
		t.Fatalf("status at the end = %q, want the no-more hint", m.status)
	}
	if n, _ := m.st.LoadPagesFetched(context.Background(), "hn", "front"); n != 1 {
		t.Fatalf("depth after end = %d, want 1 (end must not advance it)", n)
	}
}

// Ctrl+N on a source without pages (rss): the typed miss becomes the
// same "no more items" hint — a silent no-op would be a mystery key.
func TestCtrlNOnUnpagedRSSListSaysNoMore(t *testing.T) {
	m := seededModel(t, map[string]core.RSSFeed{"ex": {Label: "Example", URL: "http://127.0.0.1:1/x"}}, nil)
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // books → rss
	m = pump(t, m, cmd)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN})
	if !strings.Contains(m.status, "no more items") {
		t.Fatalf("status = %q, want the no-more hint", m.status)
	}
}

// F5 is feed-level: with feed b selected, only b's URL is dialed.
func TestF5RefreshesOnlySelectedFeed(t *testing.T) {
	rssXML := func(title, guid string) string {
		return fmt.Sprintf(`<?xml version="1.0"?><rss version="2.0"><channel><title>%s</title>
		<item><title>%s</title><guid>%s</guid><description>body</description></item>
		</channel></rss>`, title, title, guid)
	}
	var mu sync.Mutex
	aHits, bHits := 0, 0
	alpha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		aHits++
		mu.Unlock()
		w.Write([]byte(rssXML("Alpha story", "a1")))
	}))
	t.Cleanup(alpha.Close)
	beta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		bHits++
		mu.Unlock()
		w.Write([]byte(rssXML("Beta story", "b1")))
	}))
	t.Cleanup(beta.Close)

	m := seededModel(t, map[string]core.RSSFeed{
		"a": {Label: "Alpha", URL: alpha.URL},
		"b": {Label: "Beta", URL: beta.URL},
	}, nil)
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // books → rss (feed a)
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyDown}) // a → b
	m = pump(t, m, cmd)

	m = key(t, m, tea.KeyMsg{Type: tea.KeyF5})
	mu.Lock()
	a, b := aHits, bHits
	mu.Unlock()
	if a != 0 || b != 1 {
		t.Fatalf("F5 dialed alpha:%d beta:%d, want alpha:0 beta:1", a, b)
	}
	if !strings.Contains(m.View(), "Beta story") {
		t.Fatalf("the refreshed feed's story should list:\n%s", m.View())
	}
	if !strings.Contains(m.status, "refreshed: 1 new") {
		t.Fatalf("status = %q, want the refresh summary", m.status)
	}
}

// The favorites view has no current feed: F5 there keeps the M2
// whole-source behavior instead of breaking.
func TestF5InFavoritesRefreshesWholeSource(t *testing.T) {
	rssXML := func(title, guid string) string {
		return fmt.Sprintf(`<?xml version="1.0"?><rss version="2.0"><channel><title>%s</title>
		<item><title>%s</title><guid>%s</guid><description>body</description></item>
		</channel></rss>`, title, title, guid)
	}
	var mu sync.Mutex
	aHits, bHits := 0, 0
	serve := func(title, guid string, counter *int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			*counter++
			mu.Unlock()
			w.Write([]byte(rssXML(title, guid)))
		}))
	}
	alpha := serve("Alpha story", "a1", &aHits)
	t.Cleanup(alpha.Close)
	beta := serve("Beta story", "b1", &bHits)
	t.Cleanup(beta.Close)

	m := seededModel(t, map[string]core.RSSFeed{
		"a": {Label: "Alpha", URL: alpha.URL},
		"b": {Label: "Beta", URL: beta.URL},
	}, nil)
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // books → rss
	m = pump(t, m, cmd)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF3}) // favorites: no current feed
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF5})
	mu.Lock()
	a, b := aHits, bHits
	mu.Unlock()
	if a != 1 || b != 1 {
		t.Fatalf("favorites F5 dialed alpha:%d beta:%d, want both once (whole-source fallback)", a, b)
	}
}
