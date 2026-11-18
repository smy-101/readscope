// reader.go: the EPUB reader (M6 ticket 03) — an independent
// full-screen over the bookshelf (ADR #11): left TOC sidebar, right
// viewport over the continuous flow. This file holds the production
// feeder (spine chapters through the parse layer and the shared
// pipeline); the screen state, key pump and view land with it as the
// slices accrete.
package ui

import (
	"context"
	"errors"
	"math"
	"path"
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
// never an error (the engine's stitcher contract; the reader's status
// bar carries the failure text separately).
type epubFeeder struct {
	bk     *epub.Book
	cat    *i18n.Catalog
	titles map[int]string // spine idx → TOC title, first match wins
}

func newEpubFeeder(bk *epub.Book, cat *i18n.Catalog) *epubFeeder {
	f := &epubFeeder{bk: bk, cat: cat, titles: make(map[int]string, len(bk.TOC))}
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
		return f.placeholderDoc(f.cat.Get("reader.ch_err", err)), fileTitle(href)
	case len(doc.Blocks) == 0:
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
// current-chapter label rides it) — the same chain Chapter applies.
func (f *epubFeeder) title(idx int) string {
	if title := f.titles[idx]; title != "" {
		return title
	}
	href := f.bk.Spine[idx].Href
	if doc, err := f.chapterDoc(idx); err == nil {
		if title := firstHeadingTitle(doc); title != "" {
			return title
		}
	}
	return fileTitle(href)
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
	switch msg.String() {
	case "esc": // any focus: back to the shelf (progress persisted)
		return true, m.closeBook()
	case "ctrl+q": // quit from inside the reader: persist, then quit
		m.saveBookProgress()
		br.bk.Close()
		m.book = nil
		return true, tea.Quit

	case "left", "shift+tab":
		br.focusToc = true
		return true, nil
	case "right", "tab":
		if msg.String() == "tab" {
			br.focusToc = !br.focusToc // Tab toggles the focus
		} else {
			br.focusToc = false
		}
		return true, nil

	case "up":
		if br.focusToc {
			br.tocSel, br.tocTop = clampSel(br.tocSel-1, br.tocTop, len(br.toc), m.readerHeight())
			return true, nil
		}
		br.flow.up(1)
		m.maybePersistBookChapter()
		return true, nil
	case "down":
		if br.focusToc {
			br.tocSel, br.tocTop = clampSel(br.tocSel+1, br.tocTop, len(br.toc), m.readerHeight())
			return true, nil
		}
		br.flow.down(1)
		m.maybePersistBookChapter()
		return true, nil
	case "pgup":
		if br.focusToc {
			br.tocSel, br.tocTop = clampSel(br.tocSel-m.readerHeight(), br.tocTop, len(br.toc), m.readerHeight())
			return true, nil
		}
		br.flow.up(m.readerHeight())
		m.maybePersistBookChapter()
		return true, nil
	case "pgdown":
		if br.focusToc {
			br.tocSel, br.tocTop = clampSel(br.tocSel+m.readerHeight(), br.tocTop, len(br.toc), m.readerHeight())
			return true, nil
		}
		br.flow.down(m.readerHeight())
		m.maybePersistBookChapter()
		return true, nil

	case "enter": // TOC: jump to the chapter head
		if br.focusToc && br.tocSel < len(br.toc) {
			br.flow.jump(br.toc[br.tocSel].spine, 0)
			m.maybePersistBookChapter()
		}
		return true, nil

	case "ctrl+n": // next chapter head
		if ch, _ := br.flow.anchor(); ch+1 < br.flow.feed.Len() {
			br.flow.jump(ch+1, 0)
			m.maybePersistBookChapter()
		} else {
			m.status = m.cat.Get("reader.no_more")
		}
		return true, nil

	case "home": // book head
		br.flow.jump(0, 0)
		m.maybePersistBookChapter()
		return true, nil
	case "end": // book tail (block clamps to the last)
		br.flow.jump(br.flow.feed.Len()-1, 1<<30)
		m.maybePersistBookChapter()
		return true, nil

	case "ctrl+s", "ctrl+f": // book search arrives with ticket 04
		m.status = m.cat.Get("reader.search_pending")
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

// bookTextWidth is the reading column: config text_width when set
// (capped by the pane), else the pane itself (text_width=0 随终端).
func (m Model) bookTextWidth() int {
	pane := m.width - readerTocW
	if pane < 1 {
		pane = 1
	}
	if m.cfg != nil && m.cfg.TextWidth > 0 {
		return min(m.cfg.TextWidth, pane)
	}
	return pane
}

// bookView is the independent full screen: TOC sidebar left, content
// viewport right, the reader's own status line below.
func (m Model) bookView() string {
	h := m.readerHeight()
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		m.bookTocView(h),
		m.bookContentView(h),
	)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.bookStatusView())
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
	pane := m.width - readerTocW
	if pane < 1 {
		pane = 1
	}
	return lipgloss.Place(pane, h, lipgloss.Center, lipgloss.Top, block)
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
func (m Model) bookStatusView() string {
	left := m.th.StatusNormal.Render(m.bookStatusLine())
	right := m.th.StatusHint.Render(m.cat.Get("reader.hint"))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return m.th.StatusBar.Width(m.width).MaxHeight(1).
		Render(left + strings.Repeat(" ", gap) + right)
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
	pane := m.width - readerTocW
	if pane < 1 {
		pane = 1
	}
	w := m.bookTextWidth() + delta*bookWidthStep
	w = max(w, bookWidthFloor)
	w = min(w, pane)
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
