package ui

// Ticket 06 acceptance pins: the M2 read-state keys are shared
// machinery, but the acceptance line asks them to work *on HN rows*
// — this drives the real Update loop over an hn list so a regression
// in feed scoping or row mapping fails here, not in the field.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Ctrl+R clears the unread dot on an hn row, F4 filters the read row
// out, Ctrl+A (confirmed) drains the hn feed and only that feed.
func TestHNReadStateKeysWorkOnHNRows(t *testing.T) {
	m := hnModel(t, 3)
	if n := strings.Count(m.View(), "●"); n != 3 {
		t.Fatalf("setup: unread dots = %d, want 3:\n%s", n, m.View())
	}

	// Ctrl+R on the first hn row: dot drains, badge drops to 2.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	view := m.View()
	if n := strings.Count(view, "●"); n != 2 {
		t.Fatalf("Ctrl+R on hn row: dots = %d, want 2:\n%s", n, view)
	}
	if !strings.Contains(view, "2 new") {
		t.Fatalf("badge should read 2 new after Ctrl+R:\n%s", view)
	}

	// F4: the read hn row hides, the unread stay.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF4})
	view = m.View()
	if strings.Contains(view, "HN story 0") {
		t.Fatalf("F4 should hide the read hn row:\n%s", view)
	}
	if !strings.Contains(view, "HN story 1") || !strings.Contains(view, "HN story 2") {
		t.Fatalf("F4 should keep unread hn rows:\n%s", view)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF4}) // back to all

	// Ctrl+A confirmed: the hn feed drains wholesale.
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if n := strings.Count(m.View(), "●"); n != 0 {
		t.Fatalf("Ctrl+A should mark the hn feed read: dots = %d, want 0:\n%s", n, m.View())
	}
}

// A starred hn story whose items row disappears (fell off the feed,
// cache replaced) must still list in F3 and open readable — the
// snapshot, not the cache, backs the favorite.
func TestHNFavoriteSurvivesFeedEviction(t *testing.T) {
	m := hnModel(t, 1)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlF})
	if !strings.Contains(m.View(), "★") {
		t.Fatalf("setup: hn row should carry the star:\n%s", m.View())
	}

	// The story falls off the Algolia front page: its items row goes.
	if _, err := m.st.Exec(t.Context(), `DELETE FROM items WHERE source = 'hn'`); err != nil {
		t.Fatalf("evict hn rows: %v", err)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF3})
	view := m.View()
	if !strings.Contains(view, "HN story 0") {
		t.Fatalf("evicted hn favorite must still list in F3:\n%s", view)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.reader == nil {
		t.Fatal("evicted hn favorite should still open")
	}
	if !strings.Contains(m.View(), "https://example.com/s") {
		t.Fatalf("evicted hn favorite should still be readable (link line body):\n%s", m.View())
	}
}
