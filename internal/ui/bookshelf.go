// Bookshelf screen for the books source (M6 ticket 02): the scan
// command (进屏扫描 + F5), the ⚠ rows for files the parse layer
// refused, the four-column row layout (标记+标题/作者/进度%/最近阅读),
// and the shelf cursor. The reader itself is ticket 03 — Enter is
// reserved with a hint, or explains the row's failure.
package ui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/smy-101/readscope/internal/epub"
	"github.com/smy-101/readscope/internal/library"
	"github.com/smy-101/readscope/internal/store"
)

// booksMsg lands when a library scan finished (screen entry or F5):
// the shelf rows in store order plus the scan's verdict. Stale
// messages are fine to drop — the scan still freshened the database.
type booksMsg struct {
	rows   []store.BookRow
	result library.Result
	err    error
}

// brokenRow is a file the scan refused that never shelved (no
// metadata row to render): it still gets a visible ⚠ line, titled by
// the full filename — what the user sees in their file manager is
// the only honest name a refused file has.
type brokenRow struct {
	path  string
	title string // filename, extension included
}

// booksSourceID is the books source's stable id — the rail entry it
// occupies and the branch key the shell's bookshelf seams test.
const booksSourceID = "books"

// onBookshelf reports whether the books source's screen is what the
// list area shows right now (favorites is a modal over it, like over
// the feed lists).
func (m Model) onBookshelf() bool {
	return m.activeSourceID() == booksSourceID && !m.favorites
}

// shelfLen is the bookshelf's row count: shelved rows plus the
// synthesized broken-only rows behind them.
func (m Model) shelfLen() int { return len(m.books) + len(m.broken) }

// scanBooksCmd runs the library scan off the UI goroutine and reloads
// the shelf rows in the same command — the bookshelf's loadFeeds and
// F5 are one and the same seam.
func (m Model) scanBooksCmd() tea.Cmd {
	if m.st == nil {
		return nil
	}
	cfg, st := m.cfg, m.st
	return func() tea.Msg {
		dir, err := library.Dir(cfg)
		if err != nil {
			return booksMsg{err: err}
		}
		res, err := library.Scan(context.Background(), st, dir)
		if err != nil {
			return booksMsg{err: err}
		}
		rows, err := st.BookRows(context.Background())
		return booksMsg{rows: rows, result: res, err: err}
	}
}

// booksLanded applies a finished scan: shelf rows, the failure map
// (⚠ markers and Enter explanations), synthesized broken rows for
// files with nothing shelved, and the scan summary in the status bar.
func (m *Model) booksLanded(msg booksMsg) tea.Cmd {
	m.scanning = false
	if msg.err != nil {
		m.status = m.cat.Get("books.err_scan", msg.err)
		return nil
	}
	if !m.onBookshelf() {
		return nil // the user Tabbed away; the db is fresh, the rows are moot
	}
	m.books = msg.rows
	m.failErr = make(map[string]error, len(msg.result.Failed))
	shelved := make(map[string]bool, len(msg.rows))
	for _, r := range msg.rows {
		shelved[r.Path] = true
	}
	m.broken = nil
	for _, f := range msg.result.Failed {
		m.failErr[f.Path] = f.Err
		if !shelved[f.Path] {
			m.broken = append(m.broken, brokenRow{
				path:  f.Path,
				title: filepath.Base(f.Path),
			})
		}
	}
	m.clampBooksCursor()
	m.status = m.cat.Get("books.scan_done", msg.result.Scanned, msg.result.New, msg.result.Delisted)
	return nil
}

// bookshelfKeys owns the bookshelf's list-layer keys while the books
// source is active and the list is focused (the reader-overlay
// pattern): cursor moves and Enter. Region navigation and shell keys
// fall through to the shared switch.
func (m *Model) bookshelfKeys(msg tea.KeyMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "up":
		m.moveBooksSel(-1)
		return true, nil
	case "down":
		m.moveBooksSel(1)
		return true, nil
	case "pgup":
		m.moveBooksSel(-m.bodyHeight())
		return true, nil
	case "pgdown":
		m.moveBooksSel(m.bodyHeight())
		return true, nil
	case "enter":
		m.openBook()
		return true, nil
	}
	return false, nil
}

// moveBooksSel moves the shelf cursor by delta rows (or pages),
// clamped and scroll-windowed like the feed list's cursor.
func (m *Model) moveBooksSel(delta int) {
	m.booksSel, m.booksTop = clampSel(m.booksSel+delta, m.booksTop, m.shelfLen(), m.bodyHeight())
}

// clampBooksCursor keeps the shelf cursor valid when rows change.
func (m *Model) clampBooksCursor() {
	m.booksSel, m.booksTop = clampSel(m.booksSel, m.booksTop, m.shelfLen(), m.bodyHeight())
}

// openBook is Enter on a shelf row: a broken row explains itself with
// the typed parse error (DRM / corrupt zip), a shelved row shows the
// reserved hint — the reader opens with ticket 03.
func (m *Model) openBook() {
	if m.booksSel >= m.shelfLen() {
		return
	}
	path := ""
	if m.booksSel < len(m.books) {
		path = m.books[m.booksSel].Path
	} else {
		path = m.broken[m.booksSel-len(m.books)].path
	}
	if err, broken := m.failErr[path]; broken {
		switch {
		case errors.Is(err, epub.ErrDRMUnsupported):
			m.status = m.cat.Get("books.err_drm")
		case errors.Is(err, epub.ErrInvalidZip):
			m.status = m.cat.Get("books.err_zip")
		default:
			m.status = m.cat.Get("books.err_open", err)
		}
		return
	}
	m.status = m.cat.Get("books.reader_pending")
}

// Column width clamps for the four-column layout: the author column
// sizes to its widest content but never eats the title (the meta
// column's rule from the feed list); the time column is fixed room
// for the longest relative age; the title drops columns before it
// squeezes (list.go's standing rule).
const (
	bookAuthorMinW = 8
	bookAuthorMaxW = 20
	bookTimeW      = 12
	bookPctW       = 4
)

// bookshelfView renders the shelf: ⚠ + title | author | 进度% |
// 最近阅读, rows in store order (in-progress on top) with the
// broken-only rows behind them.
func (m Model) bookshelfView() string {
	w, h := m.listWidth(), m.bodyHeight()
	if m.shelfLen() == 0 {
		return m.th.List.Width(w).Height(h).
			Render(m.th.Dim.Render(m.cat.Get("books.empty")))
	}
	usable := w - 1 // List has PaddingLeft(1)

	authorW := bookAuthorMinW
	for _, r := range m.books {
		if dw := runewidth.StringWidth(r.Author); dw > authorW {
			authorW = dw
		}
	}
	if authorW > bookAuthorMaxW {
		authorW = bookAuthorMaxW
	}
	titleW := usable - 2 /*marker*/ - 1 - authorW - 1 - bookPctW - 1 - bookTimeW
	if titleW < 8 { // narrow: drop the author column before the title
		authorW = 0
		titleW = usable - 2 - 1 - bookPctW - 1 - bookTimeW
	}
	if titleW < 8 { // still narrow: drop the time column too
		titleW = usable - 2 - 1 - bookPctW - 1
	}
	if titleW < 1 {
		titleW = 1
	}

	end := m.booksTop + h
	if end > m.shelfLen() {
		end = m.shelfLen()
	}
	lines := make([]string, 0, end-m.booksTop)
	for i := m.booksTop; i < end; i++ {
		lines = append(lines, m.bookshelfLine(i, titleW, authorW))
	}
	return m.th.List.Width(w).Height(h).Render(strings.Join(lines, "\n"))
}

// bookshelfLine renders shelf row idx with the computed column
// widths: marker (⚠ for the scan's failures, blank else), title,
// author, percent and last-read time; the selected row re-renders
// every segment through ListSelected (the feed list's embedded-reset
// lesson) and pads to the full usable width.
func (m Model) bookshelfLine(idx, titleW, authorW int) string {
	selected := idx == m.booksSel
	var title, author, pct, timeTxt string
	marker := " "
	if idx < len(m.books) {
		r := m.books[idx]
		title, author = r.Title, r.Author
		if _, broken := m.failErr[r.Path]; broken {
			marker = "⚠"
		}
		pct, timeTxt = m.cat.Get("books.never"), m.cat.Get("books.never")
		if r.HasProgress {
			pct = formatPct(r.Pct)
			at := r.ReadAt
			timeTxt = m.relTime(&at)
		}
	} else {
		b := m.broken[idx-len(m.books)]
		title, marker = b.title, "⚠"
		pct, timeTxt = m.cat.Get("books.never"), m.cat.Get("books.never")
	}

	markerStyle, titleStyle, metaStyle := m.th.UnreadDot, m.th.Title, m.th.Meta
	if selected {
		markerStyle, titleStyle, metaStyle = m.th.ListSelected, m.th.ListSelected, m.th.ListSelected
	}
	line := markerStyle.Render(marker+" ") + titleStyle.Render(
		runewidth.FillRight(runewidth.Truncate(title, titleW, "…"), titleW))
	if authorW > 0 {
		author := runewidth.FillLeft(runewidth.Truncate(author, authorW, "…"), authorW)
		line += metaStyle.Render(" " + author)
	}
	line += metaStyle.Render(" " + runewidth.FillLeft(pct, bookPctW))
	if w := runewidth.StringWidth(timeTxt); w < bookTimeW {
		timeTxt = strings.Repeat(" ", bookTimeW-w) + timeTxt
	} else {
		timeTxt = runewidth.Truncate(timeTxt, bookTimeW, "…")
	}
	line += metaStyle.Render(" " + timeTxt)
	if selected {
		usable := m.listWidth() - 1
		if pad := usable - lipgloss.Width(line); pad > 0 {
			line += m.th.ListSelected.Render(strings.Repeat(" ", pad))
		}
	}
	return line
}

// formatPct renders the progress column: a whole-number percent from
// the stored fraction — the shelf and the reader's status show the
// same figure.
func formatPct(f float64) string {
	return fmt.Sprintf("%d%%", int(math.Round(f*100)))
}
