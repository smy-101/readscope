package ui

// Ticket 01 UI tests for the heybox source: Tab onto 小黑盒 — the
// sidebar fills with the recommend feed (label resolved through
// i18n), cached post cards render the award/comment radar meta, the
// zero-segment drop keeps bare rows clean, and a risk-control fetch
// failure renders the 「稍后再试」 line instead of a raw error.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source/heybox"
)

// heyboxCard builds one cached post card with the full heat snapshot.
func heyboxCard(hexID, title, author string, award, comments *int) core.Item {
	return core.Item{
		ID: "hb:link/" + hexID, Source: "heybox", Feed: "recommend",
		Title: title, URL: "https://api.xiaoheihe.cn/v3/bbs/app/api/web/share?link_id=" + hexID,
		Author: author, FetchedAt: time.Now().UTC(),
		Points: award, NumComments: comments,
	}
}

// heyboxModel seeds the recommend feed and lands the model on the
// heybox list (books → rss → hn → zhihu → heybox, focus on the list).
func heyboxModel(t *testing.T, items []core.Item) Model {
	t.Helper()
	m := seededModel(t, nil, nil)
	if _, _, err := m.st.SaveFeedItems(context.Background(), "heybox", "recommend", items); err != nil {
		t.Fatalf("seed heybox items: %v", err)
	}
	m = size(m, 120, 30)
	for range 4 { // books → rss → hn → zhihu → heybox
		var cmd tea.Cmd
		m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
		m = pump(t, m, cmd)
	}
	return m
}

func TestHeyboxTabShowsRecommendFeedAndCachedItems(t *testing.T) {
	m := heyboxModel(t, []core.Item{
		heyboxCard("236172cefddd", "背刺拉满！Steam史低", "知许。", ip(2108), ip(102)),
		heyboxCard("7db172cf6006", "无热度帖", "新人", ip(0), ip(0)),
		heyboxCard("ab5972cdd6b3", "只有作者", "菠萝少侠c", nil, nil),
	})
	if m.activeSourceID() != "heybox" {
		t.Fatalf("active source = %q, want heybox", m.activeSourceID())
	}
	view := m.View()
	// Sidebar: the recommend feed, its label resolved through i18n.
	if !strings.Contains(view, "For You") {
		t.Errorf("sidebar missing the recommend feed label:\n%s", view)
	}
	if strings.Contains(view, "heybox.recommend") {
		t.Error("the raw i18n key must never render")
	}
	// The heat radar: award + comment counts + author, hn's shape.
	if !strings.Contains(view, "2108 up") || !strings.Contains(view, "102 comments") {
		t.Errorf("missing the award/comment radar segments:\n%s", view)
	}
	// Zero segments drop (0 up / 0 comments say nothing), author
	// stays; a bare card renders the author alone — never an empty
	// "· ·" run.
	if strings.Contains(view, "0 up") || strings.Contains(view, "0 comments") {
		t.Errorf("zero-valued heat segments must drop:\n%s", view)
	}
	if !strings.Contains(view, "新人") || !strings.Contains(view, "菠萝少侠c") {
		t.Errorf("authors missing from the meta column:\n%s", view)
	}
}

// TestHeyboxRiskRefusalRendersTryLater: a fetch failure of the risk
// family becomes the 稍后再试 status line — no raw error, no retry
// ceremony (an anonymous source has nothing to re-login into).
func TestHeyboxRiskRefusalRendersTryLater(t *testing.T) {
	m := heyboxModel(t, []core.Item{heyboxCard("236172cefddd", "缓存里的帖子", "知许。", ip(3), ip(9))})
	label := m.cat.Get("heybox.recommend")
	got := m.fetchErrText(label,
		fmt.Errorf("fetch recommend: heybox: risk control refused — try later: %w", heybox.ErrTryLater))
	if !strings.Contains(got, "try later") && !strings.Contains(got, "稍后再试") {
		t.Fatalf("risk refusal rendered %q, want the try-later line", got)
	}
	// A plain network error stays raw — only the risk family gets the
	// sugar-coating.
	raw := m.fetchErrText("recommend", errors.New("dial tcp: refused"))
	if !strings.Contains(raw, "dial tcp") {
		t.Fatalf("plain error rendered %q, want the raw error kept", raw)
	}
}
