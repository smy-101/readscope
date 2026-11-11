package ui

// Ticket 04 UI tests: read/star/filter/confirm/favorites drive the real
// Update loop through pump — writes land in a real store and the reload
// chain must bring the list, sidebar counts and status bar back in
// sync, exactly what a user sees.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/smy-101/readscope/internal/core"
)

// rssModel seeds one feed with n titled items and lands the model on
// the loaded RSS list (books → rss, focus on the list).
func rssModel(t *testing.T, feedLabel string, n int) Model {
	t.Helper()
	seed := make([]core.Item, 0, n)
	for i := 0; i < n; i++ {
		seed = append(seed, core.Item{
			ID: fmt.Sprintf("rss:ex:%d", i), Source: "rss", Feed: "ex",
			Title: fmt.Sprintf("Story %d", i), URL: "https://example.com/s",
			FetchedAt: time.Now().UTC().Add(-time.Duration(i) * time.Hour),
			Body:      "<p>body</p>",
		})
	}
	m := seededModel(t,
		map[string]core.RSSFeed{"ex": {Label: feedLabel, URL: "http://127.0.0.1:1/x"}},
		map[string][]core.Item{"ex": seed},
	)
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // books → rss
	m = pump(t, m, cmd)
	return m
}

func key(t *testing.T, m Model, msg tea.KeyMsg) Model {
	t.Helper()
	res, cmd := m.Update(msg)
	m2, ok := res.(Model)
	if !ok {
		t.Fatalf("Update returned %T", res)
	}
	return pump(t, m2, cmd)
}

func TestCtrlRTogglesRead(t *testing.T) {
	m := rssModel(t, "Example", 2)
	view := m.View()
	if n := strings.Count(view, "●"); n != 2 {
		t.Fatalf("setup: unread dots = %d, want 2:\n%s", n, view)
	}

	// Ctrl+R on the first item: dot gone, sidebar badge drops to 1.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	view = m.View()
	if n := strings.Count(view, "●"); n != 1 {
		t.Fatalf("after Ctrl+R: unread dots = %d, want 1:\n%s", n, view)
	}
	if !strings.Contains(view, "1 new") {
		t.Fatalf("sidebar/status counts should drop to 1 new:\n%s", view)
	}

	// Toggle back: unread again.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if n := strings.Count(m.View(), "●"); n != 2 {
		t.Fatalf("after second Ctrl+R: unread dots = %d, want 2", n)
	}
}

func TestCtrlRPersistsAcrossRestart(t *testing.T) {
	m := rssModel(t, "Example", 3)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})

	// "Restart": a fresh model over the same store must see 1 read row.
	// seededModel writes to a fresh DB each call, so rebuild via New.
	fresh := New(m.cfg, m.cat, m.th, m.st, m.srcs)
	fresh = size(fresh, 100, 30)
	fresh, cmd := keyCmd(fresh, tea.KeyMsg{Type: tea.KeyTab})
	fresh = pump(t, fresh, cmd)
	if n := strings.Count(fresh.View(), "●"); n != 2 {
		t.Fatalf("read state lost across restart: dots = %d, want 2", n)
	}
}

func TestCtrlFStarsAndF3ListsFavorites(t *testing.T) {
	m := rssModel(t, "Example", 2)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyRight}) // sidebar → list
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyDown})  // select Story 1

	// Ctrl+F stars the selected row: ★ replaces the dot.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlF})
	view := m.View()
	if !strings.Contains(view, "★") || strings.Count(view, "●") != 1 {
		t.Fatalf("after Ctrl+F: want ★ on Story 1 and ● on Story 0:\n%s", view)
	}

	// F3 opens the favorites list: only the starred item, cross-feed.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF3})
	view = m.View()
	if !strings.Contains(view, "Favorites") {
		t.Fatalf("F3 should show the favorites list:\n%s", view)
	}
	if strings.Contains(view, "Story 0") || !strings.Contains(view, "Story 1") {
		t.Fatalf("favorites list should hold only the starred item:\n%s", view)
	}

	// Enter opens it, Esc drops back to the favorites list, Esc again
	// returns to the feed list with everything still starred.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.reader == nil {
		t.Fatal("Enter in favorites should open the reader")
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.favorites {
		t.Fatal("Esc from the reader should still be in favorites")
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	view = m.View()
	if m.favorites || !strings.Contains(view, "Story 0") {
		t.Fatalf("Esc should close favorites back onto the feed list:\n%s", view)
	}
	if !strings.Contains(view, "★") {
		t.Fatal("star marker lost after favorites round-trip")
	}

	// Unstar from the list: ★ disappears.
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlF})
	if strings.Contains(m.View(), "★") {
		t.Fatalf("unstar should drop the ★ marker:\n%s", m.View())
	}
}

func TestFavoriteSurvivesCacheEviction(t *testing.T) {
	m := rssModel(t, "Example", 1)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlF})

	// Simulate the source item disappearing (cache eviction): the
	// favorites list must still show and open the snapshot.
	if _, err := m.st.Exec(t.Context(), `DELETE FROM items`); err != nil {
		t.Fatalf("evict: %v", err)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF3})
	view := m.View()
	if !strings.Contains(view, "Story 0") {
		t.Fatalf("favorite must survive eviction of its items row:\n%s", view)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.reader == nil || !strings.Contains(m.View(), "body") {
		t.Fatalf("evicted favorite should still open and read:\n%s", m.View())
	}
}

func TestF4FiltersUnreadAndPersists(t *testing.T) {
	m := rssModel(t, "Example", 3)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlR}) // Story 0 read

	m = key(t, m, tea.KeyMsg{Type: tea.KeyF4})
	view := m.View()
	if strings.Contains(view, "Story 0") {
		t.Fatalf("F4 should hide the read item:\n%s", view)
	}
	if !strings.Contains(view, "Story 1") || !strings.Contains(view, "Story 2") {
		t.Fatalf("F4 should keep the unread items:\n%s", view)
	}
	if !strings.Contains(view, "unread only") {
		t.Fatalf("status bar should show the filter indicator:\n%s", view)
	}

	// The filter survives a restart: fresh model, same config + store.
	fresh := New(m.cfg, m.cat, m.th, m.st, m.srcs)
	fresh = size(fresh, 100, 30)
	fresh, cmd := keyCmd(fresh, tea.KeyMsg{Type: tea.KeyTab})
	fresh = pump(t, fresh, cmd)
	if fresh.View() == "" || strings.Contains(fresh.View(), "Story 0") {
		t.Fatalf("unread_only should be restored at startup:\n%s", fresh.View())
	}
	if !strings.Contains(fresh.View(), "Story 2") {
		t.Fatalf("filtered list should still hold unread items:\n%s", fresh.View())
	}

	// Toggling off brings everything back.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF4})
	if !strings.Contains(m.View(), "Story 0") {
		t.Fatalf("F4 off should show read items again:\n%s", m.View())
	}
}

func TestCtrlAMarksAllReadWithConfirmation(t *testing.T) {
	m := rssModel(t, "Example", 3)

	// Ctrl+A asks first: modal naming the feed, nothing marked yet.
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	view := m.View()
	if !strings.Contains(view, "Example") || !strings.Contains(view, "Enter") {
		t.Fatalf("Ctrl+A should confirm before marking:\n%s", view)
	}
	// Esc cancels: dialog gone, everything still unread.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	view = m.View()
	if strings.Contains(view, "Mark every item") {
		t.Fatalf("Esc should close the confirmation:\n%s", view)
	}
	if n := strings.Count(view, "●"); n != 3 {
		t.Fatalf("cancel must not mark anything: dots = %d", n)
	}

	// Confirmed: every item read, dots and badges drain.
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	view = m.View()
	if n := strings.Count(view, "●"); n != 0 {
		t.Fatalf("all read: dots = %d, want 0:\n%s", n, view)
	}
	if !strings.Contains(view, "all marked read") {
		t.Fatalf("status should confirm the bulk mark:\n%s", view)
	}

	// In the favorites view Ctrl+A has no feed to drain: no dialog.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF3})
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	if m.confirming {
		t.Fatal("Ctrl+A must not confirm inside favorites")
	}
}

func TestDetailCtrlRCtrlF(t *testing.T) {
	m := rssModel(t, "Example", 2)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyRight}) // focus the list
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})    // open Story 0

	// Ctrl+R while reading marks the open item read.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if !m.reader.item.Read {
		t.Fatal("Ctrl+R in the reader should mark the item read")
	}
	if !strings.Contains(m.status, "marked read") {
		t.Fatalf("status should acknowledge: %q", m.status)
	}
	// The list below already reflects it once we drop back.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if n := strings.Count(m.View(), "●"); n != 1 {
		t.Fatalf("read toggle from the reader lost: dots = %d, want 1", n)
	}

	// Reopen and star it from the reader.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlF})
	if !m.reader.item.Starred {
		t.Fatal("Ctrl+F in the reader should star the item")
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if !strings.Contains(m.View(), "★") {
		t.Fatalf("star from the reader should show in the list:\n%s", m.View())
	}

	// Toggling back from the reader unstars.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlF})
	if m.reader.item.Starred {
		t.Fatal("second Ctrl+F should unstar")
	}
}
