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
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
