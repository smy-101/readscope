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

	"github.com/smy-101/readscope/internal/epub"
	"github.com/smy-101/readscope/internal/epub/fixture"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/render"
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
