package ui

// The flow engine's spec (M6 ticket 03): the whole book is one
// continuous line stream stitched from lazily rendered chapters, the
// viewport is a window over that stream, and the 续读位 anchor
// (spine, block) is the width-independent position contract. These
// tests run the engine against a fake feeder with deterministic
// documents — the production epub feeder and the UI pump are pinned
// in their own slices.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/render"
)

// fakeFlowFeeder serves chapters whose geometry is literal: chapter i
// has a title and paragraphs p0..p{n-1}, each laying out to exactly
// one line at the widths the tests use (texts are short), so block
// starts and line counts can be written as numbers. calls counts
// Chapter invocations per chapter — the lazy-render assertions.
type fakeFlowFeeder struct {
	titles []string    // per-chapter display title
	blocks [][]string  // per-chapter content paragraphs
	calls  map[int]int // chapter idx → render count
}

func newFakeFeeder(titles []string, blocks [][]string) *fakeFlowFeeder {
	return &fakeFlowFeeder{titles: titles, blocks: blocks, calls: map[int]int{}}
}

func (f *fakeFlowFeeder) Len() int { return len(f.titles) }

func (f *fakeFlowFeeder) Chapter(idx int) (*render.Document, string) {
	f.calls[idx]++
	doc := &render.Document{}
	for _, p := range f.blocks[idx] {
		doc.Blocks = append(doc.Blocks, render.Para{Spans: []render.Span{{Text: p}}})
	}
	return doc, f.titles[idx]
}

// newTestFlow builds an engine over the fake feeder at a fixed size,
// starting at the book head.
func newTestFlow(f *fakeFlowFeeder, width, height int) *bookFlow {
	flow := newBookFlow(f, width, height)
	flow.jump(0, 0)
	return flow
}

func plainLines(lines []render.Line) []string {
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = ln.Plain()
	}
	return out
}

// A chapter's flow document wraps the boundary (rule + chapter title
// heading) around the feeder's content, and the chapter's leading
// heading is dropped when it repeats the title (the fixture-family
// rule; the production feeder decides, the engine renders what it
// gets — pinned here through the feeder's own output shape).
//
// The engine-level pins below treat the feeder's paragraphs as the
// content and assert on geometry: block counts are content+2.
func TestFlowViewStitchesAcrossChapters(t *testing.T) {
	f := newFakeFeeder(
		[]string{"甲", "乙", "丙"},
		[][]string{{"a1"}, {"b1"}, {"c1"}})
	flow := newTestFlow(f, 40, 10)

	lines, owners := flow.view()
	// One chapter lays out as rule, blank, title, blank, para — five
	// lines per single-para chapter (the rule is min(width,40) bars).
	// Ten visible: ch0 and ch1 fill the viewport exactly; ch2 lazy.
	if len(lines) != 10 || len(owners) != 10 {
		t.Fatalf("view must fill the viewport height: %d lines, %d owners", len(lines), len(owners))
	}
	rule := strings.Repeat("─", 40)
	got := plainLines(lines)
	want := []string{rule, "", "甲", "", "a1", rule, "", "乙", "", "b1"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stitched view[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
	// Owner attribution follows the stitching: ch0×5, ch1×5.
	wantOwners := []int{0, 0, 0, 0, 0, 1, 1, 1, 1, 1}
	for i := range wantOwners {
		if owners[i] != wantOwners[i] {
			t.Fatalf("owner[%d] = %d, want %d", i, owners[i], wantOwners[i])
		}
	}
	// Only what the viewport touches rendered — ch2 stayed lazy.
	if len(f.calls) != 2 {
		t.Fatalf("chapters rendered = %v, want 2 (ch2 lazy)", f.calls)
	}
}

func TestFlowScrollCrossesBoundaries(t *testing.T) {
	f := newFakeFeeder(
		[]string{"甲", "乙", "丙"},
		[][]string{{"a1"}, {"b1"}, {"c1"}})
	flow := newTestFlow(f, 40, 10)

	// Two lines down land on the title (line 2: rule, blank, title);
	// the anchor block is the title block (block 1).
	flow.down(2)
	if ch, b := flow.anchor(); ch != 0 || b != 1 {
		t.Fatalf("anchor on title = (%d,%d), want (0,1)", ch, b)
	}
	// Down onto the blank separator before the paragraph: the anchor
	// stays on the preceding block (trailing separator semantics).
	flow.down(1)
	if ch, b := flow.anchor(); ch != 0 || b != 1 {
		t.Fatalf("anchor on separator = (%d,%d), want (0,1)", ch, b)
	}
	// Down onto the paragraph: anchor block 2.
	flow.down(1)
	if ch, b := flow.anchor(); ch != 0 || b != 2 {
		t.Fatalf("anchor on paragraph = (%d,%d), want (0,2)", ch, b)
	}
	// One more line: next chapter's rule (block 0 of chapter 1).
	flow.down(1)
	if ch, b := flow.anchor(); ch != 1 || b != 0 {
		t.Fatalf("anchor across boundary = (%d,%d), want (1,0)", ch, b)
	}

	// Up from a chapter head crosses back: chapter 1's last line is
	// its paragraph, anchor block 2 of chapter 0.
	flow.up(1)
	if ch, b := flow.anchor(); ch != 0 || b != 2 {
		t.Fatalf("anchor up across boundary = (%d,%d), want (0,2)", ch, b)
	}
	lines, _ := flow.view()
	if got := lines[0].Plain(); got != "a1" {
		t.Fatalf("top after up-cross = %q, want the last line of ch0", got)
	}

	// At the book head up does nothing; at the tail down does nothing.
	flow.jump(0, 0)
	if flow.up(1) {
		t.Fatal("up at book head must not move")
	}
	flow.jump(2, 3) // last chapter, last block
	if flow.down(1) {
		t.Fatal("down at book tail must not move")
	}
	// The last chapter is 5 lines tall and the viewport 10: the top
	// clamps to the chapter head, not past it.
	lines, _ = flow.view()
	if got := lines[0].Plain(); got != strings.Repeat("─", 40) {
		t.Fatalf("tail view starts at %q, want the last chapter's rule", got)
	}
}

func TestFlowJumpLandsOnBlockStart(t *testing.T) {
	f := newFakeFeeder(
		[]string{"甲", "乙"},
		[][]string{{"a1", "a2", "a3"}, {"b1"}})
	flow := newTestFlow(f, 40, 10)

	// Chapter 0 flow blocks: 0 rule, 1 title, 2 a1, 3 a2, 4 a3.
	flow.jump(0, 3)
	lines, owners := flow.view()
	if got := lines[0].Plain(); got != "a2" {
		t.Fatalf("jump(0,3) top = %q, want a2", got)
	}
	if owners[0] != 0 {
		t.Fatalf("jump owner = %d, want 0", owners[0])
	}
	// A block past the chapter's end clamps to its last block.
	flow.jump(0, 99)
	lines, _ = flow.view()
	if got := lines[0].Plain(); got != "a3" {
		t.Fatalf("over-jump clamps to last block, got %q", got)
	}
	// Jump into a later chapter renders only what the view needs.
	f2 := newFakeFeeder([]string{"甲", "乙", "丙"}, [][]string{{"a1"}, {"b1"}, {"c1"}})
	flow2 := newBookFlow(f2, 40, 2)
	flow2.jump(2, 0)
	if _, ok := f2.calls[0]; ok {
		t.Fatalf("jump into ch2 rendered earlier chapters: %v", f2.calls)
	}
	if _, ok := f2.calls[1]; ok {
		t.Fatalf("jump into ch2 rendered earlier chapters: %v", f2.calls)
	}
	flow2.view()
	if _, ok := f2.calls[2]; !ok {
		t.Fatal("view must render the jumped-to chapter")
	}
}

// The lazy contract: scrolling renders only what the viewport touches,
// in both directions.
func TestFlowLazyRendersOnlyTheWindow(t *testing.T) {
	titles := make([]string, 20)
	blocks := make([][]string, 20)
	for i := range titles {
		titles[i] = "章" + string(rune('A'+i))
		blocks[i] = []string{"正文" + string(rune('A'+i))}
	}
	f := newFakeFeeder(titles, blocks)
	flow := newTestFlow(f, 40, 3) // a chapter is 5 lines; the view takes 3

	// Fresh view: the first three lines of chapter 0 only.
	flow.view()
	if len(f.calls) != 1 || f.calls[0] != 1 {
		t.Fatalf("fresh view rendered %v, want only chapter 0", f.calls)
	}
	// Three lines down: top reaches line 3 of chapter 0; the view
	// needs one line of chapter 1 (its rule) to fill. Chapter 0 comes
	// from cache — only chapter 1's feed call fires.
	f.calls = map[int]int{}
	flow.down(3)
	flow.view()
	if len(f.calls) != 1 || f.calls[1] != 1 {
		t.Fatalf("after down(3) rendered %v, want ch1 feed-call only (ch0 cached)", f.calls)
	}
	// Three more: the top crosses into chapter 1 — the whole view is
	// served from cache, zero feed calls.
	f.calls = map[int]int{}
	flow.down(3)
	flow.view()
	if len(f.calls) != 0 {
		t.Fatalf("after crossing rendered %v, want zero feed calls (cached)", f.calls)
	}
}

// The progress formula: pct = (chapter + block/blocksOfChapter) /
// chapters, computed from the anchor at save time.
func TestFlowProgressFormula(t *testing.T) {
	f := newFakeFeeder(
		[]string{"甲", "乙", "丙", "丁", "戊"},
		[][]string{{"a1", "a2"}, {"b1", "b2"}, {"c1"}, {"d1"}, {"e1"}})
	flow := newTestFlow(f, 40, 10)

	flow.jump(1, 2) // chapter 1, block 2 of 4 (rule, title, b1, b2)
	if ch, b, pct := flow.progress(); ch != 1 || b != 2 || pct != 0.3 {
		t.Fatalf("progress = (%d,%d,%v), want (1,2,0.3)", ch, b, pct)
	}
	// The very first position reads as zero; a view that reaches the
	// book's final line reads as one (读完), wherever the top sits.
	flow.jump(0, 0)
	if _, _, pct := flow.progress(); pct != 0 {
		t.Fatalf("book head pct = %v, want 0", pct)
	}
	flow.jump(4, 2) // last chapter: the tail view spans the whole chapter
	if _, _, pct := flow.progress(); pct != 1 {
		t.Fatalf("book end pct = %v, want 1", pct)
	}
}

// A chapter that renders zero lines would loop the stitcher forever;
// the engine renders it as a one-blank placeholder instead (the
// production feeder guarantees non-empty, this pins the engine against
// a contract breach).
func TestFlowZeroLineChapterCannotLoop(t *testing.T) {
	f := newFakeFeeder([]string{"甲", "空", "丙"}, [][]string{{"a1"}, {}, {"c1"}})
	// Force the feeder's empty chapter through as-is by wrapping it in
	// the engine's own guard: the feeder contract is tested at the
	// production feeder; here the engine must still terminate.
	flow := newTestFlow(f, 40, 4)
	flow.jump(1, 0)
	lines, _ := flow.view()
	if len(lines) != 4 {
		t.Fatalf("view over empty chapter = %d lines, want 4 (terminated)", len(lines))
	}
	// The guarded chapter still shows its boundary: rule, blank, then
	// the title line at index 2.
	if got := lines[2].Plain(); got != "空" {
		t.Fatalf("empty chapter still shows its title: %q", got)
	}
}

// The width-change contract (锚点不丢): relayout re-wraps the cached
// chapters at the new width and puts the anchor block's first line
// back on top — the anchor is the position, lines are derived. The
// re-wrap itself is pinned through the chapter's shrinking line count
// (the wrap arithmetic has its own goldens in the render package).
func TestFlowRelayoutKeepsAnchor(t *testing.T) {
	f := newFakeFeeder(
		[]string{"甲", "乙"},
		[][]string{{strings.Repeat("a", 24)}, {strings.Repeat("b", 12)}})
	flow := newTestFlow(f, 6, 10)

	// Anchor on chapter 0's paragraph block (block 2: rule, title,
	// para): at width 6 the paragraph is 4 lines.
	flow.jump(0, 2)
	ch, b := flow.anchor()
	if ch != 0 || b != 2 {
		t.Fatalf("setup anchor = (%d,%d), want (0,2)", ch, b)
	}
	narrow := len(flow.chapter(0).lines)

	flow.relayout(12)
	ch, b = flow.anchor()
	if ch != 0 || b != 2 {
		t.Fatalf("anchor after relayout = (%d,%d), want (0,2) — 锚点不丢", ch, b)
	}
	if wide := len(flow.chapter(0).lines); wide >= narrow {
		t.Fatalf("relayout did not re-wrap: %d lines at width 12 vs %d at width 6", wide, narrow)
	}
	lines, _ := flow.view()
	if got := lines[0].Plain(); got != strings.Repeat("a", 12) {
		t.Fatalf("top after relayout = %q, want the anchor block's first line", got)
	}

	// Same contract mid-book: anchored in chapter 1's content (a third
	// chapter behind it, so no end clamp interferes), a resize keeps
	// chapter and block.
	f3 := newFakeFeeder(
		[]string{"甲", "乙", "丙"},
		[][]string{{"a1"}, {strings.Repeat("b", 12)}, {"c1"}})
	flow3 := newTestFlow(f3, 12, 10)
	flow3.jump(1, 2)
	flow3.relayout(6)
	if ch, b := flow3.anchor(); ch != 1 || b != 2 {
		t.Fatalf("mid-book anchor after relayout = (%d,%d), want (1,2)", ch, b)
	}
}

// The memory decision (LRU 16): walking past the cap evicts the
// oldest chapters (a revisit re-renders them), while a viewport that
// itself spans more chapters than the cap keeps them all — memory
// follows the viewport, never the book.
func TestFlowLRUEvictsBeyondCap(t *testing.T) {
	titles := make([]string, 20)
	blocks := make([][]string, 20)
	for i := range titles {
		titles[i] = "章" + strconv.Itoa(i)
		blocks[i] = []string{"正文" + strconv.Itoa(i)}
	}
	f := newFakeFeeder(titles, blocks)
	flow := newTestFlow(f, 40, 3) // a chapter is 5 lines; the view takes 3

	for i := 0; i < 20; i++ {
		flow.jump(i, 0)
		flow.view()
	}
	if len(flow.cache) != flowCacheChapters {
		t.Fatalf("cache = %d chapters, want the cap %d", len(flow.cache), flowCacheChapters)
	}
	for i := 0; i < 4; i++ {
		if _, ok := flow.cache[i]; ok {
			t.Fatalf("chapter %d survived the LRU: %v", i, flow.cache)
		}
	}
	// Revisiting an evicted chapter re-renders it (the call count the
	// lazy contract rides).
	if f.calls[0] != 1 {
		t.Fatalf("setup: chapter 0 rendered %d times", f.calls[0])
	}
	flow.jump(0, 0)
	if f.calls[0] != 2 {
		t.Fatalf("evicted chapter re-jump rendered %d times, want 2", f.calls[0])
	}
}

// A viewport spanning more chapters than the cap keeps them all: the
// stitcher cannot serve a view from evicted chapters, so the span is
// eviction-proof by construction (each chapter is 5 lines; 20
// chapters fit one 100-line viewport).
func TestFlowCacheFollowsTheViewport(t *testing.T) {
	titles := make([]string, 20)
	blocks := make([][]string, 20)
	for i := range titles {
		titles[i] = "章" + strconv.Itoa(i)
		blocks[i] = []string{"正文" + strconv.Itoa(i)}
	}
	f := newFakeFeeder(titles, blocks)
	flow := newBookFlow(f, 40, 100)
	flow.jump(0, 0)
	flow.view()
	if len(flow.cache) != 20 {
		t.Fatalf("cache = %d chapters, want all 20 the viewport spans", len(flow.cache))
	}
}
