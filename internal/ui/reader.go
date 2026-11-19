// reader.go: the EPUB reader (M6 ticket 03) — an independent
// full-screen over the bookshelf (ADR #11): left TOC sidebar, right
// viewport over the continuous flow. This file holds the production
// feeder (spine chapters through the parse layer and the shared
// pipeline); the screen state, key pump and view land with it as the
// slices accrete.
package ui

import (
	"bytes"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/smy-101/readscope/internal/epub"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/render"
	"github.com/smy-101/readscope/internal/store"
)

// epubFeeder is the flow engine's production feeder: spine chapters
// parsed on demand and laid out by the engine. The chapter title
// chain (the ticket's fallback order): the first TOC item targeting
// the chapter → the chapter's first heading → the entry's base name.
// Degradation contract: a read failure or a textless chapter (SVG
// page) answers with a placeholder paragraph — never zero blocks,
// never an error (the engine's stitcher contract); the degraded/
// lastErr pair is the status-bar half, consumed by the key pump's
// maybeBookChErrStatus.
type epubFeeder struct {
	bk     *epub.Book
	cat    *i18n.Catalog
	titles map[int]string // spine idx → TOC title, first match wins
	cache  map[int]string // resolved display titles (title() reads chapters)

	// Degradation accounting: every placeholder the feeder served
	// (unreadable or textless chapter) bumps degraded and records
	// lastErr — the reader's key pump turns a fresh bump into the
	// status bar's 明确错误 line (maybeBookChErrStatus), while the
	// flow itself carries the placeholder paragraph.
	degraded int
	lastErr  error
}

func newEpubFeeder(bk *epub.Book, cat *i18n.Catalog) *epubFeeder {
	f := &epubFeeder{bk: bk, cat: cat, titles: make(map[int]string, len(bk.TOC)), cache: map[int]string{}}
	hrefIdx := make(map[string]int, len(bk.Spine))
	for i, s := range bk.Spine {
		if _, ok := hrefIdx[s.Href]; !ok {
			hrefIdx[s.Href] = i
		}
	}
	var walk func(items []epub.TOCItem)
	walk = func(items []epub.TOCItem) {
		for _, it := range items {
			if idx, ok := hrefIdx[it.Href]; ok && f.titles[idx] == "" {
				f.titles[idx] = it.Title
			}
			walk(it.Children)
		}
	}
	walk(bk.TOC)
	return f
}

func (f *epubFeeder) Len() int { return len(f.bk.Spine) }

// Chapter is the feeder contract: a non-empty content document plus
// the chapter's display title.
func (f *epubFeeder) Chapter(idx int) (*render.Document, string) {
	href := f.bk.Spine[idx].Href
	doc, err := f.chapterDoc(idx)
	switch {
	case err != nil:
		f.degraded++
		f.lastErr = err
		return f.placeholderDoc(f.cat.Get("reader.ch_err", err)), fileTitle(href)
	case len(doc.Blocks) == 0:
		f.degraded++
		f.lastErr = nil
		return f.placeholderDoc(f.cat.Get("reader.no_text")), f.titles[idx]
	}
	if title := f.titles[idx]; title != "" {
		return doc, title
	}
	if title := firstHeadingTitle(doc); title != "" {
		return doc, title
	}
	return doc, fileTitle(href)
}

// title is the chapter's display title on its own (the TOC sidebar's
// current-chapter label and the bookmark rows ride it) — the same
// chain Chapter applies, memoized: the fallback links read chapters.
func (f *epubFeeder) title(idx int) string {
	if t, ok := f.cache[idx]; ok {
		return t
	}
	t := f.titles[idx]
	if t == "" {
		href := f.bk.Spine[idx].Href
		if doc, err := f.chapterDoc(idx); err == nil {
			t = firstHeadingTitle(doc)
		}
		if t == "" {
			t = fileTitle(href)
		}
	}
	f.cache[idx] = t
	return t
}

// chapterDoc reads and parses the spine chapter's document.
func (f *epubFeeder) chapterDoc(idx int) (*render.Document, error) {
	data, err := f.bk.ReadItem(f.bk.Spine[idx].Href)
	if err != nil {
		return nil, err
	}
	return render.ParseHTML(string(data))
}

// placeholderDoc is the degraded chapter: one dim paragraph carrying
// the verdict (no-text or the read failure).
func (f *epubFeeder) placeholderDoc(text string) *render.Document {
	return &render.Document{Blocks: []render.Block{render.Para{Spans: []render.Span{
		{Text: text, Style: render.SpanDim},
	}}}}
}

// firstHeadingTitle is the chapter's first heading's text ("" when
// none) — the middle link of the title chain.
func firstHeadingTitle(doc *render.Document) string {
	for _, b := range doc.Blocks {
		if h, ok := b.(render.Heading); ok {
			if t := strings.TrimSpace(headingPlainText(h)); t != "" {
				return t
			}
		}
	}
	return ""
}

// fileTitle is the chain's last link: the entry's base name without
// extension.
func fileTitle(href string) string {
	return strings.TrimSuffix(path.Base(href), path.Ext(href))
}

// --- the reader screen (ADR #11: 独立全屏) ------------------------------

// readerTocW is the TOC sidebar's width (the shell sidebar's width —
// one visual column system across screens).
const readerTocW = sidebarWidth

// tocRow is one flattened TOC entry; spine is the jump target's spine
// index (-1 never happens: entries without a spine target are dropped
// at flatten time — a TOC line that cannot jump is a lie).
type tocRow struct {
	title string
	depth int
	spine int
}

// bookReader is the open book's full-screen state: the epub handle,
// the feeder, the flow engine, the flattened TOC with its cursor, and
// the persisted-anchor bookkeeping for the 章界+退出 write throttle.
type bookReader struct {
	row       store.BookRow
	bk        *epub.Book
	feeder    *epubFeeder
	flow      *bookFlow
	toc       []tocRow
	tocSel    int
	tocTop    int
	focusToc  bool
	bookmarks []store.Bookmark
	savedCh   int // anchor chapter at the last progress write

	// Bookmark modals: the Ctrl+B note dialog and the Ctrl+G list.
	bmNoteOpen bool
	bmNoteBuf  string
	bmListOpen bool
	bmSel      int

	// Search state (M6 ticket 04): the Ctrl+S input dialog, and the
	// scan session it starts (nil = no session). The session outlives
	// the dialog — Esc from a hit reopens the results (续选) until the
	// list itself is dismissed.
	srchOpen bool
	srchBuf  string
	srch     *bookSearch

	srchListOpen bool // the results overlay
	srchSel      int  // the results cursor (survives jump-and-reopen: 续选)

	seenDegrade int // feeder.degraded as of the last status report
}

// bookOpenMsg lands when the reader's open finished: the parsed book,
// the saved 续读位 and the bookmarks — or a typed failure.
type bookOpenMsg struct {
	row       store.BookRow
	bk        *epub.Book
	prog      store.Progress
	hasProg   bool
	bookmarks []store.Bookmark
	err       error
}

// openBookCmd is the reader's open half, off the UI goroutine: parse
// the book (zip central directory only — fast), then read the saved
// progress and bookmarks. The parse layer's two hard failures (DRM,
// corrupt zip) arrive typed for the status bar.
func (m Model) openBookCmd(row store.BookRow) tea.Cmd {
	if m.st == nil {
		return nil
	}
	st := m.st
	return func() tea.Msg {
		bk, err := epub.Open(row.Path)
		if err != nil {
			return bookOpenMsg{row: row, err: err}
		}
		prog, hasProg, err := st.GetProgress(context.Background(), row.ID)
		if err != nil {
			bk.Close()
			return bookOpenMsg{row: row, err: err}
		}
		bms, err := st.Bookmarks(context.Background(), row.ID)
		if err != nil {
			bk.Close()
			return bookOpenMsg{row: row, err: err}
		}
		return bookOpenMsg{row: row, bk: bk, prog: prog, hasProg: hasProg, bookmarks: bms}
	}
}

// bookLanded folds a finished open: failures keep the shelf with a
// typed status line; success builds the reader — the flow over the
// production feeder, the jump to the saved 续读位 (Enter 落到续读位,
// the shelf story 6), the flattened TOC with its cursor parked on the
// current chapter's nearest preceding entry.
func (m *Model) bookLanded(msg bookOpenMsg) tea.Cmd {
	m.bookOpening = false
	if msg.err != nil {
		switch {
		case errors.Is(msg.err, epub.ErrDRMUnsupported):
			m.status = m.cat.Get("books.err_drm")
		case errors.Is(msg.err, epub.ErrInvalidZip):
			m.status = m.cat.Get("books.err_zip")
		default:
			m.status = m.cat.Get("books.err_open", msg.err)
		}
		return nil
	}
	feeder := newEpubFeeder(msg.bk, m.cat)
	br := &bookReader{
		row: msg.row, bk: msg.bk, feeder: feeder,
		bookmarks: msg.bookmarks,
	}
	br.flow = newBookFlow(feeder, m.bookTextWidth(), m.readerHeight())
	if msg.hasProg {
		br.flow.jump(msg.prog.SpineIndex, msg.prog.BlockOffset)
	} else {
		br.flow.jump(0, 0)
	}
	br.savedCh, _ = br.flow.anchor()
	br.toc = flattenBookTOC(msg.bk)
	br.tocSel = max(br.tocHighlightIdx(), 0)
	m.book = br
	return nil
}

// closeBook is Esc: persist the final position, release the handle,
// and reload the shelf rows (the percent and order changed; the disk
// did not — no rescan).
func (m *Model) closeBook() tea.Cmd {
	br := m.book
	if br == nil {
		return nil
	}
	m.saveBookProgress()
	br.bk.Close()
	m.book = nil
	return m.reloadBooksCmd()
}

// saveBookProgress writes the 续读位 now (the throttle's write half;
// callers are the chapter boundary and the exit paths).
func (m *Model) saveBookProgress() {
	br := m.book
	if br == nil || m.st == nil {
		return
	}
	ch, block, pct := br.flow.progress()
	if err := m.st.SaveProgress(context.Background(), br.row.ID, ch, block, pct, time.Now()); err != nil {
		m.status = m.cat.Get("errors.db_write", err)
		return
	}
	br.savedCh = ch
}

// maybePersistBookChapter is the 章界 half of the 落盘节流: crossing
// into a new chapter writes; scrolling within a chapter does not.
func (m *Model) maybePersistBookChapter() {
	if m.book == nil {
		return
	}
	if ch, _, _ := m.book.flow.progress(); ch != m.book.savedCh {
		m.saveBookProgress()
	}
}

// flattenBookTOC walks the TOC depth-first into jump-capable rows
// (href → spine index; entries targeting non-spine files drop).
func flattenBookTOC(bk *epub.Book) []tocRow {
	hrefIdx := make(map[string]int, len(bk.Spine))
	for i, s := range bk.Spine {
		if _, ok := hrefIdx[s.Href]; !ok {
			hrefIdx[s.Href] = i
		}
	}
	var rows []tocRow
	var walk func(items []epub.TOCItem, depth int)
	walk = func(items []epub.TOCItem, depth int) {
		for _, it := range items {
			if idx, ok := hrefIdx[it.Href]; ok {
				rows = append(rows, tocRow{title: it.Title, depth: depth, spine: idx})
			}
			walk(it.Children, depth+1)
		}
	}
	walk(bk.TOC, 0)
	return rows
}

// tocHighlightIdx is the TOC row the current chapter highlights: the
// nearest preceding entry by spine (a chapter the TOC skips — the
// fixture's svg and corrupt chapters — reads under the previous
// entry's light). -1 when the TOC has nothing at or before it.
func (br *bookReader) tocHighlightIdx() int {
	cur, _ := br.flow.anchor()
	best, bestSpine := -1, -1
	for i, r := range br.toc {
		if r.spine <= cur && r.spine >= bestSpine {
			best, bestSpine = i, r.spine
		}
	}
	return best
}

// --- keys ----------------------------------------------------------------

// bookKeys is the reader's key pump. Consumed keys stop here; the
// shell-wide set (F1 help, Ctrl+T theme) falls through to the shared
// switch in Update. Tab never cycles sources behind the full screen
// — it toggles the TOC focus with ←/→.
func (m *Model) bookKeys(msg tea.KeyMsg) (bool, tea.Cmd) {
	br := m.book
	if br.srchOpen { // the search dialog is modal
		return m.bookSearchKeys(msg)
	}
	if br.srchListOpen { // the results list is modal
		return m.bookResultsKeys(msg)
	}
	if br.bmNoteOpen { // the note dialog is modal
		return m.bookNoteKeys(msg)
	}
	if br.bmListOpen { // the bookmark list is modal
		return m.bookListKeys(msg)
	}
	switch msg.String() {
	case "ctrl+b": // bookmark the current position (note optional)
		br.bmNoteOpen, br.bmNoteBuf = true, ""
		return true, nil
	case "ctrl+g": // the bookmark list
		br.bmListOpen = true
		br.bmSel = 0
		return true, nil
	case "esc": // layered: a live session reopens its results first
		if br.srch != nil {
			br.srchListOpen = true // 续选: back to the list at the cursor
			return true, nil
		}
		return true, m.closeBook()
	case "ctrl+q": // quit from inside the reader: persist, then quit
		m.saveBookProgress()
		br.bk.Close()
		m.book = nil
		return true, tea.Quit

	case "left":
		br.focusToc = true
		return true, nil
	case "right":
		br.focusToc = false
		return true, nil
	case "tab":
		br.focusToc = !br.focusToc // Tab toggles the focus
		return true, nil

	case "up":
		m.scrollBook(-1)
		return true, nil
	case "down":
		m.scrollBook(1)
		return true, nil
	case "pgup":
		m.scrollBook(-m.readerHeight())
		return true, nil
	case "pgdown":
		m.scrollBook(m.readerHeight())
		return true, nil

	case "enter": // TOC: jump; content: open the visible image
		if br.focusToc {
			if br.tocSel < len(br.toc) {
				br.flow.jump(br.toc[br.tocSel].spine, 0)
				m.maybePersistBookChapter()
				m.maybeBookChErrStatus()
			}
			return true, nil
		}
		m.openBookImage()
		return true, nil

	case "ctrl+o": // open the topmost visible image externally
		m.openBookImage()
		return true, nil

	case "ctrl+n": // next chapter head
		if ch, _ := br.flow.anchor(); ch+1 < br.flow.feed.Len() {
			br.flow.jump(ch+1, 0)
			m.maybePersistBookChapter()
			m.maybeBookChErrStatus()
		} else {
			m.status = m.cat.Get("reader.no_more")
		}
		return true, nil

	case "home": // book head
		br.flow.jump(0, 0)
		m.maybePersistBookChapter()
		m.maybeBookChErrStatus()
		return true, nil
	case "end": // book tail (block clamps to the last)
		br.flow.jump(br.flow.feed.Len()-1, 1<<30)
		m.maybePersistBookChapter()
		m.maybeBookChErrStatus()
		return true, nil

	case "ctrl+s", "ctrl+f": // book search (the alias rides the same seam)
		m.startSearchInput(br) // a fresh search cancels any running scan
		return true, nil

	case "+":
		m.adjustBookWidth(1)
		return true, nil
	case "-":
		m.adjustBookWidth(-1)
		return true, nil
	case "ctrl+_": // the terminal byte for ctrl+- (0x1F)
		m.adjustBookWidth(-1)
		return true, nil
	}
	return false, nil
}

// --- view ----------------------------------------------------------------

// readerHeight is the content height: the terminal minus the reader's
// own status line.
func (m Model) readerHeight() int {
	h := m.height - 1
	if h < 1 {
		h = 1
	}
	return h
}

// bookPaneWidth is the content pane's width: the terminal minus the
// TOC sidebar box (readerTocW + its left border), never below 1.
func (m Model) bookPaneWidth() int {
	return max(m.width-readerTocW-1, 1)
}

// bookTextWidth is the reading column: config text_width when set
// (capped by the pane), else the pane itself (text_width=0 随终端).
func (m Model) bookTextWidth() int {
	if m.cfg != nil && m.cfg.TextWidth > 0 {
		return min(m.cfg.TextWidth, m.bookPaneWidth())
	}
	return m.bookPaneWidth()
}

// bookView is the independent full screen: TOC sidebar left, content
// viewport right, the reader's own status line below.
func (m Model) bookView() string {
	if m.book.srchOpen { // the search dialog is its own screen
		return m.bookSearchView()
	}
	if m.book.bmNoteOpen { // the note dialog is its own screen
		return m.bookBmView()
	}
	h := m.readerHeight()
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		m.bookTocView(h),
		m.bookContentView(h),
	)
	view := lipgloss.JoinVertical(lipgloss.Left, body, m.bookStatusView())
	if m.book.srchListOpen { // the results overlay the reader
		return m.bookResultsView()
	}
	if m.book.bmListOpen { // the list overlays the reader
		return m.bookBmListView()
	}
	return view
}

// bookTocView renders the TOC sidebar: depth-indented entries with
// the current chapter's row marked ▸ (nearest preceding — a chapter
// the TOC skips reads under the previous entry's light), the cursor
// row highlighted while the TOC holds the focus, windowed like the
// shell's lists.
func (m Model) bookTocView(h int) string {
	br := m.book
	var b strings.Builder
	if len(br.toc) == 0 {
		b.WriteString(m.th.Dim.Render(m.cat.Get("reader.no_toc")))
	} else {
		hl := br.tocHighlightIdx()
		br.tocSel, br.tocTop = clampSel(br.tocSel, br.tocTop, len(br.toc), h)
		end := min(br.tocTop+h, len(br.toc))
		for i := br.tocTop; i < end; i++ {
			r := br.toc[i]
			marker := "  "
			if i == hl {
				marker = "▸ "
			}
			label := strings.Repeat(" ", 2*r.depth) + marker + r.title
			label = runewidth.FillRight(runewidth.Truncate(label, readerTocW-2, "…"), readerTocW-2)
			if br.focusToc && i == br.tocSel {
				b.WriteString(m.th.SidebarSelected.Render(label))
			} else {
				b.WriteString(m.th.SidebarItem.Render(label))
			}
			if i < end-1 {
				b.WriteString("\n")
			}
		}
	}
	return m.th.Sidebar.Width(readerTocW).Height(h).Render(b.String())
}

// bookContentView renders the flow's viewport into the content pane:
// ANSI-styled lines at the reading width, centered when the column is
// narrower than the pane.
func (m Model) bookContentView(h int) string {
	lines, _ := m.book.flow.view()
	text := render.ToANSI(lines, m.th.StyleFor)
	w := m.bookTextWidth()
	block := lipgloss.NewStyle().Width(w).Height(h).Render(text)
	return lipgloss.Place(m.bookPaneWidth(), h, lipgloss.Center, lipgloss.Top, block)
}

// bookStatusLine is the reader status line's left half: 书名 ·
// 第 x/y 章 · pct — the same percent figure the shelf column shows.
func (m Model) bookStatusLine() string {
	br := m.book
	ch, _, pct := br.flow.progress()
	return br.row.Title + " · " + m.cat.Get("reader.pos",
		ch+1, br.flow.feed.Len(), int(math.Round(pct*100)))
}

// bookStatusView is the reader's status line: the position triple
// left, the exit hint right.
// bookStatusView is the reader's status line: the position triple
// left, the exit hint right. The gap budget subtracts the bar's own
// Padding(0,1) — two columns — or the line overflows Width, wraps at
// a space in the right half, and MaxHeight(1) eats the tail.
func (m Model) bookStatusView() string {
	left := m.th.StatusNormal.Render(m.bookStatusLine())
	hint := m.cat.Get("reader.hint")
	if m.book.srch != nil {
		hint = m.searchStateLine() // the live scan state replaces the static hint
	}
	right := m.th.StatusHint.Render(hint)
	gap := m.width - 2 - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return m.th.StatusBar.Width(m.width).MaxHeight(1).
		Render(left + strings.Repeat(" ", gap) + right)
}

// maybeBookChErrStatus surfaces a fresh feeder degradation in the
// status bar (书文件被移走/损坏 → 明确状态栏错误): the placeholder
// paragraph keeps the flow readable, this line says what went wrong.
// A textless chapter (lastErr nil) reports the no-text line.
func (m *Model) maybeBookChErrStatus() {
	br := m.book
	if br == nil || br.feeder.degraded == br.seenDegrade {
		return
	}
	br.seenDegrade = br.feeder.degraded
	if err := br.feeder.lastErr; err != nil {
		m.status = m.cat.Get("reader.ch_err", err)
	} else {
		m.status = m.cat.Get("reader.no_text")
	}
}

// scrollBook is the arrows' one path: the TOC cursor moves while the
// TOC holds the focus, the flow scrolls otherwise (with the 章界
// persist check and the mid-session read-failure status after).
func (m *Model) scrollBook(delta int) {
	br := m.book
	if br.focusToc {
		br.tocSel, br.tocTop = clampSel(br.tocSel+delta, br.tocTop, len(br.toc), m.readerHeight())
		return
	}
	if delta < 0 {
		br.flow.up(-delta)
	} else {
		br.flow.down(delta)
	}
	m.maybePersistBookChapter()
	m.maybeBookChErrStatus()
}

// bookWidthStep is the +/- increment (the approved key package).
const bookWidthStep = 2

// bookWidthFloor is the narrowest reading column (the approved
// package's floor).
const bookWidthFloor = 20

// adjustBookWidth steps the reading column and re-wraps in place: the
// anchor block stays on top (锚点不丢), text_width persists (leaving the
// follow-terminal mode behind on the first press), the status line
// reports the new width. IME note: +/- are the ticket's one printable
// exception; ctrl+_ (what a terminal sends for ctrl+-) is the alias —
// ctrl+= has no control byte and cannot be a binding.
func (m *Model) adjustBookWidth(delta int) {
	br := m.book
	if br == nil || m.cfg == nil {
		return
	}
	w := m.bookTextWidth() + delta*bookWidthStep
	w = max(w, bookWidthFloor)
	w = min(w, m.bookPaneWidth())
	m.cfg.TextWidth = w
	if err := m.cfg.Save(); err != nil {
		m.status = m.cat.Get("errors.config_save", err)
		return
	}
	if w != br.flow.width {
		br.flow.relayout(w)
	}
	m.status = m.cat.Get("reader.width", w)
}

// --- bookmarks (Ctrl+B add, Ctrl+G list) ---------------------------------

// bmRow is one rendered bookmark-list row: the target's chapter title
// (resolved once when the list opens — feeder.title reads lazily),
// the note, and the jump anchor.
type bmRow struct {
	title string
	note  string
	ch    int
	block int
}

// bmRows builds the list's rows from the store's bookmarks (newest
// first — the store's order).
func (m Model) bmRows() []bmRow {
	br := m.book
	rows := make([]bmRow, 0, len(br.bookmarks))
	for _, b := range br.bookmarks {
		rows = append(rows, bmRow{
			title: br.feeder.title(b.SpineIndex),
			note:  b.Note,
			ch:    b.SpineIndex, block: b.BlockOffset,
		})
	}
	return rows
}

// bookNoteKeys is the note dialog's modal pump: runes accumulate
// (typed or pasted), Enter saves at the current anchor (an empty note
// is a valid bookmark), Esc cancels, backspace edits.
func (m *Model) bookNoteKeys(msg tea.KeyMsg) (bool, tea.Cmd) {
	br := m.book
	switch msg.String() {
	case "enter":
		br.bmNoteOpen = false
		ch, block := br.flow.anchor()
		if err := m.st.AddBookmark(context.Background(), br.row.ID, ch, block, br.bmNoteBuf, time.Now()); err != nil {
			m.status = m.cat.Get("errors.db_write", err)
			return true, nil
		}
		br.bmNoteBuf = ""
		if bms, err := m.st.Bookmarks(context.Background(), br.row.ID); err == nil {
			br.bookmarks = bms
		}
		m.status = m.cat.Get("reader.bm_saved", ch+1, br.flow.feed.Len())
		return true, nil
	case "esc":
		br.bmNoteOpen, br.bmNoteBuf = false, ""
		return true, nil
	case "backspace":
		if rs := []rune(br.bmNoteBuf); len(rs) > 0 {
			br.bmNoteBuf = string(rs[:len(rs)-1])
		}
		return true, nil
	}
	if msg.Type == tea.KeyRunes && !msg.Paste {
		br.bmNoteBuf += string(msg.Runes)
		return true, nil
	}
	return true, nil // modal: everything else waits
}

// bookListKeys is the bookmark list's modal pump: arrows walk, Enter
// jumps to the row's block, Ctrl+D deletes, Esc closes.
func (m *Model) bookListKeys(msg tea.KeyMsg) (bool, tea.Cmd) {
	br := m.book
	switch msg.String() {
	case "esc":
		br.bmListOpen = false
		return true, nil
	case "up":
		if br.bmSel > 0 {
			br.bmSel--
		}
		return true, nil
	case "down":
		if br.bmSel < len(br.bookmarks)-1 {
			br.bmSel++
		}
		return true, nil
	case "ctrl+d":
		if len(br.bookmarks) == 0 {
			return true, nil
		}
		b := br.bookmarks[br.bmSel]
		if err := m.st.DeleteBookmark(context.Background(), br.row.ID, b.SpineIndex, b.BlockOffset); err != nil {
			m.status = m.cat.Get("errors.db_write", err)
			return true, nil
		}
		m.status = m.cat.Get("reader.bm_deleted")
		if bms, err := m.st.Bookmarks(context.Background(), br.row.ID); err == nil {
			br.bookmarks = bms
		}
		br.bmSel = min(br.bmSel, max(len(br.bookmarks)-1, 0))
		return true, nil
	case "enter":
		if len(br.bookmarks) == 0 {
			return true, nil
		}
		b := br.bookmarks[br.bmSel]
		br.flow.jump(b.SpineIndex, b.BlockOffset)
		m.maybePersistBookChapter()
		br.bmListOpen = false
		return true, nil
	}
	return true, nil // modal: everything else waits
}

// bookBmView renders the note dialog (centered, the buffer shown in
// plain text — a note is meant to be read back).
func (m Model) bookBmView() string {
	br := m.book
	buf := br.bmNoteBuf
	if buf == "" {
		buf = " "
	}
	inner := max(m.width-6, 10) // border + padding of the overlay box
	box := m.th.HelpOverlay.Render(
		m.th.HelpTitle.Render(runewidth.Truncate(
			m.cat.Get("reader.bm_note_title"), inner, "…")) + "\n\n" +
			m.th.Title.Render(runewidth.Truncate(buf, inner, "…")) + "\n\n" +
			m.th.Dim.Render(runewidth.Truncate(
				m.cat.Get("reader.bm_note_hint"), inner, "…")))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// bookBmListView renders the bookmark list over the reader: chapter
// position + title + note per row, the selected row highlighted, the
// empty state when the book has none.
func (m Model) bookBmListView() string {
	br := m.book
	budget := max(m.width-6, 10) // the overlay box's border + padding
	var rows []string
	if len(br.bookmarks) == 0 {
		rows = append(rows, m.th.Dim.Render(
			runewidth.Truncate(m.cat.Get("reader.bm_empty"), budget, "…")))
	} else {
		for i, b := range m.bmRows() {
			pos := m.cat.Get("reader.bm_row", b.ch+1, br.flow.feed.Len())
			titleW := min(24, max(budget-runewidth.StringWidth(pos)-4, 1))
			title := runewidth.Truncate(b.title, titleW, "…")
			row := "  " + m.th.HelpKey.Render(pos) + "  " + m.th.HelpDesc.Render(title)
			if b.note != "" {
				noteW := max(budget-4-runewidth.StringWidth(pos)-runewidth.StringWidth(title), 1)
				row += m.th.Dim.Render(" — " + runewidth.Truncate(b.note, min(30, noteW), "…"))
			}
			if i == br.bmSel {
				row = m.th.ListSelected.Render(row)
			}
			rows = append(rows, row)
		}
		rows = append(rows, "", m.th.Dim.Render(
			runewidth.Truncate(m.cat.Get("reader.bm_list_hint"), budget, "…")))
	}
	box := m.th.HelpOverlay.Render(
		m.th.HelpTitle.Render(runewidth.Truncate(
			m.cat.Get("reader.bm_title"), budget, "…")) + "\n\n" +
			strings.Join(rows, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// --- book search (Ctrl+S / Ctrl+F) --------------------------------------

// bookScanMsg is the stepper's heartbeat: a no-op message whose
// handling runs one bounded scan step on the UI goroutine (the scan
// reads the flow cache — UI state — so it must not run on a
// goroutine; see search.go).
type bookScanMsg struct{}

// bookScanCmd posts the next step.
func bookScanCmd() tea.Cmd {
	return func() tea.Msg { return bookScanMsg{} }
}

// bookScanStep is the heartbeat's handler: one budgeted step of the
// active scan, the composed state line into the status seam, and
// the next beat — or quiescence when the scan is done or gone.
func (m *Model) bookScanStep() tea.Cmd {
	br := m.book
	if br == nil || br.srch == nil || br.srch.done {
		return nil
	}
	br.srch.step(br.flow)
	m.status = m.searchStateLine()
	if br.srch.done {
		return nil
	}
	return bookScanCmd()
}

// searchStateLine is the session's composed state — progress while
// scanning, the hit count when done. The reader's status right half
// shows it in place of the static hint while a session lives; the
// status seam carries it (visible after the reader closes).
func (m Model) searchStateLine() string {
	s := m.book.srch
	if !s.done {
		return m.cat.Get("reader.search_progress", s.next, m.book.flow.feed.Len())
	}
	return m.cat.Get("reader.search_done", len(s.hits))
}

// bookSearchKeys is the input dialog's modal pump: runes accumulate,
// Enter starts the scan at the current session-or-nil (an empty
// query just closes), Esc cancels, backspace edits.
func (m *Model) bookSearchKeys(msg tea.KeyMsg) (bool, tea.Cmd) {
	br := m.book
	switch msg.String() {
	case "enter":
		br.srchOpen = false
		if br.srchBuf == "" {
			br.srchBuf = ""
			return true, nil
		}
		br.srch = newBookSearch(br.srchBuf)
		br.srchBuf = ""
		br.srchListOpen, br.srchSel = true, 0
		return true, bookScanCmd()
	case "esc":
		br.srchOpen, br.srchBuf = false, ""
		return true, nil
	case "backspace":
		if rs := []rune(br.srchBuf); len(rs) > 0 {
			br.srchBuf = string(rs[:len(rs)-1])
		}
		return true, nil
	}
	if msg.Type == tea.KeyRunes && !msg.Paste {
		br.srchBuf += string(msg.Runes)
		return true, nil
	}
	return true, nil // modal: everything else waits
}

// startSearchInput begins a fresh search: any running scan dies, the
// results close, the input dialog opens empty (the list's Ctrl+S and
// the reader's are one behavior — the shared seam the two pumps
// reach for).
func (m *Model) startSearchInput(br *bookReader) {
	br.srch = nil
	br.srchListOpen = false
	br.srchOpen, br.srchBuf = true, ""
}

// bookResultsKeys is the results list's modal pump: arrows walk,
// Enter jumps to the hit's block (the list closes, the session
// stays — Esc from the hit reopens at the cursor), Esc closes the
// list AND ends the session, Ctrl+S/F refine from right here.
func (m *Model) bookResultsKeys(msg tea.KeyMsg) (bool, tea.Cmd) {
	br := m.book
	switch msg.String() {
	case "esc":
		br.srchListOpen = false
		br.srch = nil // the session ends with the list
		return true, nil
	case "ctrl+s", "ctrl+f":
		m.startSearchInput(br)
		return true, nil
	case "up":
		if br.srchSel > 0 {
			br.srchSel--
		}
		return true, nil
	case "down":
		if br.srchSel < len(br.srch.hits)-1 {
			br.srchSel++
		}
		return true, nil
	case "enter":
		if len(br.srch.hits) == 0 {
			return true, nil
		}
		h := br.srch.hits[br.srchSel]
		br.flow.jump(h.chapter, h.block)
		m.maybePersistBookChapter()
		m.maybeBookChErrStatus()
		br.srchListOpen = false // the session survives: Esc reopens (续选)
		return true, nil
	}
	return true, nil // modal: everything else waits
}

// bookResultsView renders the results over the reader: one row per
// hit (chapter position + title + the excerpt), the scan's state in
// the footer while it runs (the overlay covers the status line),
// the empty state when done with nothing found.
func (m Model) bookResultsView() string {
	br := m.book
	budget := max(m.width-6, 10) // the overlay box's border + padding
	var rows []string
	switch {
	case len(br.srch.hits) == 0 && br.srch.done:
		rows = append(rows, m.th.Dim.Render(
			runewidth.Truncate(m.cat.Get("reader.search_none"), budget, "…")))
	default:
		n := br.flow.feed.Len()
		for i, h := range br.srch.hits {
			pos := m.cat.Get("reader.bm_row", h.chapter+1, n)
			row := m.searchRow(pos, br.feeder.title(h.chapter), h.excerpt, budget)
			if i == br.srchSel {
				row = m.th.ListSelected.Render(row)
			}
			rows = append(rows, row)
		}
	}
	footer := m.cat.Get("reader.search_hint")
	if !br.srch.done { // the scan's live state rides the footer
		footer = m.searchStateLine() + " · " + footer
	}
	box := m.th.HelpOverlay.Render(
		m.th.HelpTitle.Render(runewidth.Truncate(
			m.cat.Get("reader.search_results_title", br.srch.needle), budget, "…")) + "\n\n" +
			strings.Join(rows, "\n") + "\n\n" +
			m.th.Dim.Render(runewidth.Truncate(footer, budget, "…")))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// searchRow assembles one result row within budget, degrading
// gracefully: title caps at 16, the excerpt takes the rest; when the
// budget cannot even carry position + markers the row falls back to
// the excerpt alone (a hit's quote is the meat; the chapter number
// is the garnish).
func (m Model) searchRow(pos, title, excerpt string, budget int) string {
	headW := 2 + runewidth.StringWidth(pos) + 2
	if budget-headW < 6 {
		return "  " + m.th.Dim.Render(
			runewidth.Truncate(excerpt, max(budget-2, 1), "…"))
	}
	title = runewidth.Truncate(title, min(16, budget-headW-4), "…")
	exW := budget - headW - runewidth.StringWidth(title) - 3
	return "  " + m.th.HelpKey.Render(pos) + "  " +
		m.th.HelpDesc.Render(title) +
		m.th.Dim.Render(" — "+runewidth.Truncate(excerpt, max(exW, 1), "…"))
}

// bookSearchView renders the input dialog (the note dialog's shape).
func (m Model) bookSearchView() string {
	br := m.book
	buf := br.srchBuf
	if buf == "" {
		buf = " "
	}
	inner := max(m.width-6, 10) // border + padding of the overlay box
	box := m.th.HelpOverlay.Render(
		m.th.HelpTitle.Render(runewidth.Truncate(
			m.cat.Get("reader.search_title"), inner, "…")) + "\n\n" +
			m.th.Title.Render(runewidth.Truncate(buf, inner, "…")) + "\n\n" +
			m.th.Dim.Render(runewidth.Truncate(
				m.cat.Get("reader.search_input_hint"), inner, "…")))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// --- image external open (Enter on content / Ctrl+O) ---------------------

// visibleImage finds the topmost visible image marker: the first line
// in the viewport carrying a [图片 span with a link number, and the
// chapter that owns it (the link table is per-chapter). ok=false when
// the view holds no marker.
func (m Model) visibleImage() (chapter, link int, ok bool) {
	lines, owners := m.book.flow.view()
	for i, ln := range lines {
		for _, s := range ln.Spans {
			if s.Link > 0 && strings.HasPrefix(s.Text, "[图片") {
				return owners[i], s.Link, true
			}
		}
	}
	return 0, 0, false
}

// openBookImage opens the topmost visible image externally: an
// http(s) src goes straight to the browser, anything else resolves
// chapter-relatively (ReadItemIn — the seam slice 2 added), lands as
// a content-hash-named temp file (re-open reuses it) and opens with
// the system program. goread's degradation model: the terminal shows
// the marker, the real viewer shows the picture.
func (m *Model) openBookImage() {
	ch, link, ok := m.visibleImage()
	if !ok {
		m.status = m.cat.Get("reader.no_image")
		return
	}
	fc := m.book.flow.chapter(ch)
	if link > len(fc.doc.Links) {
		m.status = m.cat.Get("reader.no_image")
		return
	}
	href := fc.doc.Links[link-1]
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		m.status = m.cat.Get("reader.img_opening")
		openBrowserCmd(href)()
		return
	}
	data, err := m.book.bk.ReadItemIn(m.book.bk.Spine[ch].Href, href)
	if err != nil {
		m.status = m.cat.Get("reader.img_err", err)
		return
	}
	ext := path.Ext(strings.SplitN(href, "#", 2)[0])
	if ext == "" {
		ext = ".img"
	}
	sum := sha1.Sum([]byte(m.book.row.Path + "\x00" + href))
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("readscope-%x%s", sum[:6], ext))
	if fresh, err := os.ReadFile(tmp); err != nil || !bytes.Equal(fresh, data) {
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			m.status = m.cat.Get("reader.img_err", err)
			return
		}
	}
	if err := openBrowser(tmp); err != nil {
		m.status = m.cat.Get("errors.open_browser", err)
		return
	}
	m.status = m.cat.Get("reader.img_opened")
}

// readerHelpBindings is the reader's own key table (F1 while a book
// is open) — the shell's list is meaningless behind the full screen.
var readerHelpBindings = []binding{
	{"← → / Tab", "toc_focus"},
	{"↑ ↓ PgUp PgDn", "scroll"},
	{"Enter(目录)", "toc_jump"},
	{"Enter(正文) / Ctrl+O", "open_image"},
	{"Ctrl+N", "next_chapter"},
	{"Ctrl+B", "add_bookmark"},
	{"Ctrl+G", "bookmark_list"},
	{"+ / -", "text_width"},
	{"Home / End", "book_ends"},
	{"Ctrl+T", "theme"},
	{"Ctrl+Q", "quit"},
	{"Esc", "back_shelf"},
}

// readerHelpView renders the reader's F1 table (helpView's branch
// while a book is open).
func (m Model) readerHelpView() string {
	rows := make([]string, 0, len(readerHelpBindings))
	for _, b := range readerHelpBindings {
		rows = append(rows, "  "+m.th.HelpKey.Render(pad(b.key, 22))+
			m.th.HelpDesc.Render(m.cat.Get("help.keys."+b.desc)))
	}
	box := m.th.HelpOverlay.Render(
		m.th.HelpTitle.Render(m.cat.Get("help.title")) + "\n\n" +
			strings.Join(rows, "\n") + "\n\n" +
			m.th.Dim.Render(m.cat.Get("help.close")))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
