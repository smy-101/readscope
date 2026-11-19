// search.go: the book search engine (M6 ticket 04) — a scan over
// the whole book in flow-doc coordinates. A hit is an occurrence of
// the needle in a chapter's projected text, carrying the (chapter,
// block) anchor the reader's jump already consumes. The projection
// is the flow engine's own: per block, the laid-out lines' plain
// text — quote bars, list markers, indents and degradation markers
// included — so 搜到的=看到的 holds by construction (the boundary
// rule's bars and the link appendix carry no block and are never
// searchable; the chapter title block is, it is on screen).
//
// The scan is deliberately stepper-driven, not a goroutine: it reads
// the flow's LRU cache (UI state), so it runs on the message loop in
// bounded steps (searchStepChapters per message) — the UI never
// blocks on a big book. Warm chapters reuse the cache's rendered
// lines; cold chapters render fresh WITHOUT entering the LRU, so a
// first full-book search leaves the memory window untouched.

package ui

import (
	"strings"

	"github.com/smy-101/readscope/internal/render"
)

// searchStepChapters is the per-step scan budget: chapters scanned
// per step message. Four keeps a step under a few milliseconds on
// real books while a 500-chapter scan still finishes in ~125 steps.
const searchStepChapters = 4

// searchHit is one occurrence: the jump target in flow-doc
// coordinates plus the excerpt around the hit (the result row's
// meat, assembled when the hit lands).
type searchHit struct {
	chapter int
	block   int
	excerpt string
}

// bookSearch is a scan session's state: the needle, the folded
// matcher copy, the next unscanned chapter, and the hits so far.
type bookSearch struct {
	needle string
	folded string
	next   int
	done   bool
	hits   []searchHit
}

// newBookSearch starts a scan for needle. An empty needle is done at
// birth (the UI never starts one; the engine stays total).
func newBookSearch(needle string) *bookSearch {
	s := &bookSearch{needle: needle}
	s.folded = asciiFold(needle)
	if needle == "" {
		s.done = true
	}
	return s
}

// step scans up to searchStepChapters chapters from the cursor,
// appending hits in book order (spine, then block, then position).
func (s *bookSearch) step(flow *bookFlow) {
	if s.done {
		return
	}
	n := flow.feed.Len()
	for i := 0; i < searchStepChapters && s.next < n; i++ {
		s.scanChapter(flow, s.next)
		s.next++
	}
	if s.next >= n {
		s.done = true
	}
}

// scanChapter projects one chapter and collects its hits. Warm: the
// LRU cache's rendered lines. Cold: a fresh render through the same
// pipeline (feeder + boundary wrap + layout at the flow's width) —
// pure functions, so cold and warm projections agree by identity of
// inputs — without touching the cache.
func (s *bookSearch) scanChapter(flow *bookFlow, ch int) {
	var texts []string
	if c, ok := flow.cache[ch]; ok {
		texts = blockTexts(c.doc.Blocks, c.lines, c.starts)
	} else {
		content, title := flow.feed.Chapter(ch)
		doc := ensureContentDoc(content)
		doc = wrapBoundary(title, doc)
		lines, starts := render.LayoutIndexed(doc, flow.width)
		texts = blockTexts(doc.Blocks, lines, starts)
	}
	for b, text := range texts {
		folded := asciiFold(text)
		for _, at := range matchAll(folded, s.folded) {
			s.hits = append(s.hits, searchHit{
				chapter: ch, block: b,
				excerpt: excerpt(text, at, len(s.needle)),
			})
		}
	}
}

// excerptAssemble is the excerpt radius: runes of context on each
// side of the hit (the ticket's 命中前后 N 字, N=16).
const excerptAssemble = 16

// excerpt assembles the hit's context: radius runes each side of
// the occurrence, cut from the unfolded text — the projection is
// what the screen shows, so the excerpt quotes the screen's casing.
// at is a byte offset in the folded text; A–Z folding preserves
// byte layout, so the offset translates to the unfolded text
// directly. Clamped at the block's edges: no padding, no markers.
func excerpt(text string, at, n int) string {
	rs := []rune(text)
	rAt := len([]rune(text[:at]))
	rEnd := rAt + len([]rune(text[at:at+n]))
	lo := max(rAt-excerptAssemble, 0)
	hi := min(rEnd+excerptAssemble, len(rs))
	return string(rs[lo:hi])
}

// blockTexts projects a laid-out chapter into per-block text: block
// b's lines concatenated — exactly the characters on screen for that
// block, wrapping read across line breaks. starts covers content
// blocks only, so the link appendix (after the sentinel) never
// enters a projection.
func blockTexts(blocks []render.Block, lines []render.Line, starts []int) []string {
	texts := make([]string, len(blocks))
	for b := range blocks {
		var sb strings.Builder
		for _, ln := range lines[starts[b]:starts[b+1]] {
			sb.WriteString(ln.Plain())
		}
		texts[b] = sb.String()
	}
	return texts
}

// asciiFold lowercases the ASCII letters A–Z only (byte-wise, no
// Unicode casing): the ticket's ASCII-case-insensitive match — CJK
// and non-ASCII letters compare byte-equal.
func asciiFold(s string) string {
	changed := false
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			changed = true
			break
		}
	}
	if !changed {
		return s
	}
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// matchAll reports every non-overlapping occurrence of needle in
// haystack (both pre-folded), advancing by the needle length.
func matchAll(haystack, needle string) []int {
	if needle == "" {
		return nil
	}
	var at []int
	off := 0
	for {
		i := strings.Index(haystack[off:], needle)
		if i < 0 {
			return at
		}
		at = append(at, off+i)
		off += i + len(needle)
	}
}
