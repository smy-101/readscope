// flow.go: the continuous-flow engine under the EPUB reader (M6
// ticket 03). GLOSSARY 连续流: the whole book is ONE content stream —
// chapters stitch into it lazily, the boundary is a rule plus the
// chapter title heading, and paging never pauses at chapter ends.
//
// The engine is deliberately Bubble-Tea-free and feeder-driven: a
// chapterFeeder supplies spine length and content documents on demand
// (the production feeder parses chapters through epub + render, tests
// use fakes with literal geometry), and everything here — the
// viewport window, the 续读位 anchor (spine, block), the LRU, the
// resize relayout — is pure state over that seam.

package ui

import (
	"github.com/smy-101/readscope/internal/render"
)

// flowCacheChapters is the rendered-chapter LRU cap (the ticket's
// memory decision). Chapters the current viewport spans are never
// evicted, so a pathological viewport (hundreds of one-line chapters)
// may hold more — memory then follows the viewport, still bounded by
// what one screen can stitch.
const flowCacheChapters = 16

// chapterFeeder is the engine's only dependency. The contract: Len is
// the spine length; Chapter always returns a content document with at
// least one block and the chapter's display title (TOC match →
// in-chapter heading → filename, the feeder's fallback chain) —
// reading failures and textless chapters (SVG pages) degrade to
// placeholder paragraphs on the feeder side, where the catalog lives.
// The engine adds its own zero-block guard as a breach net (a
// zero-height chapter would loop the stitcher).
type chapterFeeder interface {
	Len() int
	Chapter(idx int) (*render.Document, string)
}

// flowChapter is one chapter's rendered state at the current width:
// the flow document (boundary rule + title heading around the
// content, the content's link table intact), its lines, and each
// block's first line index — the anchor's coordinate system.
type flowChapter struct {
	doc    *render.Document
	lines  []render.Line
	starts []int // len == len(doc.Blocks)+1, sentinel at the end
}

// bookFlow is the continuous flow: a fixed-height viewport window
// over the book's stitched line stream. The position contract is the
// anchor (topCh, block of the top line) — width-independent by
// construction, so relayout re-wraps lines while the anchor block
// stays put on top.
type bookFlow struct {
	feed   chapterFeeder
	width  int
	height int

	cache map[int]*flowChapter
	lru   []int  // chapter indices, most recent first
	span  [2]int // chapters the last view spanned (never evicted)

	topCh   int
	topLine int
}

func newBookFlow(feed chapterFeeder, width, height int) *bookFlow {
	return &bookFlow{
		feed: feed, width: max(width, 1), height: max(height, 1),
		cache: map[int]*flowChapter{}, span: [2]int{-1, -1},
	}
}

// chapter returns the chapter's rendered state, rendering on demand
// (boundary wrap + layout) and touching the LRU. The zero-block guard
// keeps the stitcher's advance invariant even if a feeder misbehaves.
func (f *bookFlow) chapter(idx int) *flowChapter {
	if c, ok := f.cache[idx]; ok {
		f.touch(idx)
		return c
	}
	content, title := f.feed.Chapter(idx)
	if len(content.Blocks) == 0 {
		content = &render.Document{
			Blocks: []render.Block{render.Para{Spans: []render.Span{{Text: " ", Style: render.SpanDim}}}},
			Links:  content.Links,
		}
	}
	doc := wrapBoundary(title, content)
	lines, starts := render.LayoutIndexed(doc, f.width)
	c := &flowChapter{doc: doc, lines: lines, starts: starts}
	f.cache[idx] = c
	f.touch(idx)
	f.evict()
	return c
}

// wrapBoundary assembles a chapter's flow document: the boundary rule
// plus title heading (章界呈现 = 分隔线+标题), then the content — a
// leading content heading that repeats the title is dropped (the
// boundary already says it; a chapter whose TOC title and in-chapter
// heading differ keeps both).
func wrapBoundary(title string, content *render.Document) *render.Document {
	blocks := make([]render.Block, 0, len(content.Blocks)+2)
	blocks = append(blocks,
		render.Rule{},
		render.Heading{Level: 1, Spans: []render.Span{{Text: title, Style: render.SpanBold}}})
	rest := content.Blocks
	if len(rest) > 0 {
		// The pipeline squeezes CJK-interior spaces out of heading text;
		// the title keeps its source spelling — compare squeezed.
		if h, ok := rest[0].(render.Heading); ok && headingPlainText(h) == render.SqueezeCJK(title) {
			rest = rest[1:]
		}
	}
	blocks = append(blocks, rest...)
	return &render.Document{Blocks: blocks, Links: content.Links}
}

// headingPlainText flattens a heading's spans (wrapBoundary's dedup).
func headingPlainText(h render.Heading) string {
	var b []byte
	for _, s := range h.Spans {
		b = append(b, s.Text...)
	}
	return string(b)
}

// touch moves idx to the LRU front.
func (f *bookFlow) touch(idx int) {
	out := make([]int, 0, len(f.lru))
	out = append(out, idx)
	for _, i := range f.lru {
		if i != idx {
			out = append(out, i)
		}
	}
	f.lru = out
}

// evict trims the cache to the cap, dropping the least recently used
// chapter that the current viewport does not span (the top chapter
// counts as spanned even before a view runs); if every entry is in
// span the trim stands down (see flowCacheChapters).
func (f *bookFlow) evict() {
	for len(f.cache) > flowCacheChapters {
		dropped := false
		for i := len(f.lru) - 1; i >= 0; i-- {
			idx := f.lru[i]
			if idx == f.topCh || (idx >= f.span[0] && idx <= f.span[1]) {
				continue
			}
			delete(f.cache, idx)
			f.lru = append(f.lru[:i], f.lru[i+1:]...)
			dropped = true
			break
		}
		if !dropped {
			return
		}
	}
}

// view stitches the viewport: height lines from the top position
// across chapter boundaries (each touched chapter renders on demand),
// blank-padded past the book's end. owners[i] names the chapter line
// i belongs to (-1 for padding) — the caller's key to a visible
// line's origin (the image-opening key needs the owning chapter).
func (f *bookFlow) view() ([]render.Line, []int) {
	lines := make([]render.Line, 0, f.height)
	owners := make([]int, 0, f.height)
	ch, ln := f.topCh, f.topLine
	f.span = [2]int{f.topCh, f.topCh}
	for len(lines) < f.height && ch < f.feed.Len() {
		f.span[1] = ch // before the render: the chapter being consumed is in-span
		c := f.chapter(ch)
		for ; ln < len(c.lines) && len(lines) < f.height; ln++ {
			lines = append(lines, c.lines[ln])
			owners = append(owners, ch)
		}
		ln = 0
		ch++
	}
	for len(lines) < f.height {
		lines = append(lines, render.Line{})
		owners = append(owners, -1)
	}
	return lines, owners
}

// down advances the top by n lines, crossing chapter boundaries
// forward. Inside the final chapter the top clamps so the page ends
// at the book's end (no scrolling past the last line). It reports
// whether the position moved.
func (f *bookFlow) down(n int) bool {
	moved := false
	for n > 0 {
		c := f.chapter(f.topCh)
		maxLine := len(c.lines) - 1
		if f.topCh == f.feed.Len()-1 {
			maxLine = max(0, len(c.lines)-f.height)
		}
		if f.topLine < maxLine {
			step := min(n, maxLine-f.topLine)
			f.topLine += step
			n -= step
			moved = true
			continue
		}
		if f.topCh == f.feed.Len()-1 {
			return moved
		}
		f.topCh++
		f.topLine = 0
		n--
		moved = true
	}
	return moved
}

// up retreats the top by n lines, crossing boundaries backwards onto
// the previous chapter's last line (the page then shows its tail and
// the current chapter's head — the seamless-flow contract). It
// reports whether the position moved.
func (f *bookFlow) up(n int) bool {
	moved := false
	for n > 0 {
		if f.topLine > 0 {
			step := min(n, f.topLine)
			f.topLine -= step
			n -= step
			moved = true
			continue
		}
		if f.topCh == 0 {
			return moved
		}
		f.topCh--
		c := f.chapter(f.topCh)
		f.topLine = len(c.lines) - 1
		n--
		moved = true
	}
	return moved
}

// anchor is the 续读位: the chapter of the top line and the block
// that contains it. A block separator line belongs to the block it
// follows (starts[b] is block b's first line, separators precede).
func (f *bookFlow) anchor() (chapter, block int) {
	c := f.chapter(f.topCh)
	b := 0
	for i := 1; i < len(c.starts)-1; i++ {
		if c.starts[i] <= f.topLine {
			b = i
		} else {
			break
		}
	}
	return f.topCh, b
}

// jump sets the top to a chapter/block anchor (clamped in range). In
// the final chapter the top additionally honors the end clamp: a
// short tail chapter shows whole, from its head.
func (f *bookFlow) jump(chapter, block int) {
	if chapter < 0 {
		chapter = 0
	}
	if chapter > f.feed.Len()-1 {
		chapter = f.feed.Len() - 1
	}
	c := f.chapter(chapter)
	if block < 0 {
		block = 0
	}
	if block > len(c.doc.Blocks)-1 {
		block = len(c.doc.Blocks) - 1
	}
	top := c.starts[block]
	if chapter == f.feed.Len()-1 {
		top = min(top, max(0, len(c.lines)-f.height))
	}
	f.topCh, f.topLine = chapter, top
}

// progress is the save-time triple: the anchor plus the display-only
// pct — (chapter + block/blocks) / chapters, read as 1.0 when the
// viewport reaches the book's final line (读完), so a finished book
// shows 100% on the shelf.
func (f *bookFlow) progress() (chapter, block int, pct float64) {
	chapter, block = f.anchor()
	n := f.feed.Len()
	c := f.chapter(chapter)
	pct = (float64(chapter) + float64(block)/float64(len(c.doc.Blocks))) / float64(n)
	if chapter == n-1 && f.topLine+f.height >= len(c.lines) {
		pct = 1
	}
	return chapter, block, min(pct, 1)
}

// relayout re-wraps every cached chapter at the new width and puts
// the anchor block's first line back on top — the width-change
// contract (锚点不丢): the position is the block, lines are derived.
func (f *bookFlow) relayout(width int) {
	ch, b := f.anchor()
	f.width = max(width, 1)
	for _, c := range f.cache {
		c.lines, c.starts = render.LayoutIndexed(c.doc, f.width)
	}
	c := f.chapter(ch)
	top := c.starts[min(b, len(c.doc.Blocks)-1)]
	if ch == f.feed.Len()-1 {
		top = min(top, max(0, len(c.lines)-f.height))
	}
	f.topCh, f.topLine = ch, top
}

// resize sets the viewport height, re-clamping the top in place.
func (f *bookFlow) resize(height int) {
	f.height = max(height, 1)
}
