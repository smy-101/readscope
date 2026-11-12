package ui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/store"
	"github.com/smy-101/readscope/internal/theme"

	_ "github.com/smy-101/readscope/internal/source/rss" // registry: rss
)

// seededModel builds a model over a real migrated store holding seed
// items, with the given feeds wired into sources (URLs deliberately
// never dialed unless a test refreshes). ReadIDs mark items read via
// raw SQL — ticket 04 adds the DAO.
func seededModel(t *testing.T, feeds map[string]core.RSSFeed, seed map[string][]core.Item, readIDs ...string) Model {
	t.Helper()
	t.Setenv("READSCOPE_CONFIG", t.TempDir())
	t.Setenv("LC_ALL", "en_US.UTF-8")
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	for feed, items := range seed {
		if _, _, err := st.SaveFeedItems(ctx, "rss", feed, items); err != nil {
			t.Fatalf("seed %s: %v", feed, err)
		}
	}
	for _, id := range readIDs {
		if _, err := st.Exec(ctx, `INSERT INTO read_state (item_id, read_at) VALUES (?, ?)`,
			id, time.Now().UTC()); err != nil {
			t.Fatalf("mark %s read: %v", id, err)
		}
	}
	cfg := core.Defaults()
	cat, err := i18n.Load("en")
	if err != nil {
		t.Fatalf("i18n: %v", err)
	}
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatalf("theme: %v", err)
	}
	srcs := core.DefaultSources()
	srcs.RSS.Feeds = feeds
	return New(cfg, cat, th, st, srcs)
}

// pump runs a command chain to quiescence: each cmd's message goes back
// through Update, following the cmds Update returns. This is the Elm
// loop executed synchronously — the whole async flow, deterministically.
func pump(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		res, next := m.Update(msg)
		var ok bool
		if m, ok = res.(Model); !ok {
			t.Fatalf("Update returned %T, want Model", res)
		}
		cmd = next
	}
	return m
}

// deadFeedURL serves a listener that records any dial; a test fails if
// readscope touches the network when it shouldn't.
func deadFeedURL(t *testing.T) (url string, dialed *bool) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	hit := false
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			hit = true
			c.Close()
		}
	}()
	return "http://" + ln.Addr().String() + "/feed", &hit
}

func TestStartupRendersFromCacheOffline(t *testing.T) {
	mkItem := func(id, title string, age time.Duration) core.Item {
		published := time.Now().UTC().Add(-age)
		return core.Item{
			ID: id, Source: "rss", Feed: "ruanyq",
			Title: title, URL: "https://example.com/" + id,
			PublishedAt: &published, FetchedAt: time.Now().UTC(), Body: "<p>snapshot</p>",
		}
	}
	url, dialed := deadFeedURL(t)
	m := seededModel(t,
		map[string]core.RSSFeed{"ruanyq": {Label: "Ruanyifeng Weekly", URL: url}},
		map[string][]core.Item{"ruanyq": {
			mkItem("rss:ruanyq:1", "Fresh take", 90*time.Minute),
			mkItem("rss:ruanyq:2", "Already read", 26*time.Hour),
		}},
		"rss:ruanyq:2", // read
	)
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // books → rss, chain loads feeds+items
	m = pump(t, m, cmd)

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 30 {
		t.Fatalf("view height = %d lines, want 30", len(lines))
	}
	// Sidebar row (body region, not the status bar): label + unread badge.
	badge := false
	for _, ln := range lines[1 : len(lines)-1] {
		if strings.Contains(ln, "Ruanyifeng Weekly") && strings.Contains(ln, "1") {
			badge = true
		}
	}
	if !badge {
		t.Fatalf("sidebar row missing label or unread badge:\n%s", view)
	}
	// List: both titles render, with relative time; exactly one unread
	// dot (the read item loses its dot).
	for _, want := range []string{"Fresh take", "Already read", "1h", "1d"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if n := strings.Count(view, "●"); n != 1 {
		t.Fatalf("unread dots = %d, want 1 (read item must lose its dot):\n%s", n, view)
	}
	// Status bar: current feed + item/new counts.
	status := lines[len(lines)-1]
	for _, want := range []string{"Ruanyifeng Weekly", "2 items", "1 new"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status bar missing %q: %q", want, status)
		}
	}
	// Cache first: startup must not have dialed the feed URL.
	if *dialed {
		t.Fatal("startup dialed the network — the list must render from the SQLite cache")
	}
}

func TestSidebarFeedSwitchLoadsItems(t *testing.T) {
	mkItem := func(feed, id, title string) core.Item {
		return core.Item{ID: id, Source: "rss", Feed: feed, Title: title, FetchedAt: time.Now().UTC()}
	}
	m := seededModel(t,
		map[string]core.RSSFeed{
			"alpha": {Label: "Alpha", URL: "http://127.0.0.1:1/a"},
			"beta":  {Label: "Beta", URL: "http://127.0.0.1:1/b"},
		},
		map[string][]core.Item{
			"alpha": {mkItem("alpha", "rss:alpha:1", "Alpha only story")},
			"beta":  {mkItem("beta", "rss:beta:1", "Beta only story")},
		},
	)
	m = size(m, 100, 30)
	m = pump(t, m, m.Init())
	// Tab once: books → rss, focus lands on the sidebar.
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
	m = pump(t, m, cmd)
	if !strings.Contains(m.View(), "Alpha only story") {
		t.Fatalf("first feed's items should show after Tab:\n%s", m.View())
	}
	// Down: select Beta; its items load, Alpha's disappear.
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyDown})
	m = pump(t, m, cmd)
	view := m.View()
	if !strings.Contains(view, "Beta only story") {
		t.Fatalf("Beta's items missing after Down:\n%s", view)
	}
	if strings.Contains(view, "Alpha only story") {
		t.Fatalf("Alpha's items still visible after switching feeds:\n%s", view)
	}
	if !strings.Contains(view, "Beta") {
		t.Fatalf("status bar should name the current feed:\n%s", view)
	}
}

func TestListNavigationClampsPagesAndScrolls(t *testing.T) {
	const n = 30
	seed := make([]core.Item, 0, n)
	for i := 0; i < n; i++ {
		seed = append(seed, core.Item{
			ID: fmt.Sprintf("rss:big:%d", i), Source: "rss", Feed: "big",
			Title: fmt.Sprintf("Story number %02d", i), FetchedAt: time.Now().UTC(),
		})
	}
	m := seededModel(t,
		map[string]core.RSSFeed{"big": {Label: "Big", URL: "http://127.0.0.1:1/x"}},
		map[string][]core.Item{"big": seed},
	)
	m = size(m, 100, 30) // body height 28: 28 rows visible
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
	m = pump(t, m, cmd)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyRight}) // sidebar → list focus
	if m.listSel != 0 || m.listTop != 0 {
		t.Fatalf("cursor should start at top: sel=%d top=%d", m.listSel, m.listTop)
	}

	// Up at the top clamps.
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.listSel != 0 {
		t.Fatalf("up at top should clamp, sel=%d", m.listSel)
	}

	// Down moves within the window; pgdown jumps a page and drags the
	// window along.
	for range 3 {
		m = updateKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.listSel != 3 || m.listTop != 0 {
		t.Fatalf("after 3 downs: sel=%d top=%d, want 3/0", m.listSel, m.listTop)
	}
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.listSel != 0 {
		t.Fatalf("pgup at top should clamp: sel=%d", m.listSel)
	}
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.listSel != m.bodyHeight() {
		t.Fatalf("pgdown should move a page: sel=%d, want %d", m.listSel, m.bodyHeight())
	}
	if m.listTop != m.listSel-m.bodyHeight()+1 {
		t.Fatalf("window should follow the cursor: top=%d sel=%d", m.listTop, m.listSel)
	}

	// Hammer pgdown past the end: clamp at the last row with the window
	// pinned to the bottom.
	for range 10 {
		m = updateKey(m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if m.listSel != n-1 {
		t.Fatalf("cursor should clamp to last item: sel=%d want %d", m.listSel, n-1)
	}
	if m.listTop != n-m.bodyHeight() {
		t.Fatalf("window should pin to bottom: top=%d want %d", m.listTop, n-m.bodyHeight())
	}
	// The bottom of the list (oldest item, last visible row) is on screen.
	if !strings.Contains(m.View(), "Story number 00") {
		t.Fatalf("last item should be visible after paging to the end:\n%s", m.View())
	}
}

// TestNarrowTerminalStaysIntact renders a long CJK title at width 40:
// no line may exceed the terminal width and the title must truncate
// with an ellipsis instead of wrapping.
func TestNarrowTerminalStaysIntact(t *testing.T) {
	long := strings.Repeat("长标题例句", 12) // 48 display cols
	m := seededModel(t,
		map[string]core.RSSFeed{"cn": {Label: "中文订阅名字很长也会被截断", URL: "http://127.0.0.1:1/x"}},
		map[string][]core.Item{"cn": {{
			ID: "rss:cn:1", Source: "rss", Feed: "cn", Title: long,
			FetchedAt: time.Now().UTC(),
		}}},
	)
	m = size(m, 40, 24)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
	m = pump(t, m, cmd)

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 24 {
		t.Fatalf("height = %d lines, want 24", len(lines))
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > 40 {
			t.Fatalf("line %d overflows: width %d > 40: %q", i, w, ln)
		}
	}
	if strings.Contains(view, long) {
		t.Fatal("long title must be truncated, not rendered in full")
	}
	if !strings.Contains(view, "…") {
		t.Fatalf("truncated title should end with an ellipsis:\n%s", view)
	}
}

// TestSelectedRowHighlightsFullWidth: the selected list row must read
// as one full-width bar. Wrapping an already-styled line in
// ListSelected dies at the first embedded reset, so the highlight
// collapsed onto the marker column and the cursor looked like a lone
// dot. The fix re-renders every segment through ListSelected — assert
// marker, title and meta each open with the selection escape
// (#ffffff on #094771, the dark theme), and that the bar reaches the
// row's right edge (the meta segment closes the line).
func TestSelectedRowHighlightsFullWidth(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := seededModel(t,
		map[string]core.RSSFeed{"f": {Label: "Feed", URL: "http://127.0.0.1:1/x"}},
		map[string][]core.Item{"f": {
			{ID: "rss:f:0", Source: "rss", Feed: "f", Title: "First story here", FetchedAt: time.Now().UTC(), PublishedAt: ptrTime(time.Now().UTC())},
			{ID: "rss:f:1", Source: "rss", Feed: "f", Title: "Second story here", FetchedAt: time.Now().UTC(), PublishedAt: ptrTime(time.Now().UTC())},
		}},
	)
	m = size(m, 100, 24)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
	m = pump(t, m, cmd)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyRight}) // sidebar → list focus
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyDown})  // sel = 1
	if m.listSel != 1 {
		t.Fatalf("setup: sel=%d, want 1", m.listSel)
	}
	// The sorted list decides which title sits on the cursor — read it
	// back instead of assuming an order.
	wantSel, wantOther := m.items[1].Item.Title, m.items[0].Item.Title

	const sel = "\x1b[38;2;255;255;255;48;2;9;71;113m"
	var selLine, otherLine string
	for _, ln := range strings.Split(m.View(), "\n") {
		switch {
		case strings.Contains(ln, wantSel):
			selLine = ln
		case strings.Contains(ln, wantOther):
			otherLine = ln
		}
	}
	if selLine == "" || otherLine == "" {
		t.Fatalf("setup: rows not found in view:\n%s", m.View())
	}
	if !strings.Contains(selLine, sel+"●") {
		t.Errorf("marker column not highlighted:\n%q", selLine)
	}
	if !strings.Contains(selLine, sel+wantSel) {
		t.Errorf("title not highlighted (highlight collapses at the first embedded reset):\n%q", selLine)
	}
	// The meta segment is the row's last content, so the bar reaches
	// the right edge: <sel>·spaces·“now”·reset at end of line.
	metaEnd := regexp.MustCompile(regexp.QuoteMeta(sel) + ` +now` + `\x1b\[0m[ \t]*$`)
	if !metaEnd.MatchString(selLine) {
		t.Errorf("meta column not highlighted — bar stops before the row's right edge:\n%q", selLine)
	}
	if strings.Count(selLine, sel) < 3 {
		t.Errorf("want marker+title+meta segments highlighted, got %d:\n%q", strings.Count(selLine, sel), selLine)
	}
	if strings.Contains(otherLine, "48;2;9;71;113") {
		t.Errorf("unselected row must not carry the selection background:\n%q", otherLine)
	}
}

// ptrTime is a tiny helper for optional timestamps in seed items.
func ptrTime(t time.Time) *time.Time { return &t }

// TestRefreshKeepsCursorPosition: during an F5 fetch the user keeps
// browsing (spec: “期间可继续操作”); when the fetch lands and the same
// feed reloads, the cursor must survive — only a feed switch resets it.
func TestRefreshKeepsCursorPosition(t *testing.T) {
	mkItem := func(feed, id string) core.Item {
		return core.Item{ID: id, Source: "rss", Feed: feed, Title: id, FetchedAt: time.Now().UTC()}
	}
	m := seededModel(t,
		map[string]core.RSSFeed{
			"one": {Label: "One", URL: "http://127.0.0.1:1/x"},
			"two": {Label: "Two", URL: "http://127.0.0.1:1/y"},
		},
		map[string][]core.Item{
			"one": {mkItem("one", "rss:one:1"), mkItem("one", "rss:one:2"), mkItem("one", "rss:one:3")},
			"two": {mkItem("two", "rss:two:1")},
		},
	)
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
	m = pump(t, m, cmd)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyRight}) // list focus
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.listSel != 2 {
		t.Fatalf("setup: sel=%d, want 2", m.listSel)
	}
	// The fetch lands and the reload chain runs (same feed): cursor stays.
	res, cmd2 := m.Update(refreshDoneMsg{source: "rss"})
	m = pump(t, res.(Model), cmd2)
	if m.listSel != 2 || m.listTop != 0 {
		t.Fatalf("same-feed reload must keep the cursor: sel=%d top=%d", m.listSel, m.listTop)
	}
	// Switching feeds still resets the cursor.
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyLeft})   // list → sidebar
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyDown}) // one → two
	m = pump(t, m, cmd)
	if m.listSel != 0 {
		t.Fatalf("feed switch must reset the cursor: sel=%d", m.listSel)
	}
}

// TestRefreshErrorNamesTheRightSource: a source-level refresh failure
// must be reported against the source that was refreshed, not the one
// the user Tabbed to while the fetch ran.
func TestRefreshErrorNamesTheRightSource(t *testing.T) {
	m := seededModel(t, map[string]core.RSSFeed{"dead": {Label: "Dead", URL: "http://127.0.0.1:1/x"}}, nil)
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // rss
	m = pump(t, m, cmd)
	m.refreshing = true                              // as if F5 fired on rss
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // rss → hn mid-fetch
	m = pump(t, m, cmd)
	res, cmd := m.Update(refreshDoneMsg{source: "rss", err: errors.New("boom")})
	m = pump(t, res.(Model), cmd)
	if !strings.Contains(m.status, "rss: error: boom") {
		t.Fatalf("failed refresh should name the rss source: %q", m.status)
	}
	if strings.Contains(m.status, "hn") {
		t.Fatalf("failure mislabeled with the active source: %q", m.status)
	}
	if m.refreshing {
		t.Fatal("refreshing flag should clear when the fetch lands")
	}
}

// TestF5RefreshRunsAgainstLoopback drives the real refresh path: F5
// kicks a background fetch against an httptest server on loopback (the
// only network the sandbox allows), and when it lands the list and
// counts update from the store.
func TestF5RefreshRunsAgainstLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>Serve</title>
		<item><title>Loopback story one</title><guid>l1</guid><description>body one</description></item>
		<item><title>Loopback story two</title><guid>l2</guid><description>body two</description></item>
		</channel></rss>`))
	}))
	t.Cleanup(srv.Close)

	m := seededModel(t,
		map[string]core.RSSFeed{"srv": {Label: "Serve", URL: srv.URL}},
		nil,
	)
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // books → rss, feeds+items load
	m = pump(t, m, cmd)
	if strings.Contains(m.View(), "Loopback story") {
		t.Fatal("nothing fetched yet — the list should be empty")
	}
	// F5 from the list region (default focus after Esc).
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	res, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF5})
	m = res.(Model)
	if !strings.Contains(m.View(), "refreshing") {
		t.Fatalf("status should show refreshing while the fetch runs:\n%s", m.View())
	}
	m = pump(t, m, cmd)
	view := m.View()
	for _, want := range []string{"Loopback story one", "Loopback story two", "2 items", "2 new"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q after refresh:\n%s", want, view)
		}
	}
}
