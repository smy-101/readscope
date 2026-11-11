package ui

// Ticket 01 UI tests: Tab onto the Hacker News source — the sidebar
// fills with the four fixed feeds (labels resolved through i18n),
// cached items render with the same unread mechanics as RSS, and Tab
// onto a source with no registered package (books) says so in the
// status bar instead of showing a mystery blank.
//
// Ticket 02 UI tests: the heat radar meta column ("N pts · N comments ·
// author" per hn row, relative time for everyone else), its graceful
// degradation, points-ranked front lists vs chronological new, and the
// narrow-terminal truncation rules.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/smy-101/readscope/internal/core"

	_ "github.com/smy-101/readscope/internal/source/hn" // register "hn", as main does
)

// hnModel seeds n cached HN stories in the front feed and lands the
// model on the Hacker News list (books → rss → hn, focus on the list).
func hnModel(t *testing.T, n int) Model {
	t.Helper()
	seed := make([]core.Item, 0, n)
	for i := 0; i < n; i++ {
		seed = append(seed, core.Item{
			ID:     fmt.Sprintf("hn:algolia:%d", 1000+i),
			Source: "hn", Feed: "front",
			Title:     fmt.Sprintf("HN story %d", i),
			URL:       "https://example.com/s",
			FetchedAt: time.Now().UTC().Add(-time.Duration(i) * time.Hour),
			Body:      "https://example.com/s",
		})
	}
	m := seededModel(t, nil, nil)
	if _, _, err := m.st.SaveFeedItems(context.Background(), "hn", "front", seed); err != nil {
		t.Fatalf("seed hn items: %v", err)
	}
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // books → rss
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // rss → hn
	m = pump(t, m, cmd)
	return m
}

func TestHNTabShowsFeedsAndCachedItems(t *testing.T) {
	m := hnModel(t, 2)
	view := m.View()
	// Sidebar: the four fixed feeds, labels resolved through i18n
	// (LabelKey → catalog), not raw keys.
	for _, want := range []string{"Front", "New", "Ask HN", "Show HN"} {
		if !strings.Contains(view, want) {
			t.Errorf("sidebar missing feed %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "hn.front") {
		t.Errorf("label keys must resolve, raw key leaked:\n%s", view)
	}
	// List: cached items render with the shared unread mechanics.
	if !strings.Contains(view, "HN story 0") {
		t.Fatalf("list missing cached item:\n%s", view)
	}
	if n := strings.Count(view, "●"); n != 2 {
		t.Fatalf("unread dots = %d, want 2:\n%s", n, view)
	}
	if m.selectedFeedKey() != "front" {
		t.Fatalf("selected feed = %q, want front", m.selectedFeedKey())
	}
}

func TestFeedLabelResolvesLabelKeyOverRawLabel(t *testing.T) {
	m := newTestModel(t)
	keyed := core.Feed{Key: "front", LabelKey: "hn.front", Label: "stale"}
	if got := m.feedLabel(&keyed); got != "Front" {
		t.Fatalf("feedLabel(keyed) = %q, want catalog label %q", got, "Front")
	}
	plain := core.Feed{Key: "ruanyq", Label: "阮一峰周刊"}
	if got := m.feedLabel(&plain); got != "阮一峰周刊" {
		t.Fatalf("feedLabel(plain) = %q, want the label as-is", got)
	}
	if got := m.feedLabel(nil); got != "" {
		t.Fatalf("feedLabel(nil) = %q, want empty", got)
	}
}

func TestTabToUnimplementedSourceHintsLater(t *testing.T) {
	m := size(newTestModel(t), 100, 30)
	// books → rss → hn → books: landing back on the unimplemented
	// source, the status bar explains itself.
	for i := 0; i < 3; i++ {
		var cmd tea.Cmd
		m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
		m = pump(t, m, cmd)
	}
	if m.active != 0 {
		t.Fatalf("active = %d, want books", m.active)
	}
	if !strings.Contains(m.status, "later milestone") {
		t.Fatalf("status should hint at a later milestone, got %q", m.status)
	}
	if !strings.Contains(m.View(), "later milestone") {
		t.Fatal("the hint should be visible in the status bar")
	}
}

// hnHeatItem builds one cached hn story with the full heat snapshot.
func hnHeatItem(feed, id, title, author string, age time.Duration, points, comments *int) core.Item {
	published := time.Now().UTC().Add(-age)
	return core.Item{
		ID: "hn:algolia:" + id, Source: "hn", Feed: feed,
		Title: title, URL: "https://example.com/s", Author: author,
		PublishedAt: &published, FetchedAt: time.Now().UTC(),
		Body: "https://example.com/s", Points: points, NumComments: comments,
	}
}

func ip(n int) *int { return &n }

// hnSeeded seeds hn items under feedKey and lands the model on that
// feed's list (sidebar order: front, new, ask, show).
func hnSeeded(t *testing.T, feedKey string, items []core.Item) Model {
	t.Helper()
	m := seededModel(t, nil, nil)
	if _, _, err := m.st.SaveFeedItems(context.Background(), "hn", feedKey, items); err != nil {
		t.Fatalf("seed hn items: %v", err)
	}
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // books → rss
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // rss → hn
	m = pump(t, m, cmd)
	for m.selectedFeedKey() != feedKey {
		m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyDown})
		m = pump(t, m, cmd)
	}
	return m
}

func TestHNListShowsHeatMeta(t *testing.T) {
	m := hnSeeded(t, "front", []core.Item{
		hnHeatItem("front", "1001", "Heat story", "alice", time.Hour, ip(120), ip(45)),
	})
	view := m.View()
	for _, want := range []string{"120 pts", "45 comments", "alice"} {
		if !strings.Contains(view, want) {
			t.Errorf("heat meta missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "120 pts · 45 comments · alice") {
		t.Fatalf("heat radar should render as one segment run:\n%s", view)
	}
}

func TestHNHeatMetaDegradesWithoutEmptySegments(t *testing.T) {
	m := hnSeeded(t, "front", []core.Item{
		hnHeatItem("front", "1001", "Full snapshot", "alice", time.Hour, ip(120), ip(45)),
		hnHeatItem("front", "1002", "No author", "", time.Hour, ip(30), ip(2)),
		hnHeatItem("front", "1003", "Bare record", "", time.Hour, nil, nil),
	})
	view := m.View()
	// Present fields render; absent ones drop, never an empty "· ·" run.
	if !strings.Contains(view, "30 pts · 2 comments") {
		t.Errorf("no-author row should end at its last real segment:\n%s", view)
	}
	if strings.Contains(view, "· ·") || strings.Contains(view, "·  ·") {
		t.Fatalf("empty segment leaked into the meta column:\n%s", view)
	}
	// "30 pts" must not trip the zero check: a nil snapshot renders
	// nothing, a real zero would render " 0 pts" (space-padded by the
	// right-aligned column).
	if strings.Contains(view, " 0 pts") || strings.Contains(view, " 0 comments") {
		t.Fatalf("nil heat must not render as zero:\n%s", view)
	}
	// The bare record renders like the date-less rows always have: a
	// blank meta, not a crash or a placeholder.
	if !strings.Contains(view, "Bare record") {
		t.Fatalf("bare record should still list:\n%s", view)
	}
}

func TestFrontListRanksByPointsSnapshot(t *testing.T) {
	// Age order: Bravo (1h), Charlie (3h), Alpha (5h). Heat order:
	// Alpha 500, Charlie 300, Bravo 100 — the front feed ranks by the
	// points snapshot, so Alpha leads despite being the oldest.
	m := hnSeeded(t, "front", []core.Item{
		hnHeatItem("front", "1001", "Alpha tale", "a", 5*time.Hour, ip(500), ip(10)),
		hnHeatItem("front", "1002", "Bravo tale", "b", time.Hour, ip(100), ip(2)),
		hnHeatItem("front", "1003", "Charlie tale", "c", 3*time.Hour, ip(300), ip(5)),
	})
	view := m.View()
	if !(strings.Index(view, "Alpha tale") < strings.Index(view, "Charlie tale") &&
		strings.Index(view, "Charlie tale") < strings.Index(view, "Bravo tale")) {
		t.Fatalf("front should rank by points snapshot (Alpha, Charlie, Bravo):\n%s", view)
	}
}

func TestNewFeedKeepsTimeOrder(t *testing.T) {
	// Same seeds as the ranking test: on New the newest story (Bravo,
	// 1h) leads regardless of Alpha's 500 points.
	m := hnSeeded(t, "new", []core.Item{
		hnHeatItem("new", "1001", "Alpha tale", "a", 5*time.Hour, ip(500), ip(10)),
		hnHeatItem("new", "1002", "Bravo tale", "b", time.Hour, ip(100), ip(2)),
	})
	view := m.View()
	if !(strings.Index(view, "Bravo tale") < strings.Index(view, "Alpha tale")) {
		t.Fatalf("new must stay chronological (Bravo first):\n%s", view)
	}
}

// A long heat string on a narrow terminal degrades by the standing
// list rules: never wrap, never overflow — the column truncates with
// "…" once it hits the cap, and drops entirely before squeezing the
// title. (The spec's "作者可缩略": author tails abbreviate.)
func TestNarrowHNTerminalStaysIntact(t *testing.T) {
	longAuthor := "incrediblylongusername"
	items := []core.Item{
		hnHeatItem("front", "1001", "Narrow heat story", longAuthor, time.Hour, ip(9999), ip(888)),
	}
	for _, tc := range []struct {
		width    int
		title    string // what must survive of the title
		heat     string // what must survive of the heat radar
		fullMeta bool   // the whole radar must render
	}{
		// 40 cols: usable 13 — the column drops, the title keeps 11 cells.
		{40, "Narrow hea", "", false},
		// 70 cols: usable 43 — the radar truncates at the cap, the
		// title squeezes to a stub.
		{70, "Narrow", "9999 pts · 888 comments ·", false},
		// 120 cols: comfortable — same cap, so the author still
		// abbreviates; the numbers always render whole.
		{120, "Narrow heat story", "9999 pts · 888 comments · incre…", false},
	} {
		m := hnSeeded(t, "front", items)
		m = size(m, tc.width, 24)
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) != 24 {
			t.Fatalf("width %d: height = %d lines, want 24", tc.width, len(lines))
		}
		for i, ln := range lines {
			if lw := lipgloss.Width(ln); lw > tc.width {
				t.Fatalf("width %d: line %d overflows (%d cols): %q", tc.width, i, lw, ln)
			}
		}
		if !strings.Contains(view, tc.title) {
			t.Errorf("width %d: title %q missing:\n%s", tc.width, tc.title, view)
		}
		if tc.heat != "" && !strings.Contains(view, tc.heat) {
			t.Errorf("width %d: heat %q missing:\n%s", tc.width, tc.heat, view)
		}
		if strings.Contains(view, longAuthor) {
			t.Errorf("width %d: the long author must truncate or drop, not render in full", tc.width)
		}
	}
	// 40 cols dropped the column entirely: no heat anywhere.
	m := hnSeeded(t, "front", items)
	m = size(m, 40, 24)
	if strings.Contains(m.View(), "9999 pts") {
		t.Fatal("at 40 cols the heat column should drop, not squeeze the title")
	}
}

// The favorites list mixes sources row by row; each row keeps its own
// source's meta — heat for the hn row, relative age for the rss row.
func TestFavoritesRenderMetaPerSource(t *testing.T) {
	published := time.Now().UTC().Add(-time.Hour)
	rssSeed := core.Item{
		ID: "rss:ex:1", Source: "rss", Feed: "ex", Title: "RSS story",
		URL: "https://example.com/a", PublishedAt: &published,
		FetchedAt: time.Now().UTC(), Body: "<p>body</p>",
	}
	m := seededModel(t,
		map[string]core.RSSFeed{"ex": {Label: "Example", URL: "http://127.0.0.1:1/x"}},
		map[string][]core.Item{"ex": {rssSeed}},
	)
	hnSeed := hnHeatItem("front", "1001", "HN story", "alice", time.Hour, ip(120), ip(45))
	if _, _, err := m.st.SaveFeedItems(context.Background(), "hn", "front", []core.Item{hnSeed}); err != nil {
		t.Fatalf("seed hn: %v", err)
	}
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // books → rss
	m = pump(t, m, cmd)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlF})    // star the rss row
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // rss → hn
	m = pump(t, m, cmd)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlF}) // star the hn row
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF3})
	view := m.View()
	if !strings.Contains(view, "120 pts · 45 comments · alice") {
		t.Errorf("hn favorite should keep its heat meta:\n%s", view)
	}
	if !strings.Contains(view, "1h") {
		t.Errorf("rss favorite should keep its relative time:\n%s", view)
	}
}
