package ui

// Ticket 02 UI tests: Tab onto the zhihu source — the sidebar fills
// with the hot feed (label resolved through i18n), cached question
// cards render with the heat meta ("N 万热度" restored from the folded
// points snapshot), the list ranks by that snapshot, and the
// source-agnostic mechanics (unread dot, Ctrl+R) hold for zhihu rows
// exactly as for rss/hn.

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/smy-101/readscope/internal/core"

	_ "github.com/smy-101/readscope/internal/source/zhihu" // register "zhihu", as main does (cmd/readscope/zhihu.go)
)

// zhihuHeatItem builds one cached hot-board question card. The points
// value is the folded heat snapshot ("2045 万热度" → 20450000).
func zhihuHeatItem(qid, title string, fetchedAgo time.Duration, points *int) core.Item {
	return core.Item{
		ID: "zh:question/" + qid, Source: "zhihu", Feed: "hot",
		Title: title, URL: "https://www.zhihu.com/question/" + qid,
		FetchedAt: time.Now().UTC().Add(-fetchedAgo), Points: points,
	}
}

// zhihuModel seeds the hot feed and lands the model on the zhihu list
// (books → rss → hn → zhihu, focus on the list).
func zhihuModel(t *testing.T, items []core.Item) Model {
	t.Helper()
	m := seededModel(t, nil, nil)
	if _, _, err := m.st.SaveFeedItems(context.Background(), "zhihu", "hot", items); err != nil {
		t.Fatalf("seed zhihu items: %v", err)
	}
	m = size(m, 100, 30)
	for range 3 { // books → rss → hn → zhihu
		var cmd tea.Cmd
		m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
		m = pump(t, m, cmd)
	}
	return m
}

func TestZhihuTabShowsHotFeedAndCachedItems(t *testing.T) {
	m := zhihuModel(t, []core.Item{
		zhihuHeatItem("18984420158198241699", "热榜问题一", 0, ip(20450000)),
		zhihuHeatItem("100001", "热榜问题二", 0, nil),
	})
	if m.activeSourceID() != "zhihu" {
		t.Fatalf("active source = %q, want zhihu", m.activeSourceID())
	}
	view := m.View()
	// Sidebar: the hot feed, its label resolved through i18n
	// (LabelKey → catalog), never the raw key.
	if !strings.Contains(view, "Hot List") {
		t.Errorf("sidebar missing the hot feed label:\n%s", view)
	}
	if strings.Contains(view, "zhihu.hot") {
		t.Errorf("label keys must resolve, raw key leaked:\n%s", view)
	}
	// List: cached question cards render with the shared unread
	// mechanics; the heat meta restores the 万 reading.
	if !strings.Contains(view, "热榜问题一") {
		t.Errorf("list missing cached item:\n%s", view)
	}
	if !strings.Contains(view, "2045万 heat") {
		t.Errorf("heat meta should restore the folded snapshot:\n%s", view)
	}
	if n := strings.Count(view, "●"); n != 2 {
		t.Errorf("unread dots = %d, want 2:\n%s", n, view)
	}
}

func TestZhihuHeatMetaDegradesWithoutHeat(t *testing.T) {
	m := zhihuModel(t, []core.Item{
		zhihuHeatItem("100001", "置顶卡", 0, nil),
	})
	view := m.View()
	if !strings.Contains(view, "置顶卡") {
		t.Fatalf("pinned card should still list:\n%s", view)
	}
	if strings.Contains(view, "0万") {
		t.Fatalf("nil heat must not render as zero:\n%s", view)
	}
}

func TestHotListRanksByHeatSnapshot(t *testing.T) {
	// Fetch order (newest first): quiet card, then hot card. Heat
	// order: 热榜问题 2045万 > 小众问题 12万 — the board ranks by the
	// snapshot, so the hotter row leads despite being fetched later.
	m := zhihuModel(t, []core.Item{
		zhihuHeatItem("100002", "小众问题", time.Hour, ip(120000)),
		zhihuHeatItem("100001", "热榜问题", 2*time.Hour, ip(20450000)),
	})
	view := m.View()
	if !(strings.Index(view, "热榜问题") < strings.Index(view, "小众问题")) {
		t.Fatalf("hot board should rank by heat snapshot:\n%s", view)
	}
}

func TestCtrlRWorksOnZhihuRows(t *testing.T) {
	m := zhihuModel(t, []core.Item{
		zhihuHeatItem("18984420158198241699", "热榜问题一", 0, ip(20450000)),
		zhihuHeatItem("100001", "热榜问题二", 0, nil),
	})
	if n := strings.Count(m.View(), "●"); n != 2 {
		t.Fatalf("setup: unread dots = %d, want 2", n)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if n := strings.Count(m.View(), "●"); n != 1 {
		t.Fatalf("after Ctrl+R: unread dots = %d, want 1 (zhihu rows ride the same read state)", n)
	}
}

// F4 (unread-only) and Ctrl+A (mark all read) are the same generic
// machinery as rss/hn — pinned once on zhihu rows so the acceptance
// row (● / Ctrl+R / F4 / Ctrl+A 行为一致) is demonstrated, not just
// inferred from the shared code path.
func TestF4AndCtrlAWorkOnZhihuRows(t *testing.T) {
	m := zhihuModel(t, []core.Item{
		zhihuHeatItem("100001", "热榜问题一", 0, ip(20450000)),
		zhihuHeatItem("100002", "热榜问题二", 0, nil),
	})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlR}) // 第一行已读

	// F4: the read row drops out of the unread-only view.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF4})
	view := m.View()
	if !strings.Contains(view, "热榜问题二") || strings.Contains(view, "热榜问题一") {
		t.Fatalf("F4 unread-only should hide the read zhihu row:\n%s", view)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF4}) // 回到全部

	// Ctrl+A: modal confirm, Enter commits, every zhihu row read.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	if !m.confirming {
		t.Fatal("Ctrl+A should ask for confirmation first")
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if view = m.View(); strings.Contains(view, "●") {
		t.Fatalf("Ctrl+A should mark every zhihu row read:\n%s", view)
	}
}
