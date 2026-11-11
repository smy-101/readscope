package ui

// Ticket 04 UI tests: the three-layer detail over a real store and a
// fake Algolia — body → floors → replies driven through the real
// Update loop. The pure flattening is covered in floors_test.go; these
// pin what a user sees: layer transitions with state kept, floor
// headers, the loading/refresh lifecycle, offline re-opens, per-layer
// Ctrl+O/Ctrl+Y, and in-memory Ctrl+N paging.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/render"

	_ "github.com/smy-101/readscope/internal/source/hn" // register "hn", as main does
)

// hnDetailJSON is story 1001's /items payload: a link post with three
// floors — alice (rich HTML body, two-reply subtree), a deleted
// placeholder with one child, erin (no replies). ages land ~2h ago.
func hnDetailJSON() string {
	age := time.Now().UTC().Add(-2 * time.Hour).Unix()
	return fmt.Sprintf(`{
	  "id": 1001, "author": "poster", "title": "Hot story", "points": 42,
	  "children": [
	    {"id": 10, "author": "alice", "created_at_i": %d, "parent_id": 1001,
	     "text": "<p>first para</p><blockquote><p>quoted words</p></blockquote><p>go <a href=\"https://example.com/x\">a link</a></p><p><img src=\"i.png\" alt=\"pic\"></p><p>tail para that keeps the floor body long enough to scroll when the terminal is short</p>",
	     "children": [
	       {"id": 11, "author": "bob", "created_at_i": %d, "parent_id": 10,
	        "text": "<p>direct reply from bob</p>",
	        "children": [
	          {"id": 111, "author": "carol", "created_at_i": %d, "parent_id": 11,
	           "text": "<p>deep reply from carol</p><p>filler paragraph 1 to stretch the reply layer past one screen</p><p>filler paragraph 2 to stretch the reply layer past one screen</p><p>filler paragraph 3 to stretch the reply layer past one screen</p><p>filler paragraph 4 to stretch the reply layer past one screen</p><p>filler paragraph 5 to stretch the reply layer past one screen</p>", "children": []}
	        ]}
	     ]},
	    {"id": 12, "deleted": true, "parent_id": 1001, "children": [
	       {"id": 121, "author": "dave", "created_at_i": %d, "parent_id": 12,
	        "text": "<p>reply under the deleted floor</p>", "children": [
	          {"id": 1211, "author": "frank", "created_at_i": %d, "parent_id": 121,
	           "text": "<p>deep under a deleted parent</p>", "children": []}
	        ]}
	    ]},
	    {"id": 13, "author": "erin", "created_at_i": %d, "parent_id": 1001,
	     "text": "<p>floor without replies</p>", "children": []}
	  ]
	}`, age, age, age, age, age, age)
}

// hnDetailServer fakes Algolia's /items endpoint for story 1001 with
// the given payload (tests swap payloads to simulate refreshes).
func hnDetailServer(t *testing.T, json string) (url string, hits *int) {
	t.Helper()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/items/1001" {
			n++
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, json)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/api/v1", &n
}

// hnStoryRow seeds the link post into the hn front feed.
func hnStoryRow() core.Item {
	return core.Item{
		ID: "hn:algolia:1001", Source: "hn", Feed: "front",
		Title: "Hot story", URL: "https://example.com/article",
		Author: "poster", FetchedAt: time.Now().UTC(),
		Body: "https://example.com/article",
	}
}

// openHNDetailCmd lands on the hn list with the story seeded and
// opens the reader (body layer), returning the still-unpumped open
// command — tests that inspect the cache-first load mid-flight drive
// it from there. openHNDetail is the pump-to-quiescence wrapper.
func openHNDetailCmd(t *testing.T, seedCache []core.Comment, base string) (Model, tea.Cmd) {
	t.Helper()
	m := seededModel(t, nil, nil)
	if _, _, err := m.st.SaveFeedItems(context.Background(), "hn", "front", []core.Item{hnStoryRow()}); err != nil {
		t.Fatalf("seed story: %v", err)
	}
	if seedCache != nil {
		if err := m.st.SaveComments(context.Background(), "hn:algolia:1001", seedCache); err != nil {
			t.Fatalf("seed comments: %v", err)
		}
	}
	t.Setenv("READSCOPE_HN_BASE", base)
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // books → rss
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // rss → hn
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // sidebar → list
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // open the story
	if m.reader == nil {
		t.Fatal("Enter on the hn row must open the reader")
	}
	return m, cmd
}

func openHNDetail(t *testing.T, seedCache []core.Comment, base string) Model {
	t.Helper()
	m, cmd := openHNDetailCmd(t, seedCache, base)
	return pump(t, m, cmd)
}

// cachedTree is hnDetailJSON's comment set as it lands in the cache
// (the hn fetch's flattening, pinned by source tests): floors in
// payload order, each followed by its subtree.
func cachedTree() []core.Comment {
	age := time.Now().UTC().Add(-2 * time.Hour)
	at := func() *time.Time { t := age; return &t }
	return []core.Comment{
		{ID: "10", Author: "alice", CreatedAt: at(),
			Text: "<p>first para</p><blockquote><p>quoted words</p></blockquote><p>go <a href=\"https://example.com/x\">a link</a></p><p><img src=\"i.png\" alt=\"pic\"></p><p>tail para that keeps the floor body long enough to scroll when the terminal is short</p>"},
		{ID: "11", ParentID: "10", Author: "bob", CreatedAt: at(), Text: "<p>direct reply from bob</p>"},
		{ID: "111", ParentID: "11", Author: "carol", CreatedAt: at(), Text: "<p>deep reply from carol</p><p>filler paragraph 1 to stretch the reply layer past one screen</p><p>filler paragraph 2 to stretch the reply layer past one screen</p><p>filler paragraph 3 to stretch the reply layer past one screen</p><p>filler paragraph 4 to stretch the reply layer past one screen</p><p>filler paragraph 5 to stretch the reply layer past one screen</p>"},
		{ID: "12"},
		{ID: "121", ParentID: "12", Author: "dave", CreatedAt: at(), Text: "<p>reply under the deleted floor</p>"},
		{ID: "1211", ParentID: "121", Author: "frank", CreatedAt: at(), Text: "<p>deep under a deleted parent</p>"},
		{ID: "13", Author: "erin", CreatedAt: at(), Text: "<p>floor without replies</p>"},
	}
}

// Three layers in, three layers out — and every layer hands back the
// state it had: the body keeps its scroll, the floor layer keeps its
// floor, replyless floors refuse Enter.
func TestThreeLayerNavigationKeepsState(t *testing.T) {
	base, _ := hnDetailServer(t, hnDetailJSON())
	m := openHNDetail(t, nil, base)
	d := m.reader
	if d.layer != layerBody || !d.loaded || len(d.floors) != 3 {
		t.Fatalf("setup: layer=%v loaded=%v floors=%d", d.layer, d.loaded, len(d.floors))
	}

	// Body: the link post shows its one-line link + the floor count.
	view := m.View()
	if !strings.Contains(view, "https://example.com/article") || !strings.Contains(view, "3 floors") {
		t.Fatalf("body layer missing link line / floor footer:\n%s", view)
	}
	m = size(m, 100, 12) // short terminal: a floor's body outscrolls one screen
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	bodyTop := m.reader.top

	// Enter → floor 1: header + pipeline-rendered body.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.reader.layer != layerFloors || m.reader.floorIdx != 0 {
		t.Fatalf("Enter must open floor 1: layer=%v idx=%d", m.reader.layer, m.reader.floorIdx)
	}
	view = m.View()
	if !strings.Contains(view, "alice · 2 replies") {
		t.Fatalf("floor 1 header wrong:\n%s", view)
	}
	if !strings.Contains(view, "floor 1/3") {
		t.Fatalf("status bar should report the floor position:\n%s", view)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyDown}) // scroll inside the floor
	floorTop := m.reader.floorTop
	if floorTop == 0 {
		t.Fatal("setup: floor body should scroll at this height")
	}

	// Esc → body: same reading position; Enter again → same floor, same scroll.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.reader.layer != layerBody || m.reader.top != bodyTop {
		t.Fatalf("body scroll lost: layer=%v top=%d want %d", m.reader.layer, m.reader.top, bodyTop)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.reader.layer != layerFloors || m.reader.floorIdx != 0 || m.reader.floorTop != floorTop {
		t.Fatalf("floor focus lost: idx=%d top=%d want %d/%d",
			m.reader.floorIdx, m.reader.floorTop, 0, floorTop)
	}

	// → floor 2 (deleted): [deleted] header, fresh scroll from the top.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.reader.floorIdx != 1 || m.reader.floorTop != 0 {
		t.Fatalf("→ must move to floor 2 from the top: idx=%d top=%d", m.reader.floorIdx, m.reader.floorTop)
	}
	if view = m.View(); !strings.Contains(view, "[deleted] · 2 replies") {
		t.Fatalf("deleted floor header wrong:\n%s", view)
	}

	// Enter → the deleted floor's reply tree; deep reply carries @bob.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.reader.layer != layerReplies {
		t.Fatal("Enter on a floor with replies must open the reply layer")
	}
	view = m.View()
	for _, want := range []string{"replies to [deleted]", "reply under the deleted floor", "@dave frank", "deep under a deleted parent"} {
		if !strings.Contains(view, want) {
			t.Errorf("reply layer missing %q:\n%s", want, view)
		}
	}

	// Esc → floor layer (same floor), Esc → body, Esc → list.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.reader.layer != layerFloors || m.reader.floorIdx != 1 {
		t.Fatalf("Esc must return to floor 2: layer=%v idx=%d", m.reader.layer, m.reader.floorIdx)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.reader != nil || m.focus != regionList {
		t.Fatal("the third Esc must close the reader onto the list")
	}

	// ← from floor 2 back to floor 1, → to floor 3: replyless floors
	// refuse Enter with a hint instead of entering an empty layer.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // body → floors (fresh at floor 1)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.reader.floorIdx != 2 {
		t.Fatalf("setup: want floor 3, idx=%d", m.reader.floorIdx)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.reader.layer != layerFloors {
		t.Fatal("replyless floor must not enter the reply layer")
	}
	if !strings.Contains(m.status, "no replies") {
		t.Fatalf("status should explain the refused Enter: %q", m.status)
	}
	// Clamped at the edges: further ← at floor 1 / → at the last floor.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.reader.floorIdx != 0 {
		t.Fatalf("floor cursor must clamp at the first floor: %d", m.reader.floorIdx)
	}
}

// Comment HTML flows through the M1 pipeline inside the reader:
// paragraphs, quote bars, inline link numbers with the appendix at the
// layer's end, image degradation — same rules as article bodies.
func TestCommentHTMLThroughPipeline(t *testing.T) {
	base, _ := hnDetailServer(t, hnDetailJSON())
	m := openHNDetail(t, nil, base)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // floor 1 (alice)

	view := m.View()
	for _, want := range []string{"first para", "│ quoted words", "a link [1]", "[图片:pic]", "tail para"} {
		if !strings.Contains(view, want) {
			t.Errorf("floor body missing %q:\n%s", want, view)
		}
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	if view = m.View(); !strings.Contains(view, "[1] https://example.com/x") {
		t.Fatalf("the floor's link appendix must sit at its end:\n%s", view)
	}

	// The reply layer renders its rows through the same pipeline; the
	// compressed second level rides the quote bar.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyHome})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // replies of floor 1
	view = m.View()
	if !strings.Contains(view, "bob") || !strings.Contains(view, "direct reply from bob") {
		t.Fatalf("reply layer missing bob's row:\n%s", view)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN}) // jump to carol's row
	view = m.View()
	for _, want := range []string{"@bob carol", "│ deep reply from carol"} {
		if !strings.Contains(view, want) {
			t.Errorf("deep reply must render @parent + quote bar (%q):\n%s", want, view)
		}
	}
}

// First open, nothing cached: the body renders from the list snapshot
// immediately, the footer says the comments are loading, and the fetch
// landing replaces it with the floor count.
func TestFirstOpenShowsLoadingThenLands(t *testing.T) {
	base, _ := hnDetailServer(t, hnDetailJSON())
	m, cmd := openHNDetailCmd(t, nil, base)
	if m.reader == nil || !m.reader.loading {
		t.Fatal("setup: first open must start in the loading state")
	}
	if !strings.Contains(m.View(), "loading comments") {
		t.Fatalf("body footer should show the loading state:\n%s", m.View())
	}
	m = pump(t, m, cmd)
	if m.reader.loading || m.reader.loadErr != nil || len(m.reader.floors) != 3 {
		t.Fatalf("fetch landed: loading=%v err=%v floors=%d", m.reader.loading, m.reader.loadErr, len(m.reader.floors))
	}
	if !strings.Contains(m.View(), "3 floors") {
		t.Fatalf("footer should count the landed floors:\n%s", m.View())
	}
}

// Re-open is cache-first: the cached tree renders before any network,
// then the background refresh silently swaps in the fresh tree.
func TestReopenIsCacheFirstThenSilentRefresh(t *testing.T) {
	fewerFloors := []core.Comment{
		{ID: "10", Author: "alice", Text: "<p>cached floor</p>"},
		{ID: "13", Author: "erin", Text: "<p>cached floor two</p>"},
	}
	base, hits := hnDetailServer(t, hnDetailJSON())
	m, cmd := openHNDetailCmd(t, fewerFloors, base)

	// Only the cache leg has run: 2 cached floors, no server hit yet.
	msg := cmd()
	res, next := m.Update(msg)
	m = res.(Model)
	if !strings.Contains(m.View(), "2 floors") || *hits != 0 {
		t.Fatalf("cache leg must render before any network: hits=%d\n%s", *hits, m.View())
	}

	// The refresh leg lands silently: 3 floors, no status noise.
	m = pump(t, m, next)
	if len(m.reader.floors) != 3 || *hits != 1 {
		t.Fatalf("background refresh must swap in the fresh tree: hits=%d floors=%d", *hits, len(m.reader.floors))
	}
	if m.status != "" {
		t.Fatalf("background refresh must be silent on success: %q", m.status)
	}
	if !strings.Contains(m.View(), "3 floors") {
		t.Fatalf("footer should recount after the refresh:\n%s", m.View())
	}
}

// Offline, a previously opened story still reads through all three
// layers; the failed background refresh stays silent because the cache
// is on screen.
func TestOfflineReopenReadsAllThreeLayers(t *testing.T) {
	// A dead local listener: the refresh dials out and fails fast.
	url, _ := deadFeedURL(t)
	m := openHNDetail(t, cachedTree(), url)

	if !m.reader.loaded || len(m.reader.floors) != 3 {
		t.Fatalf("cache must render offline: loaded=%v floors=%d", m.reader.loaded, len(m.reader.floors))
	}
	if m.status != "" {
		t.Fatalf("failed refresh over a live cache must stay silent: %q", m.status)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.View(), "alice · 2 replies") {
		t.Fatalf("floor layer must render offline:\n%s", m.View())
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.View(), "direct reply from bob") {
		t.Fatalf("reply layer must render offline:\n%s", m.View())
	}
}

// An opened story with genuinely no comments is not a first open: the
// cache renders "no comments" and the (failing) refresh stays silent.
func TestEmptyDiscussionIsNotAFirstOpen(t *testing.T) {
	url, _ := deadFeedURL(t)
	m := openHNDetail(t, []core.Comment{}, url)

	if !m.reader.loaded || len(m.reader.floors) != 0 {
		t.Fatalf("empty opened tree must be loaded-and-empty: loaded=%v floors=%d",
			m.reader.loaded, len(m.reader.floors))
	}
	if !strings.Contains(m.View(), "no comments") {
		t.Fatalf("footer should state the discussion is empty:\n%s", m.View())
	}
	if m.status != "" {
		t.Fatalf("a dead network over an opened story must stay silent: %q", m.status)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.reader.layer != layerFloors || !strings.Contains(m.View(), "no comments") {
		t.Fatalf("the floor layer should carry the same empty state:\n%s", m.View())
	}
}

// Ctrl+O / Ctrl+Y target the layer's own object: the article URL on
// the body (a text post falls back to its HN page), the floor's
// comment page on the floor layer, the topmost visible reply's page on
// the reply layer.
func TestCtrlOAndCtrlYPerLayer(t *testing.T) {
	t.Cleanup(func() {
		openBrowser = browserOpenDefault
		copyClipboard = oscCopyDefault
	})
	base, _ := hnDetailServer(t, hnDetailJSON())

	// Text post: no item URL → the body layer targets the story page.
	m := openHNDetail(t, nil, base)
	m.reader.item.Item.URL = ""
	var opened, copied string
	openBrowser = func(u string) error { opened = u; return nil }
	copyClipboard = func(u string) error { copied = u; return nil }

	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	if opened != "https://news.ycombinator.com/item?id=1001" {
		t.Fatalf("text post body Ctrl+O = %q, want the story page", opened)
	}

	// Floor layer: the floor's comment page.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // floor 1 = alice = comment 10
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlY})
	if copied != "https://news.ycombinator.com/item?id=10" {
		t.Fatalf("floor Ctrl+Y = %q, want comment 10's page", copied)
	}

	// Reply layer: topmost visible reply — bob (11) at the top, then
	// Ctrl+N down to carol (111). A short terminal so the reply doc
	// outscrolls one screen (scroll keys clamp on a fitting doc).
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = size(m, 100, 10)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	if opened != "https://news.ycombinator.com/item?id=11" {
		t.Fatalf("reply Ctrl+O at top = %q, want comment 11's page", opened)
	}
	// Ctrl+N walks reply by reply: first onto bob's row, then carol's.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	if opened != "https://news.ycombinator.com/item?id=111" {
		t.Fatalf("reply Ctrl+O after Ctrl+N = %q, want comment 111's page", opened)
	}
}

// Ctrl+N in the reader is in-memory paging only: the floor layer hops
// a decade of floors, the reply layer to the next reply's first line.
func TestCtrlNPagesInMemory(t *testing.T) {
	many := make([]core.Comment, 0, 15)
	for i := range 12 {
		many = append(many, core.Comment{ID: fmt.Sprintf("f%d", i), Author: "u", Text: "<p>floor</p>"})
	}
	url, _ := deadFeedURL(t)
	m := openHNDetail(t, many, url)

	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN})
	if m.reader.floorIdx != 10 {
		t.Fatalf("Ctrl+N must skip %d floors: idx=%d", floorSkip, m.reader.floorIdx)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN})
	if m.reader.floorIdx != 11 {
		t.Fatalf("Ctrl+N must clamp at the last floor: idx=%d", m.reader.floorIdx)
	}
}

// F1 lists the new binding, and the status hint follows the layer.
func TestHelpAndHintsCoverTheLayers(t *testing.T) {
	base, _ := hnDetailServer(t, hnDetailJSON())
	m := openHNDetail(t, nil, base)

	if hint := m.View(); !strings.Contains(hint, "Ctrl+R read") {
		t.Fatalf("body layer should carry the reading hint:\n%s", hint)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.View(), "←→ floors") {
		t.Fatalf("floor layer hint missing:\n%s", m.View())
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.View(), "Ctrl+N next reply") {
		t.Fatalf("reply layer hint missing:\n%s", m.View())
	}

	m = updateKey(m, tea.KeyMsg{Type: tea.KeyF1})
	view := m.View()
	if !strings.Contains(view, "Ctrl+N") || !strings.Contains(view, "Load more") {
		t.Fatalf("F1 must document Ctrl+N:\n%s", view)
	}
}

// The reader over an rss row (no detail layer) never grows a comment
// footer and Enter stays the old no-op — the regression guard for the
// extension being optional.
func TestRSSReaderStaysSingleLayer(t *testing.T) {
	m := rssModel(t, "Example", 1)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // sidebar → list
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // open the reader
	if m.reader == nil || m.reader.hasDetail {
		t.Fatal("rss reader must open without a detail layer")
	}
	if strings.Contains(m.View(), "floors") || strings.Contains(m.View(), "loading comments") {
		t.Fatalf("rss reader must not grow comment chrome:\n%s", m.View())
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.reader.layer != layerBody {
		t.Fatal("Enter in the rss reader must stay a no-op")
	}
}

// store.ItemRow sanity for the seed helper: the reader's star/read
// machinery keeps working on hn rows opened through the layers.
func TestReadAndStarFromCommentLayers(t *testing.T) {
	base, _ := hnDetailServer(t, hnDetailJSON())
	m := openHNDetail(t, nil, base)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // floors
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // replies

	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if !m.reader.item.Read {
		t.Fatal("Ctrl+R from the reply layer must mark the story read")
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlF})
	if !m.reader.item.Starred {
		t.Fatal("Ctrl+F from the reply layer must star the story")
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if n := strings.Count(m.View(), "★"); n != 1 {
		t.Fatalf("star from deep layers must land on the list row: ★×%d", n)
	}
}

// A deep reply whose immediate parent is deleted renders the
// placeholder in the @parent prefix (the pure side pins
// parentAuthor=""; this pins what the reader does with it).
func TestReplyPrefixUsesDeletedPlaceholder(t *testing.T) {
	m := seededModel(t, nil, nil)
	doc := m.replyDoc(replyRow{
		cm:           core.Comment{Author: "zoe", Text: "<p>body</p>"},
		depth:        2,
		parentAuthor: "",
	})
	header := doc.Blocks[0].(render.Para)
	if got := header.Spans[0].Text; got != "@[deleted] " {
		t.Fatalf("@parent prefix = %q, want the deleted placeholder", got)
	}
}

// First open with nothing cached and the fetch failing (dead network):
// the footer states the failure, the status bar carries the error, and
// the body layer stays readable — the loud half of the silence rule.
func TestFirstOpenFetchFailureKeepsBodyReadable(t *testing.T) {
	url, _ := deadFeedURL(t)
	m, cmd := openHNDetailCmd(t, nil, url)
	m = pump(t, m, cmd)

	d := m.reader
	if d.loading || d.loaded || d.loadErr == nil {
		t.Fatalf("failed first open: loading=%v loaded=%v err=%v", d.loading, d.loaded, d.loadErr)
	}
	if !strings.Contains(m.View(), "comment fetch failed") {
		t.Fatalf("footer should state the failed fetch:\n%s", m.View())
	}
	if !strings.Contains(m.status, "comment fetch failed") {
		t.Fatalf("status bar should carry the error: %q", m.status)
	}
	if !strings.Contains(m.View(), "Hot story") || !strings.Contains(m.View(), "https://example.com/article") {
		t.Fatalf("the body must stay readable after the failure:\n%s", m.View())
	}
	// The floor layer carries the same state instead of a blank screen.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.reader.layer != layerFloors || !strings.Contains(m.View(), "comment fetch failed") {
		t.Fatalf("floor layer should show the failure state:\n%s", m.View())
	}
}
