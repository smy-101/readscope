package render

import (
	"strings"
	"testing"
)

func plainLines(t *testing.T, spans []Span, width int) []string {
	t.Helper()
	lines := wrapSpans(spans, width)
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Plain()
	}
	return out
}

func spans(s string) []Span { return []Span{{Text: s}} }

func TestWrapLatinOnSpaces(t *testing.T) {
	got := plainLines(t, spans("the quick brown fox"), 10)
	want := []string{"the quick", "brown fox"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWrapHardBreaksLongWord(t *testing.T) {
	got := plainLines(t, spans("abcdefghijklmno"), 5)
	want := []string{"abcde", "fghij", "klmno"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWrapCJKBetweenRunes(t *testing.T) {
	got := plainLines(t, spans("一二三四五六七八九十"), 6) // 3 chars per line (2 cols each)
	want := []string{"一二三", "四五六", "七八九", "十"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWrapMixedCJKLatin(t *testing.T) {
	// Break between Latin and CJK, never inside the Latin word.
	// "Go 语言" is 7 columns — too wide for 6 — so 言 wraps down.
	got := plainLines(t, spans("Go 语言版本 1.27 发布说明"), 6)
	want := []string{"Go 语", "言版本", "1.27", "发布说", "明"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// 禁則: a full-width comma may not start a line even though it fits
// nowhere by greedy — the preceding rune is pushed down with it (追い出し).
func TestKinsokuNoStartComma(t *testing.T) {
	got := plainLines(t, spans("一二三四，五"), 8)
	want := []string{"一二三", "四，五"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// 禁則: pull-up — a trailing punctuation that fits joins the line above.
func TestKinsokuPullUp(t *testing.T) {
	got := plainLines(t, spans("一二三四。五六"), 10)
	want := []string{"一二三四。", "五六"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// 禁則： an opening full-width bracket may not end a line.
func TestKinsokuNoEndBracket(t *testing.T) {
	got := plainLines(t, spans("一二（abcdef"), 8)
	want := []string{"一二", "（abcdef"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWrapPreservesSpanStylesAcrossLines(t *testing.T) {
	in := []Span{{Text: "bold words stay bold", Style: SpanBold}}
	lines := wrapSpans(in, 6)
	want := []string{"bold", "words", "stay", "bold"}
	got := make([]string, len(lines))
	for i, l := range lines {
		got[i] = l.Plain()
		for _, s := range l.Spans {
			if s.Style&SpanBold == 0 {
				t.Errorf("line %d span %q lost bold", i, s.Text)
			}
		}
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWrapPreservesLinkNumbers(t *testing.T) {
	in := []Span{{Text: "see this interesting link now", Style: SpanLink, Link: 3}}
	lines := wrapSpans(in, 10)
	for _, l := range lines {
		for _, s := range l.Spans {
			if s.Link != 3 {
				t.Errorf("span %q lost link number: %d", s.Text, s.Link)
			}
		}
	}
}

func TestWrapZeroWidthIsNoBreakPoint(t *testing.T) {
	// Combining accent attaches to the previous rune's line.
	in := []Span{{Text: "a\u0301b\u0301cdefgh"}}
	lines := wrapSpans(in, 4)
	if len(lines) < 2 {
		t.Fatalf("expected wrapping, got %v", lines)
	}
	if !strings.HasPrefix(lines[0].Plain(), "a\u0301") {
		t.Fatalf("combining rune detached: %q", lines[0].Plain())
	}
}

func TestHardSplitKeepsCodeVerbatim(t *testing.T) {
	in := []Span{{Text: "ab  cd efgh", Style: SpanCode}}
	lines := hardSplit(in, 4)
	want := []string{"ab  ", "cd e", "fgh"}
	got := make([]string, len(lines))
	for i, l := range lines {
		got[i] = l.Plain()
		for _, s := range l.Spans {
			if s.Style&SpanCode == 0 {
				t.Errorf("code style lost: %+v", s)
			}
		}
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWrapWidthZeroReturnsSingleLine(t *testing.T) {
	lines := wrapSpans(spans("anything at all"), 0)
	if len(lines) != 1 || lines[0].Plain() != "anything at all" {
		t.Fatalf("got %v", lines)
	}
}
