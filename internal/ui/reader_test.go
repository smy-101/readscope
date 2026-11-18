package ui

// The production feeder (M6 ticket 03): spine chapters rendered
// through the parse layer and the shared pipeline, with the ticket's
// title fallback chain (TOC match → in-chapter heading → filename)
// and the degradation contract — a textless chapter (SVG page) and an
// unreadable chapter (corrupt entry) still produce placeholder
// paragraphs, never a zero-height chapter. Pinned against the
// ReaderFlow fixture through the engine itself: the composite seam
// the UI rides.

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/smy-101/readscope/internal/epub"
	"github.com/smy-101/readscope/internal/epub/fixture"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/render"
	"github.com/smy-101/readscope/internal/store"
)

// readerFeeder opens the ReaderFlow fixture and wraps it in the
// production feeder.
func readerFeeder(t *testing.T) *epubFeeder {
	t.Helper()
	bk, err := epub.Open(fixture.Write(t, fixture.ReaderFlow()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { bk.Close() })
	cat, err := i18n.Load("zh-CN")
	if err != nil {
		t.Fatalf("i18n: %v", err)
	}
	return newEpubFeeder(bk, cat)
}

func TestEpubFeederTitleChain(t *testing.T) {
	f := readerFeeder(t)
	if f.Len() != 5 {
		t.Fatalf("spine len = %d, want 5", f.Len())
	}
	want := []string{"第一章 起航", "1.1 小节", "插图页", "坏章", "附录"}
	for i, title := range want {
		if got := f.title(i); got != title {
			t.Errorf("chapter %d title = %q, want %q (TOC → heading → filename chain)", i, got, title)
		}
	}
}

func TestEpubFeederDegradesTextlessAndBroken(t *testing.T) {
	f := readerFeeder(t)

	// ch2: SVG-only body — the pipeline drops svg, the feeder answers
	// with the no-text placeholder, never zero blocks.
	doc, _ := f.Chapter(2)
	if len(doc.Blocks) == 0 {
		t.Fatal("svg chapter must degrade to a placeholder, not zero blocks")
	}
	if got := render.Layout(doc, 40)[0].Plain(); got != "[无可渲染文本]" {
		t.Fatalf("svg placeholder = %q, want the zh no-text line", got)
	}

	// ch3: corrupt zip entry — the read error becomes the chapter's
	// placeholder text (the status bar reports it too, but the flow
	// must carry a line so paging continues).
	doc, _ = f.Chapter(3)
	if len(doc.Blocks) == 0 {
		t.Fatal("corrupt chapter must degrade to a placeholder, not zero blocks")
	}
	var text string
	for _, ln := range render.Layout(doc, 40) {
		text += ln.Plain()
	}
	if !strings.Contains(text, "章节读取失败") {
		t.Fatalf("corrupt placeholder must carry the failure: %q", text)
	}
}

// The composite seam: the engine over the production feeder on the
// fixture — boundary dedup (ch0's leading h1 == TOC title → dropped;
// ch1's differing h1 kept), the image block's link table, and the
// exact flow block counts the UI tests below navigate by.
func TestFlowOverReaderFixture(t *testing.T) {
	f := readerFeeder(t)
	flow := newBookFlow(f, 40, 8)
	flow.jump(0, 0)

	c0 := flow.chapter(0)
	if n := len(c0.doc.Blocks); n != 4 { // rule, title, para, para
		t.Fatalf("ch0 flow blocks = %d, want 4 (leading h1 deduped)", n)
	}
	if h, ok := c0.doc.Blocks[1].(render.Heading); !ok || headingPlainText(h) != "第一章 起航" {
		t.Fatalf("ch0 boundary title wrong: %+v", c0.doc.Blocks[1])
	}

	c1 := flow.chapter(1)
	if n := len(c1.doc.Blocks); n != 5 { // rule, toc title, kept h1, para, image
		t.Fatalf("ch1 flow blocks = %d, want 5 (differing h1 kept)", n)
	}
	if h, ok := c1.doc.Blocks[2].(render.Heading); !ok || headingPlainText(h) != "1.1 长段" {
		t.Fatalf("ch1 in-chapter heading must survive: %+v", c1.doc.Blocks[2])
	}
	if _, isImg := c1.doc.Blocks[4].(render.Image); !isImg {
		t.Fatalf("ch1 block 4 must be the image: %+v", c1.doc.Blocks[4])
	}
	// The image's link table rides the flow document — the external
	// opener resolves it chapter-relatively later.
	if len(c1.doc.Links) == 0 || c1.doc.Links[0] != "../media/fig.png" {
		t.Fatalf("ch1 links = %v, want the ../media/fig.png href", c1.doc.Links)
	}
}

// --- the UI pump (slice wiring) -----------------------------------------

// openReaderModel shelves the ReaderFlow fixture, scans, opens the
// book with Enter and pumps the open chain to quiescence: a model
// with the full-screen reader up, at the book head.
func openReaderModel(t *testing.T, lang string) (Model, store.Store) {
	t.Helper()
	m, dir := bookshelfModel(t, lang)
	shelveIn(t, dir, "flow.epub", fixture.ReaderFlow())
	m = size(m, 100, 30)
	m = pump(t, m, m.Init()) // scan
	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.book == nil {
		t.Fatalf("reader did not open:\n%s", m.View())
	}
	return m, *m.st
}

// Enter on the shelf opens the independent full screen (ADR #11): TOC
// sidebar with the fixture's TOC, content viewport with chapter 1's
// body, the shell's source bar gone, and the composed status line in
// both packs (CODING_STANDARDS).
func TestBookReaderOpensFullScreen(t *testing.T) {
	m, _ := openReaderModel(t, "en")
	view := m.View()
	for _, want := range []string{
		"第一章 起航", // TOC entry
		"1.1 小节", // nested TOC entry
		"附录",     // TOC entry
		"第一段正文。", // chapter 1 content
	} {
		if !strings.Contains(view, want) {
			t.Errorf("reader view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "All books") {
		t.Error("the shell's sidebar must be gone in the full screen")
	}
	// The composed status line, both packs: title · 第 x/y 章 · pct.
	if got, want := m.bookStatusLine(), "流样本书 · ch. 1/5 · 0%"; got != want {
		t.Errorf("en status = %q, want %q", got, want)
	}
	mzh, _ := openReaderModel(t, "zh-CN")
	if got, want := mzh.bookStatusLine(), "流样本书 · 第 1/5 章 · 0%"; got != want {
		t.Errorf("zh status = %q, want %q", got, want)
	}
}

// The 续读位 contract at the UI seam: a saved progress row drops the
// re-open at its anchor; scrolling across a chapter boundary persists
// immediately (章界落盘); Esc writes the final position and returns
// to the shelf, whose rows reload with the new percent.
func TestBookReaderResumeAndPersist(t *testing.T) {
	m, dir := bookshelfModel(t, "en")
	shelveIn(t, dir, "flow.epub", fixture.ReaderFlow())
	m = size(m, 100, 30)
	m = pump(t, m, m.Init()) // scan
	ctx := t.Context()
	rows, err := m.st.BookRows(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("shelf rows = %v err=%v", rows, err)
	}
	// Seed a mid-book position: chapter 2 (spine 1), block 3. The
	// first open lands exactly there (Enter 落到续读位).
	if err := m.st.SaveProgress(ctx, rows[0].ID, 1, 3, 0.3, time.Now()); err != nil {
		t.Fatalf("seed progress: %v", err)
	}
	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.book == nil || m.book.flow.topCh != 1 {
		t.Fatalf("resume did not land on spine 1")
	}
	if m.book.savedCh != 1 {
		t.Fatalf("savedCh = %d, want 1 (throttle starts at the resume chapter)", m.book.savedCh)
	}

	// Scroll to the next chapter boundary: the write fires at the
	// boundary, not on Esc (章界+退出 throttle).
	m = key(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	p, ok, err := m.st.GetProgress(ctx, m.book.row.ID)
	if err != nil || !ok {
		t.Fatalf("boundary persist missing: ok=%v err=%v", ok, err)
	}
	if p.SpineIndex < 2 {
		t.Fatalf("boundary persist still on spine %d, want ≥2", p.SpineIndex)
	}

	// Esc: final write, back on the shelf, rows reloaded with pct.
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = pump(t, m, cmd)
	if m.book != nil {
		t.Fatal("Esc must close the reader")
	}
	view := m.View()
	if !strings.Contains(view, "All books") {
		t.Fatalf("Esc must return to the shelf:\n%s", view)
	}
	if !strings.Contains(view, "%") {
		t.Fatalf("shelf rows must show the new percent:\n%s", view)
	}
	p, ok, err = m.st.GetProgress(ctx, m.books[0].ID)
	if err != nil || !ok || p.Pct == 0 {
		t.Fatalf("final persist lost: ok=%v pct=%v err=%v", ok, p.Pct, err)
	}
	// The rows-only reload keeps the failure map: the corrupt-entry
	// book (if any) keeps its ⚠ verdicts without a rescan.
	if m.failErr == nil && len(m.broken) > 0 {
		t.Fatal("reload must not resurrect rows without verdicts")
	}
}

// A broken file at open time explains itself in the status bar and
// never enters the reader.
func TestBookReaderOpenFailureTyped(t *testing.T) {
	m, dir := bookshelfModel(t, "en")
	shelveIn(t, dir, "drm.epub", fixture.DRMEncrypted())
	m = size(m, 100, 30)
	m = pump(t, m, m.Init())
	m.booksSel = 0
	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.book != nil {
		t.Fatal("a DRM book must not open the reader")
	}
	if !strings.Contains(m.status, "DRM") {
		t.Fatalf("status = %q, want the typed DRM line", m.status)
	}
}

// The reader's stitched lines obey the widthguard rule: every line
// the full screen assembles fits the reading column (the detail
// layers' standing invariant, now covering the reader).
func TestBookReaderLinesFit(t *testing.T) {
	m, _ := openReaderModel(t, "en")
	lines, _ := m.book.flow.view()
	assertLinesFit(t, lines, m.bookTextWidth())
	// After paging into later chapters too — the stitch keeps fitting
	// across boundaries, placeholders included.
	m.book.flow.down(60)
	m.book.flow.view()
	lines, _ = m.book.flow.view()
	assertLinesFit(t, lines, m.bookTextWidth())
}

// The reader's key pump: ←/→/Tab focus the TOC, the focused TOC's
// arrows walk its cursor and Enter jumps to the chapter head, Ctrl+N
// hops chapter heads with an end hint, Home/End hit the book's ends.
func TestBookReaderTocNavigationAndJumps(t *testing.T) {
	m, _ := openReaderModel(t, "en")

	// ← focuses the TOC; ↓ walks to 1.1 小节 (spine 1); Enter jumps.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if !m.book.focusToc {
		t.Fatal("← must focus the TOC")
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.book.tocSel != 1 {
		t.Fatalf("tocSel = %d, want 1 (1.1 小节)", m.book.tocSel)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if ch, _ := m.book.flow.anchor(); ch != 1 {
		t.Fatalf("TOC Enter must jump to spine 1, got %d", ch)
	}
	if !strings.Contains(m.View(), "1.1 长段") {
		t.Fatalf("the view must show chapter 2 after the jump:\n%s", m.View())
	}
	// The jump crossed a chapter boundary: persisted immediately.
	if p, ok, _ := m.st.GetProgress(t.Context(), m.book.row.ID); !ok || p.SpineIndex != 1 {
		t.Fatalf("jump persist = ok:%v spine:%d, want spine 1", ok, p.SpineIndex)
	}

	// → returns the focus to the content; ↓ scrolls the flow.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.book.focusToc {
		t.Fatal("→ must return the focus to the content")
	}
	before := m.book.flow.topLine
	m = key(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.book.flow.topLine == before {
		t.Fatal("↓ in content must scroll")
	}

	// Ctrl+N hops chapter heads; at the last chapter it says so.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN}) // 1 → 2
	if ch, _ := m.book.flow.anchor(); ch != 2 {
		t.Fatalf("Ctrl+N = spine %d, want 2", ch)
	}
	for i := 0; i < 5; i++ {
		m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN})
	}
	if !strings.Contains(m.status, "last chapter") {
		t.Fatalf("Ctrl+N at the end = %q, want the end hint", m.status)
	}

	// Home = book head; End = book tail (the tail view reaches the
	// final line: pct reads 100).
	m = key(t, m, tea.KeyMsg{Type: tea.KeyHome})
	if ch, _ := m.book.flow.anchor(); ch != 0 || m.book.flow.topLine != 0 {
		t.Fatalf("Home = (%d,%d), want (0,0)", ch, m.book.flow.topLine)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	if got, want := m.bookStatusLine(), "流样本书 · ch. 5/5 · 100%"; got != want {
		t.Fatalf("End status = %q, want %q", got, want)
	}
}

// Tab in the reader toggles the TOC focus (it never cycles sources
// behind the full screen); the search keys are swallowed with the
// reserved line — both packs (CODING_STANDARDS composed pairs).
func TestBookReaderTabAndSearchReserved(t *testing.T) {
	for _, tc := range []struct{ lang, want string }{
		{"en", "book search arrives with the next ticket"},
		{"zh-CN", "书内搜索随下一票开通"},
	} {
		m, _ := openReaderModel(t, tc.lang)
		m = key(t, m, tea.KeyMsg{Type: tea.KeyTab})
		if m.active != 0 || !m.book.focusToc {
			t.Fatalf("%s: Tab must toggle TOC focus, not cycle sources (active=%d)", tc.lang, m.active)
		}
		m = key(t, m, tea.KeyMsg{Type: tea.KeyTab})
		if m.book.focusToc {
			t.Fatalf("%s: Tab must toggle back to the content", tc.lang)
		}
		m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlS})
		if !strings.Contains(m.status, tc.want) {
			t.Fatalf("%s: Ctrl+S status = %q, want %q", tc.lang, m.status, tc.want)
		}
		m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlF})
		if !strings.Contains(m.status, tc.want) {
			t.Fatalf("%s: Ctrl+F status = %q, want %q", tc.lang, m.status, tc.want)
		}
	}
}

// The width keys: +/- (and the ctrl+=/ctrl+- aliases, IME-safe) step
// the reading column by 2, floor 20, re-wrap immediately with the
// anchor block kept on top (锚点不丢), and persist text_width — leaving
// the follow-terminal mode behind on first press.
func TestBookReaderWidthAdjust(t *testing.T) {
	m, _ := openReaderModel(t, "en")
	// Park mid-book so the anchor is non-trivial, then read it.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN})
	ch, block := m.book.flow.anchor()

	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	if w := m.bookTextWidth(); w != 73 { // 100 - 25 (sidebar+border) = 75, minus the step 2
		t.Fatalf("after '-': width = %d, want 73", w)
	}
	if m.book.flow.width != 73 {
		t.Fatalf("flow re-wrapped at %d, want 73", m.book.flow.width)
	}
	if c2, b2 := m.book.flow.anchor(); c2 != ch || b2 != block {
		t.Fatalf("width change lost the anchor: (%d,%d) → (%d,%d)", ch, block, c2, b2)
	}
	if m.cfg.TextWidth != 73 {
		t.Fatalf("text_width not persisted: %d", m.cfg.TextWidth)
	}
	if !strings.Contains(m.status, "width 73") {
		t.Fatalf("status = %q, want the width line", m.status)
	}

	// The ctrl+_ alias (what terminals send for ctrl+-, 0x1F) steps
	// down; the zh pack composes its own line.
	mzh, _ := openReaderModel(t, "zh-CN")
	mzh = key(t, mzh, tea.KeyMsg{Type: tea.KeyCtrlUnderscore})
	if w := mzh.bookTextWidth(); w != 75-2 {
		t.Fatalf("ctrl+_ width = %d, want 73", w)
	}
	if !strings.Contains(mzh.status, "宽度 73") {
		t.Fatalf("zh status = %q, want 宽度 73", mzh.status)
	}

	// The floor: hammering '-' stops at 20.
	for i := 0; i < 60; i++ {
		m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	}
	if w := m.bookTextWidth(); w != 20 {
		t.Fatalf("floor = %d, want 20", w)
	}
	// '+' grows back, capped by the pane.
	for i := 0; i < 80; i++ {
		m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	}
	if w := m.bookTextWidth(); w != 75 {
		t.Fatalf("cap = %d, want the pane width 75", w)
	}
}

// The bookmark flow: Ctrl+B opens the note dialog (empty note is a
// valid bookmark), Enter saves at the current anchor, Ctrl+G lists
// them (Enter jumps to the block, Ctrl+D deletes), Esc cancels the
// dialog without writing.
func TestBookBookmarkAddJumpDelete(t *testing.T) {
	m, _ := openReaderModel(t, "en")
	ctx := t.Context()
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN}) // park on spine 1
	for i := 0; i < 4; i++ {                      // walk onto a content block (block 2)
		m = key(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}

	// Ctrl+B → note dialog; typing accumulates; Enter saves.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlB})
	if !m.book.bmNoteOpen {
		t.Fatal("Ctrl+B must open the note dialog")
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("关键段")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	bms, err := m.st.Bookmarks(ctx, m.book.row.ID)
	if err != nil || len(bms) != 1 {
		t.Fatalf("bookmarks after save = %v err=%v", bms, err)
	}
	if bms[0].SpineIndex != 1 || bms[0].BlockOffset != 2 || bms[0].Note != "关键段" {
		t.Fatalf("saved bookmark = %+v, want spine 1 block 2 note 关键段", bms[0])
	}
	if !strings.Contains(m.status, "bookmark added") {
		t.Fatalf("status = %q, want the composed saved line", m.status)
	}

	// A second bookmark elsewhere; the zh pack composes its own line.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN}) // spine 2
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlB})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // empty note is valid
	bms, _ = m.st.Bookmarks(ctx, m.book.row.ID)
	if len(bms) != 2 || bms[0].SpineIndex != 2 || bms[0].Note != "" {
		t.Fatalf("two bookmarks, newest first: %+v", bms)
	}

	// Ctrl+G lists them; Enter jumps to the selected one's block.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlG})
	if !m.book.bmListOpen {
		t.Fatal("Ctrl+G must open the bookmark list")
	}
	view := m.View()
	for _, want := range []string{"关键段", "1.1 小节"} { // note + chapter title
		if !strings.Contains(view, want) {
			t.Errorf("bookmark list missing %q:\n%s", want, view)
		}
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyDown}) // to the older one (spine 1)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.book.bmListOpen {
		t.Fatal("Enter must close the list")
	}
	if ch, _ := m.book.flow.anchor(); ch != 1 {
		t.Fatalf("bookmark jump = spine %d, want 1", ch)
	}

	// Delete from the list: the row goes, the store follows, an empty
	// list shows the empty state and stays escapable.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlG})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})
	bms, _ = m.st.Bookmarks(ctx, m.book.row.ID)
	if len(bms) != 1 {
		t.Fatalf("after delete = %d bookmarks, want 1", len(bms))
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlD}) // delete the last one
	bms, _ = m.st.Bookmarks(ctx, m.book.row.ID)
	if len(bms) != 0 {
		t.Fatalf("after second delete = %d bookmarks, want 0", len(bms))
	}
	if !strings.Contains(m.View(), "no bookmarks yet") {
		t.Fatalf("empty list state missing:\n%s", m.View())
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.book == nil || m.book.bmListOpen {
		t.Fatal("Esc must close the list back to the reader")
	}
}

// Esc in the note dialog cancels: nothing written.
func TestBookBookmarkNoteCancel(t *testing.T) {
	m, _ := openReaderModel(t, "en")
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlB})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("草稿")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.book.bmNoteOpen {
		t.Fatal("Esc must close the note dialog")
	}
	bms, _ := m.st.Bookmarks(t.Context(), m.book.row.ID)
	if len(bms) != 0 {
		t.Fatalf("cancelled dialog wrote %d bookmarks", len(bms))
	}
}

// The image keys (Enter on content / Ctrl+O): the topmost visible
// [图片: alt] marker resolves chapter-relatively (ReadItemIn), lands
// as a temp file and opens with the system program; a view without a
// marker says so; an http(s) src goes straight to the browser.
func TestBookReaderOpenImage(t *testing.T) {
	m, _ := openReaderModel(t, "en")

	// Park on the fixture's image block (spine 1, last block).
	m.book.flow.jump(1, 4)
	var opened string
	orig := openBrowser
	openBrowser = func(p string) error { opened = p; return nil }
	t.Cleanup(func() { openBrowser = orig })

	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	if opened == "" {
		t.Fatalf("no image opened (status %q)", m.status)
	}
	if !strings.HasSuffix(opened, ".png") {
		t.Fatalf("temp file = %q, want a .png suffix", opened)
	}
	data, err := os.ReadFile(opened)
	t.Cleanup(func() { os.Remove(opened) })
	if err != nil || string(data) != "\x89PNG fake png bytes" {
		t.Fatalf("temp file bytes = %q err=%v, want the zip entry's", data, err)
	}
	if !strings.Contains(m.status, "opened") {
		t.Fatalf("status = %q, want the opened line", m.status)
	}

	// Enter on the content does the same job.
	opened = ""
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if opened == "" {
		t.Fatal("content Enter must open the visible image too")
	}

	// A view without any marker explains itself (a short viewport over
	// the book head — the stitching would otherwise reach the image).
	m = size(m, 100, 7)
	m.book.flow.jump(0, 0)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	if !strings.Contains(m.status, "no image") {
		t.Fatalf("status = %q, want the no-image line", m.status)
	}
}

// F1 inside the reader shows the reader's own key table (the shell
// list is meaningless behind the full screen).
func TestBookReaderHelp(t *testing.T) {
	m, _ := openReaderModel(t, "en")
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF1})
	view := m.View()
	for _, want := range []string{"Ctrl+B", "Ctrl+G", "Ctrl+N", "Esc"} {
		if !strings.Contains(view, want) {
			t.Errorf("reader help missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Ctrl+A") {
		t.Error("the shell's mark-all key has no meaning in the reader")
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.help {
		t.Fatal("Esc must close the help overlay")
	}
}

// A book file damaged mid-session (truncated on disk — an unlinked
// but open handle still reads on Unix, truncation does not): chapters
// past the cache render as the read-failure placeholder, nothing
// panics, and Esc still retreats whole (the ticket's 全身而退).
func TestBookReaderSurvivesVanishedFile(t *testing.T) {
	m, _ := openReaderModel(t, "en")
	if err := os.Truncate(m.book.row.Path, 0); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN}) // into an uncached chapter
	if !strings.Contains(m.View(), "chapter read failed") {
		t.Fatalf("the vanished file must surface as the read-failure placeholder:\n%s", m.View())
	}
	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = pump(t, m, cmd)
	if m.book != nil {
		t.Fatal("Esc must still exit the reader")
	}
}

// The review pair the standards axis asked for: the zh pack's
// composed bookmark-saved line (format verbs aboard — the m5 lesson).
func TestBookBookmarkSavedComposedZh(t *testing.T) {
	m, _ := openReaderModel(t, "zh-CN")
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlB})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.status, "书签已加（第 1/5 章）") {
		t.Fatalf("zh composed saved line = %q", m.status)
	}
}

// The mid-session read failure reaches the status bar too (书文件被
// 移走/损坏 → 明确状态栏错误), not only the in-flow placeholder.
func TestBookReaderVanishedFileStatusLine(t *testing.T) {
	m, _ := openReaderModel(t, "zh-CN")
	if err := os.Truncate(m.book.row.Path, 0); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN}) // into an uncached chapter
	if !strings.Contains(m.status, "章节读取失败") {
		t.Fatalf("status = %q, want the read-failure line", m.status)
	}
}

// The whole-screen width guard: every line the reader assembles —
// TOC sidebar rows, the status line, both modals — fits the terminal.
func TestBookReaderViewLinesFitTerminal(t *testing.T) {
	for _, sz := range [][2]int{{100, 30}, {60, 12}, {38, 8}} {
		m, _ := openReaderModel(t, "en")
		m = size(m, sz[0], sz[1])
		m.book.flow.jump(1, 4)
		for _, view := range []string{
			m.View(),
			func() string { m.book.bmListOpen = true; return m.View() }(),
			func() string { m.book.bmListOpen = false; m.book.bmNoteOpen = true; return m.View() }(),
		} {
			for i, ln := range strings.Split(view, "\n") {
				if w := lipgloss.Width(ln); w > sz[0] {
					t.Fatalf("%dx%d line %d is %d cols wide: %q", sz[0], sz[1], i, w, ln)
				}
			}
		}
	}
}
