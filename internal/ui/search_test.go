package ui

// The book search engine's spec (M6 ticket 04): a scan over the
// book's chapters in flow-doc coordinates — hits carry (chapter,
// block) anchors the reader's jump already speaks. The scan rides the
// flow engine's own projections: warm chapters read the LRU cache's
// rendered lines, cold chapters render fresh without entering the
// LRU (a first search must not blow the memory window open). These
// tests run against the fake feeder's literal geometry; the
// production feeder and the UI pump are pinned in their own slices.

import (
	"strings"
	"testing"
)

// runSearch drives a search to completion in one-chapter steps (the
// budget's floor case — every boundary crossing is exercised).
func runSearch(flow *bookFlow, needle string) *bookSearch {
	s := newBookSearch(needle)
	for !s.done {
		s.step(flow)
	}
	return s
}

// hitsAre asserts the hit list as (chapter, block) pairs in order.
func hitsAre(t *testing.T, s *bookSearch, want [][2]int) {
	t.Helper()
	got := make([][2]int, len(s.hits))
	for i, h := range s.hits {
		got[i] = [2]int{h.chapter, h.block}
	}
	if len(got) != len(want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hits = %v, want %v", got, want)
		}
	}
}

// Hits land in spine order, then block order, then position — the
// result list's order is the book's order. Block indices are
// flow-doc coordinates (rule=0, title=1, content from 2), the same
// indices jump consumes. Warm and cold chapters agree: chapter 0 is
// warm (the jump rendered it), 1 and 2 are cold.
func TestBookSearchHitsInBookOrder(t *testing.T) {
	f := newFakeFeeder(
		[]string{"甲", "乙", "丙"},
		[][]string{
			{"alpha first", "also Alpha here"},
			{"nothing of interest"},
			{"the alpha tail"},
		})
	flow := newTestFlow(f, 40, 10)
	s := runSearch(flow, "alpha")
	hitsAre(t, s, [][2]int{{0, 2}, {0, 3}, {2, 2}})
}

// Matching is substring, ASCII-case-insensitive (A–Z only — the
// folded pair must be the ASCII pair, Unicode casing untouched), CJK
// direct.
func TestBookSearchFoldASCIICaseOnly(t *testing.T) {
	f := newFakeFeeder(
		[]string{"甲"},
		[][]string{{"Größe STRASSE straße"}},
	)
	flow := newTestFlow(f, 40, 10)

	s := runSearch(flow, "STRASSE")
	hitsAre(t, s, [][2]int{{0, 2}}) // ASCII fold: matches both spellings

	s = runSearch(flow, "straße")
	hitsAre(t, s, [][2]int{{0, 2}}) // ß never folds — byte equality

	// Fullwidth Ａ is not ASCII A: no cross-fold.
	fw := newFakeFeeder([]string{"甲"}, [][]string{{"ＡＢ"}})
	s = runSearch(newTestFlow(fw, 40, 10), "AB")
	hitsAre(t, s, nil)

	cjk := newFakeFeeder([]string{"甲"}, [][]string{{"中文直配文本"}})
	s = runSearch(newTestFlow(cjk, 40, 10), "直配")
	hitsAre(t, s, [][2]int{{0, 2}})
}

// Every non-overlapping occurrence is its own hit (grep semantics):
// "aa" over "aaaa" finds two, advanced by the needle length.
func TestBookSearchEveryOccurrence(t *testing.T) {
	f := newFakeFeeder([]string{"甲"}, [][]string{{"x aa y aa aa"}})
	flow := newTestFlow(f, 40, 10)
	s := runSearch(flow, "aa")
	if len(s.hits) != 3 {
		t.Fatalf("hits = %d, want 3 (per occurrence, non-overlapping)", len(s.hits))
	}
}

// A first search over a cold book must not open the LRU window: cold
// chapters render for projection only, never enter the cache — the
// ticket's memory bound survives full-book scans.
func TestBookSearchColdScanKeepsLRUClean(t *testing.T) {
	f := newFakeFeeder(
		[]string{"甲", "乙", "丙", "丁"},
		[][]string{{"a"}, {"alpha"}, {"c"}, {"alpha"}})
	flow := newBookFlow(f, 40, 10) // no jump: the cache starts empty
	s := runSearch(flow, "alpha")
	hitsAre(t, s, [][2]int{{1, 2}, {3, 2}})
	if len(flow.cache) != 0 {
		t.Fatalf("cold scan entered the LRU: %d chapters cached", len(flow.cache))
	}
	if f.calls[1] != 1 || f.calls[3] != 1 {
		t.Fatalf("cold chapters must render once each: calls = %v", f.calls)
	}
}

// Cold and warm projections must be the same text (both ride the
// same pipeline at the same width): a hit found cold jumps to the
// block the warm view shows — 搜到的=看到的 across both paths.
func TestBookSearchColdWarmAgree(t *testing.T) {
	f := newFakeFeeder(
		[]string{"甲", "乙"},
		[][]string{{"quote and list shapes"}, {"alpha cold"}})
	warm := newTestFlow(f, 40, 10)
	warm.chapter(1) // both chapters into the cache
	cold := newBookFlow(f, 40, 10)

	for _, needle := range []string{"alpha", "shapes", "乙"} {
		hw := runSearch(warm, needle)
		hc := runSearch(cold, needle)
		if len(hw.hits) != len(hc.hits) {
			t.Fatalf("needle %q: warm hits %v, cold hits %v", needle, hw.hits, hc.hits)
		}
		for i := range hw.hits {
			if hw.hits[i] != hc.hits[i] {
				t.Fatalf("needle %q: warm %v, cold %v", needle, hw.hits, hc.hits)
			}
		}
	}
	// The chapter title block is searchable (it is on screen); the
	// rule block and the link appendix are not part of any hit's text
	// unless they carry the needle — the rule's bars never match prose.
	s := runSearch(warm, "乙")
	hitsAre(t, s, [][2]int{{1, 1}})
}

// An empty needle is a no-op search (done immediately, zero hits) —
// the UI never starts one, the engine stays total.
func TestBookSearchEmptyNeedle(t *testing.T) {
	f := newFakeFeeder([]string{"甲"}, [][]string{{"text"}})
	s := newBookSearch("")
	if !s.done {
		t.Fatal("empty needle must be done at birth")
	}
	s.step(newTestFlow(f, 40, 10))
	hitsAre(t, s, nil)
}

// The step budget: one step scans up to searchStepChapters chapters;
// the scan resumes exactly where the last step stopped.
func TestBookSearchStepBudget(t *testing.T) {
	f := newFakeFeeder(
		[]string{"甲", "乙", "丙", "丁", "戊", "己", "庚"},
		[][]string{{"a"}, {"b"}, {"c"}, {"d"}, {"e"}, {"f"}, {"g"}})
	flow := newTestFlow(f, 40, 10)
	s := newBookSearch("zzz") // no hits; only the cursor moves
	s.step(flow)
	if s.done || s.next != searchStepChapters {
		t.Fatalf("step 1: next = %d, done = %v", s.next, s.done)
	}
	s.step(flow) // 7 chapters, 3 left: finishes the book mid-step
	if !s.done || s.next != 7 {
		t.Fatalf("step 2: next = %d, done = %v (must finish short books early)", s.next, s.done)
	}
	s.step(flow) // stepping a done scan is a no-op
	if !s.done || s.next != 7 {
		t.Fatalf("step 3 after done: next = %d, done = %v", s.next, s.done)
	}
}

// The excerpt: radius runes each side of the occurrence, cut from
// the UNfolded text (a case-insensitive hit must quote the screen's
// original casing), clamped at the block's edges.
func TestSearchExcerpt(t *testing.T) {
	prefix, suffix := strings.Repeat("前", 30), strings.Repeat("后", 30)

	// CJK middle: exactly radius runes of context each side.
	f := newFakeFeeder([]string{"甲"}, [][]string{{prefix + "靶心" + suffix}})
	s := runSearch(newTestFlow(f, 200, 10), "靶心")
	if len(s.hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(s.hits))
	}
	want := strings.Repeat("前", excerptAssemble) + "靶心" + strings.Repeat("后", excerptAssemble)
	if s.hits[0].excerpt != want {
		t.Fatalf("excerpt = %q, want radius-clamped window", s.hits[0].excerpt)
	}

	// Folded hit, unfolded quote: the needle was ALPHA, the screen
	// shows Alpha — the excerpt must quote the screen.
	f = newFakeFeeder([]string{"甲"}, [][]string{{"aaaa Alpha bbbb"}})
	s = runSearch(newTestFlow(f, 200, 10), "ALPHA")
	if !strings.Contains(s.hits[0].excerpt, "Alpha") {
		t.Fatalf("excerpt must quote unfolded casing: %q", s.hits[0].excerpt)
	}

	// Clamps: a hit at the block's head and tail carry no phantom
	// context (no padding, no ellipsis — edge hits read naturally).
	f = newFakeFeeder([]string{"甲"}, [][]string{{"靶头" + prefix}})
	s = runSearch(newTestFlow(f, 200, 10), "靶头")
	if s.hits[0].excerpt != "靶头"+strings.Repeat("前", excerptAssemble) {
		t.Fatalf("head-clamped excerpt = %q", s.hits[0].excerpt)
	}
	f = newFakeFeeder([]string{"甲"}, [][]string{{prefix + "靶尾"}})
	s = runSearch(newTestFlow(f, 200, 10), "靶尾")
	if s.hits[0].excerpt != strings.Repeat("前", excerptAssemble)+"靶尾" {
		t.Fatalf("tail-clamped excerpt = %q", s.hits[0].excerpt)
	}
}
