package ui

// Ticket 08: the recommend feed in the sidebar — the label resolved
// through i18n (like every feed label), the row order 热榜→推荐→关注→
// 日报 pinned by position in the rendered view, and the meta column
// carrying the card's author (the answerer on answer cards) through
// the same follow-feed routing.

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/smy-101/readscope/internal/core"
)

// recommendModel boots the app against a store seeded with recommend
// rows and lands the sidebar on the recommend feed (one KeyDown from
// the hot board).
func recommendModel(t *testing.T, items []core.Item) Model {
	t.Helper()
	m := seededModel(t, nil, nil)
	if _, _, err := m.st.SaveFeedItems(context.Background(), "zhihu", "recommend", items); err != nil {
		t.Fatalf("seed recommend items: %v", err)
	}
	m = size(m, 100, 30)
	for range 3 { // books → rss → hn → zhihu
		var cmd tea.Cmd
		m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
		m = pump(t, m, cmd)
	}
	// sidebar: hot → recommend (feedsMsg already landed via the pump)
	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyDown})
	m = pump(t, m, cmd)
	if m.selectedFeedKey() != "recommend" {
		t.Fatalf("selected feed = %q, want recommend", m.selectedFeedKey())
	}
	return m
}

func TestRecommendFeedSidebarLabelOrderAndAuthorMeta(t *testing.T) {
	m := recommendModel(t, []core.Item{
		{ID: "zh:question/2001", Source: "zhihu", Feed: "recommend",
			Title: "推荐的问题", URL: "https://www.zhihu.com/question/2001",
			Author: "赵六", FetchedAt: time.Now().UTC()},
	})
	view := m.View()
	if !strings.Contains(view, "Recommend") {
		t.Errorf("sidebar missing the recommend feed label:\n%s", view)
	}
	if strings.Contains(view, "zhihu.recommend") {
		t.Errorf("label keys must resolve, raw key leaked:\n%s", view)
	}
	// The spec's sidebar order: recommend sits between the hot board
	// and the follow feed.
	ih, ir, if_ := strings.Index(view, "Hot List"), strings.Index(view, "Recommend"), strings.Index(view, "Following")
	if ih < 0 || ir < 0 || if_ < 0 || !(ih < ir && ir < if_) {
		t.Errorf("sidebar order broken: hot@%d recommend@%d follow@%d\n%s", ih, ir, if_, view)
	}
	if !strings.Contains(view, "推荐的问题") {
		t.Errorf("list missing the recommend row:\n%s", view)
	}
	if !strings.Contains(view, "赵六") {
		t.Errorf("recommend meta column should carry the author:\n%s", view)
	}
}
