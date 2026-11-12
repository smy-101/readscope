package ui

// Ticket 03 UI tests: the daily digest in the sidebar (label resolved
// through i18n, hint riding the meta column), and the reader's lazy
// body — Enter on a body-less daily row fetches the story through
// the real BodySource → refresh.Body → store pipeline and renders it;
// a re-open renders the snapshot without a second request; an
// unreachable story door on a first open is a loud, named failure
// (the ticket's 未开条目离线时明确提示).

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/smy-101/readscope/internal/core"

	_ "github.com/smy-101/readscope/internal/source/zhihu" // register "zhihu", as main does
)

func dailyItem(id, title, hint, body string) core.Item {
	return core.Item{
		ID: "zh:daily/" + id, Source: "zhihu", Feed: "daily",
		Title: title, URL: "https://daily.zhihu.com/story/" + id,
		Meta: hint, Body: body, FetchedAt: time.Now().UTC(),
	}
}

// dailyModel seeds the daily feed's rows and lands the model on the
// zhihu source with the daily feed selected (sidebar slot 3:
// hot → follow → daily).
func dailyModel(t *testing.T, items []core.Item) Model {
	t.Helper()
	m := seededModel(t, nil, nil)
	if _, _, err := m.st.SaveFeedItems(context.Background(), "zhihu", "daily", items); err != nil {
		t.Fatalf("seed daily items: %v", err)
	}
	m = size(m, 100, 30)
	for range 3 { // books → rss → hn → zhihu
		var cmd tea.Cmd
		m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
		m = pump(t, m, cmd)
	}
	for range 2 { // sidebar: hot → follow → daily
		var cmd tea.Cmd
		m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyDown})
		m = pump(t, m, cmd)
	}
	if m.selectedFeedKey() != "daily" {
		t.Fatalf("selected feed = %q, want daily", m.selectedFeedKey())
	}
	return m
}

func TestDailyFeedSidebarAndHintMeta(t *testing.T) {
	m := dailyModel(t, []core.Item{
		dailyItem("9792937", "机器知道它在读什么吗", "AI 新榜单 · 12 分钟阅读", ""),
		dailyItem("9792938", "没有 hint 的条目", "", ""),
	})
	view := m.View()
	if !strings.Contains(view, "Daily") {
		t.Errorf("sidebar missing the daily feed label:\n%s", view)
	}
	if strings.Contains(view, "zhihu.daily") {
		t.Errorf("label keys must resolve, raw key leaked:\n%s", view)
	}
	if !strings.Contains(view, "AI 新榜单 · 12 分钟阅读") {
		t.Errorf("the hint should ride the meta column:\n%s", view)
	}
	// Hot rows keep their heat meta — the per-feed routing (02
	// review's transfer) must not regress the board.
	if m2 := zhihuModel(t, []core.Item{
		zhihuHeatItem("100001", "热榜问题", 0, ip(20450000)),
	}); !strings.Contains(m2.View(), "2045万 heat") {
		t.Errorf("hot board's heat meta regressed:\n%s", m2.View())
	}
}

// dailyStoryServer counts its story hits — the zero-request contract
// of a second open is asserted against the counter.
func dailyStoryServer(t *testing.T, storyID, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/4/story/"+storyID {
			hits.Add(1)
			fmt.Fprintf(w, `{"title":"t","body":%q}`, body)
			return
		}
		http.Error(w, "no fixture", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestDailyEnterFetchesBodyAndRenders(t *testing.T) {
	srv, hits := dailyStoryServer(t, "9792937", "<p>全文正文，离线快照用。</p>")
	t.Setenv("READSCOPE_ZHIHU_BASE", srv.URL)

	m := dailyModel(t, []core.Item{dailyItem("9792937", "机器知道它在读什么吗", "AI 新榜单 · 12 分钟阅读", "")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // sidebar Enter: focus the list
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // list Enter: open the reader
	m = pump(t, m, cmd)
	view := m.View()
	if !strings.Contains(view, "全文正文，离线快照用") {
		t.Fatalf("reader should render the fetched story:\n%s", view)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("story hits = %d, want exactly 1", n)
	}
	// No comments footer: the digest has no comment layer — Enter
	// already sits in the full-text layer.
	if strings.Contains(view, "no comments") {
		t.Fatalf("daily reader must not show the comments footer:\n%s", view)
	}

	// Second open: the snapshot renders, zero new requests.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	view = m.View()
	if !strings.Contains(view, "全文正文，离线快照用") {
		t.Fatalf("second open should render the cached body:\n%s", view)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("second open dialed the story door again: %d hits, want 1", n)
	}
}

func TestDailyOfflineFirstOpenIsLoud(t *testing.T) {
	// A door that answers nothing: every dial fails (the ticket's
	// 未开条目离线时明确提示).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()
	t.Setenv("READSCOPE_ZHIHU_BASE", srv.URL)

	m := dailyModel(t, []core.Item{dailyItem("9792937", "机器知道它在读什么吗", "", "")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // sidebar Enter: focus the list
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // list Enter: open the reader
	m = pump(t, m, cmd)
	if m.status == "" || !strings.Contains(m.status, "body fetch failed") {
		t.Fatalf("offline first open should be a loud named failure, status = %q", m.status)
	}
	// The reader stays up with the no-body placeholder — the row is
	// still readable metadata (title, hint), just no cached text yet.
	if !strings.Contains(m.View(), "机器知道它在读什么吗") {
		t.Fatalf("the reader should stay open with the row's own metadata:\n%s", m.View())
	}
}
