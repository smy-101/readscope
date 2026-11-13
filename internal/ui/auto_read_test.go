package ui

// auto-read-on-open UI tests: Enter opens a detail and silently marks
// the item read — the write rides the same stateSavedMsg reload chain
// as the manual toggles, with no "marked read" flash (opening is its
// own feedback). Already-read opens kick no write at all; the unread
// dot semantics pinned by the GLOSSARY (打开即消, Ctrl+R 可恢复) hold
// across sources and restarts, and unread-only drops the opened item
// on Esc-back exactly like a manual Ctrl+R does.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	_ "github.com/smy-101/readscope/internal/source/hn" // register "hn", as main does
)

// openFirstRSS lands the list focus and opens item 0, pumping the
// whole open chain (auto read write + reload included).
func openFirstRSS(t *testing.T, m Model) Model {
	t.Helper()
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnter}) // sidebar → list focus
	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // open item 0
	if m.reader == nil {
		t.Fatal("Enter on the list must open the detail layer")
	}
	return pump(t, m, cmd)
}

func TestOpenMarksReadSilently(t *testing.T) {
	m := rssModel(t, "Example", 3)
	if n := strings.Count(m.View(), "●"); n != 3 {
		t.Fatalf("setup: unread dots = %d, want 3", n)
	}

	m = openFirstRSS(t, m)

	// Silent: the auto mark must not flash "marked read" — that is the
	// manual toggles' feedback, and every Enter would be noise. The
	// assertion reads the string through the i18n catalog, not hardcoded
	// English (decision #8).
	if strings.Contains(m.status, m.cat.Get("status.marked_read")) {
		t.Fatalf("auto mark should be silent, status = %q", m.status)
	}
	// Esc back: the opened row lost its dot, sidebar count dropped.
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	view := m.View()
	if n := strings.Count(view, "●"); n != 2 {
		t.Fatalf("after open+Esc: unread dots = %d, want 2:\n%s", n, view)
	}
	if !strings.Contains(view, m.cat.Get("status.new", 2)) {
		t.Fatalf("sidebar count should read 2 new:\n%s", view)
	}
}

func TestOpenMarksReadPersistsAcrossRestart(t *testing.T) {
	m := openFirstRSS(t, rssModel(t, "Example", 3))

	fresh := New(m.cfg, m.cat, m.th, m.st, m.srcs)
	fresh = size(fresh, 100, 30)
	var cmd tea.Cmd
	fresh, cmd = keyCmd(fresh, tea.KeyMsg{Type: tea.KeyTab}) // books → rss
	fresh = pump(t, fresh, cmd)
	view := fresh.View()
	if !strings.Contains(view, "Story 0") {
		t.Fatalf("read item should still be listed:\n%s", view)
	}
	if n := strings.Count(view, "●"); n != 2 {
		t.Fatalf("restart: unread dots = %d, want 2 (Story 0 stays read)", n)
	}
	// F4 keeps only the unread ones.
	fresh = key(t, fresh, tea.KeyMsg{Type: tea.KeyF4})
	view = fresh.View()
	if strings.Contains(view, "Story 0") {
		t.Fatalf("F4 should hide the auto-read item:\n%s", view)
	}
	if !strings.Contains(view, "Story 2") {
		t.Fatalf("F4 should keep the unread items:\n%s", view)
	}
}

func TestOpenAlreadyReadItemWritesNothing(t *testing.T) {
	m := rssModel(t, "Example", 3)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlR}) // item 0 read, the manual way

	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnter}) // sidebar → list focus
	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // open the read item
	if m.reader == nil {
		t.Fatal("Enter on the list must open the detail layer")
	}
	// No unread mark to write: the open kicks no command at all (no
	// read_at refresh, no reload chain).
	if cmd != nil {
		t.Fatalf("opening an already-read item should kick no command, got %T", cmd)
	}
}

func TestOpenThenCtrlRInDetailMarksUnreadAgain(t *testing.T) {
	m := openFirstRSS(t, rssModel(t, "Example", 2))

	// The manual toggle still talks: Ctrl+R in the detail flips the
	// auto-marked item back to unread, with its status flash intact.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if !strings.Contains(m.status, m.cat.Get("status.marked_unread")) {
		t.Fatalf("manual Ctrl+R should keep its status, got %q", m.status)
	}
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if n := strings.Count(m.View(), "●"); n != 2 {
		t.Fatalf("after Ctrl+R back: unread dots = %d, want 2", n)
	}
}

func TestOpenDropsItemFromUnreadOnlyList(t *testing.T) {
	m := rssModel(t, "Example", 3)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF4})

	// Open item 0 under the filter: the write + reload drop it from
	// the filtered list behind the detail (same shape as a manual
	// Ctrl+R there) — Esc lands on the remaining unread rows.
	m = openFirstRSS(t, m)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	view := m.View()
	if strings.Contains(view, "Story 0") {
		t.Fatalf("opened item should leave the unread-only list:\n%s", view)
	}
	if !strings.Contains(view, "Story 1") || !strings.Contains(view, "Story 2") {
		t.Fatalf("unread rows should survive the reload:\n%s", view)
	}
}

func TestOpenMarksReadOnHN(t *testing.T) {
	// Same open path, other source: the auto mark is uniform across
	// feeds — the single HN story loses its dot after one open.
	base, _ := hnDetailServer(t, hnDetailJSON())
	m := openHNDetail(t, nil, base)
	if strings.Contains(m.status, m.cat.Get("status.marked_read")) {
		t.Fatalf("auto mark should be silent, status = %q", m.status)
	}
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if n := strings.Count(m.View(), "●"); n != 0 {
		t.Fatalf("after open+Esc: unread dots = %d, want 0 (the only story is read)", n)
	}
}
