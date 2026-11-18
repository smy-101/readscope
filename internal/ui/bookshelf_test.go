package ui

// Bookshelf screen tests (M6 ticket 02): entering the books source
// scans the library dir (upsert + ⚠ rows for hard failures), the
// four-column rows keep the shelf order, Enter is reserved with a
// hint (the reader is ticket 03), F5 rescans, and the composed
// status line is asserted in both languages (CODING_STANDARDS: i18n
// assertions ride composed strings, zh and en).

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/epub/fixture"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/store"
	"github.com/smy-101/readscope/internal/theme"
)

// bookshelfModel builds a model whose active source is books (the
// startup source) over a real store and a library dir the test fills
// by hand.
func bookshelfModel(t *testing.T, lang string) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(t.TempDir(), "shelf.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := core.Defaults()
	cfg.BookDir = dir
	cat, err := i18n.Load(lang)
	if err != nil {
		t.Fatalf("i18n %s: %v", lang, err)
	}
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatalf("theme: %v", err)
	}
	m := New(cfg, cat, th, st, core.DefaultSources())
	return m, dir
}

func shelveIn(t *testing.T, dir, name string, files []fixture.File) string {
	t.Helper()
	src := fixture.Write(t, files)
	dst := filepath.Join(dir, name)
	if err := os.Rename(src, dst); err != nil {
		t.Fatalf("shelve %s: %v", name, err)
	}
	return dst
}

func TestBookshelfScansOnEntry(t *testing.T) {
	m, dir := bookshelfModel(t, "en")
	shelveIn(t, dir, "good.epub", fixture.ValidEPUB3())
	shelveIn(t, dir, "drm.epub", fixture.DRMEncrypted())

	m2 := size(m, 100, 30)
	m2 = pump(t, m2, m2.Init())
	view := m2.View()
	for _, want := range []string{
		"All books",          // sidebar entry
		"合成书 · EPUB3",        // shelved row title (metadata, not filename)
		"作者甲",                // author column
		"⚠ drm.epub",         // broken-but-visible row: marker + filename title
		"2 books · 1 broken", // composed status line
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestBookshelfOrderAndProgressColumns(t *testing.T) {
	m, dir := bookshelfModel(t, "en")
	shelveIn(t, dir, "read.epub", fixture.ValidEPUB3())
	shelveIn(t, dir, "fresh.epub", fixture.MinimalEPUB3())
	m = size(m, 100, 30)
	m = pump(t, m, m.Init())

	// Seed reading progress on read.epub: it must top the shelf and
	// show its percent; the unopened book shows the never marker.
	rows, err := m.st.BookRows(context.Background())
	if err != nil || len(rows) != 2 {
		t.Fatalf("BookRows = %+v, %v", rows, err)
	}
	var readID int64
	for _, r := range rows {
		if filepath.Base(r.Path) == "read.epub" {
			readID = r.ID
		}
	}
	if _, err := m.st.Exec(context.Background(),
		`INSERT INTO progress (book_id, spine_index, block_offset, pct, updated_at) VALUES (?, 0, 3, 0.5, ?)`,
		readID, time.Now().UTC()); err != nil {
		t.Fatalf("seed progress: %v", err)
	}
	m = pump(t, m, m.scanBooksCmd()) // rescan refreshes rows

	view := m.View()
	if !strings.Contains(view, "50%") {
		t.Errorf("percent column missing 50%%:\n%s", view)
	}
	if !strings.Contains(view, "—") {
		t.Errorf("unopened book must show the never marker:\n%s", view)
	}
	readAt := strings.Index(view, "合成书 · EPUB3")
	freshAt := strings.Index(view, "最小 EPUB3")
	if readAt < 0 || freshAt < 0 || readAt > freshAt {
		t.Errorf("in-progress book must sort first (read@%d fresh@%d):\n%s", readAt, freshAt, view)
	}
}

func TestBookshelfEnterOpensAndBrokenExplains(t *testing.T) {
	m, dir := bookshelfModel(t, "en")
	shelveIn(t, dir, "good.epub", fixture.ValidEPUB3())
	shelveIn(t, dir, "drm.epub", fixture.DRMEncrypted())
	m = size(m, 100, 30)
	m = pump(t, m, m.Init())

	// Enter on a shelved book starts the reader's open chain (ticket
	// 03 took the key over from the reserved hint).
	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.status, "Opening") {
		t.Fatalf("Enter on a shelved book should start the open, got %q", m.status)
	}
	m = pump(t, m, cmd)
	if m.book == nil {
		t.Fatalf("the reader should be open:\n%s", m.View())
	}

	// The broken row sits after the shelved rows; walk down to it.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.status, "DRM-protected") {
		t.Fatalf("Enter on a broken book should explain the failure, got %q", m.status)
	}
}

func TestBookshelfF5Rescans(t *testing.T) {
	m, dir := bookshelfModel(t, "en")
	shelveIn(t, dir, "first.epub", fixture.ValidEPUB3())
	m = size(m, 100, 30)
	m = pump(t, m, m.Init())

	shelveIn(t, dir, "second.epub", fixture.MinimalEPUB3())
	m2, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyF5})
	if cmd == nil {
		t.Fatal("F5 on the bookshelf must fire a rescan")
	}
	m2 = pump(t, m2, cmd)
	if !strings.Contains(m2.View(), "最小 EPUB3") {
		t.Fatalf("F5 rescan must pick up newly shelved books:\n%s", m2.View())
	}
	if m2.scanning {
		t.Fatal("scanning flag must clear when the scan lands")
	}
}

// Narrow terminals must not wrap: every visible line stays within the
// terminal width even with long CJK titles and authors in play (the
// list-layer sibling of the detail layer's assertLinesFit).
func TestBookshelfFitsNarrowWidth(t *testing.T) {
	m, dir := bookshelfModel(t, "en")
	shelveIn(t, dir, "long.epub", fixture.ValidEPUB3()) // CJK title/author
	shelveIn(t, dir, "drm.epub", fixture.DRMEncrypted())
	m = size(m, 40, 30)
	m = pump(t, m, m.Init())
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 40 {
			t.Fatalf("line %d overflows narrow terminal (w=%d): %q", i, w, line)
		}
	}
}

// The composed status line, asserted in Chinese too: parity tests
// cannot catch a bad composition, only a missing key.
func TestBookshelfStatusComposedZH(t *testing.T) {
	m, dir := bookshelfModel(t, "zh-CN")
	shelveIn(t, dir, "good.epub", fixture.ValidEPUB3())
	shelveIn(t, dir, "drm.epub", fixture.DRMEncrypted())
	m = size(m, 100, 30)
	m = pump(t, m, m.Init())
	for _, want := range []string{"全部书籍", "2 本 · 1 册损坏", "扫描"} {
		if !strings.Contains(m.View(), want) {
			t.Errorf("zh view missing %q:\n%s", want, m.View())
		}
	}
	// The scan-done line composes counts too: F5 → pump → exact zh string.
	m2, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyF5})
	m2 = pump(t, m2, cmd)
	if !strings.Contains(m2.View(), "扫描 2 本 · 新上架 0 · 下架 0") {
		t.Fatalf("zh scan_done not composed:\n%s", m2.View())
	}
}
