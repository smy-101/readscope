package ui

// Ticket 01 UI tests: Tab onto the Hacker News source — the sidebar
// fills with the four fixed feeds (labels resolved through i18n),
// cached items render with the same unread mechanics as RSS, and Tab
// onto a source with no registered package (books) says so in the
// status bar instead of showing a mystery blank.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
