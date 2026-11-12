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
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/store"
	"github.com/smy-101/readscope/internal/theme"

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
	// already sits in the full-text layer. And no question layer
	// either (ticket 06 review): the pager probe must not dress a
	// daily story in a "0 answers" floor list.
	if strings.Contains(view, "no comments") {
		t.Fatalf("daily reader must not show the comments footer:\n%s", view)
	}
	if strings.Contains(view, "0 answers") {
		t.Fatalf("daily reader must not show the question layer's answers footer:\n%s", view)
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

// The restart leg of 离线可读: a body fetched in one model (one
// "process") renders in a FRESH model over the same store with the
// story door dead — the snapshot is the only copy that matters, and
// no dial happens (the dead door would turn any fetch loud).
func TestDailyBodySurvivesRestartOffline(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", t.TempDir())
	t.Setenv("LC_ALL", "en_US.UTF-8")
	srv, hits := dailyStoryServer(t, "9792937", "<p>重启后离线可读的全文。</p>")
	t.Setenv("READSCOPE_ZHIHU_BASE", srv.URL)

	freshModel := func(t *testing.T, st *store.Store) Model {
		t.Helper()
		cfg := core.Defaults()
		cat, err := i18n.Load("en")
		if err != nil {
			t.Fatalf("i18n: %v", err)
		}
		th, err := theme.Load("dark")
		if err != nil {
			t.Fatalf("theme: %v", err)
		}
		m := size(New(cfg, cat, th, st, core.DefaultSources()), 100, 30)
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
		return key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // sidebar Enter: focus the list
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "restart.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, _, err := st.SaveFeedItems(context.Background(), "zhihu", "daily",
		[]core.Item{dailyItem("9792937", "机器知道它在读什么吗", "", "")}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Process one: open the story online — the body lands in the store.
	m1 := freshModel(t, st)
	var cmd tea.Cmd
	m1, cmd = keyCmd(m1, tea.KeyMsg{Type: tea.KeyEnter})
	m1 = pump(t, m1, cmd)
	if n := hits.Load(); n != 1 {
		t.Fatalf("setup: story hits = %d, want 1", n)
	}

	// Process two: fresh model, same store, door dead. The cached
	// body renders — no dial, no error.
	srv.Close()
	m2 := freshModel(t, st)
	m2, cmd = keyCmd(m2, tea.KeyMsg{Type: tea.KeyEnter})
	m2 = pump(t, m2, cmd)
	view := m2.View()
	if !strings.Contains(view, "重启后离线可读的全文") {
		t.Fatalf("restarted model should render the cached body offline:\n%s", view)
	}
	if m2.status != "" {
		t.Fatalf("offline re-open must be silent, status = %q", m2.status)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("restart must not dial the story door again: %d hits", n)
	}
}
