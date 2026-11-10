// wrap.go: span-aware line wrapping with CJK rules. Pure functions —
// the heaviest-tested file in the pipeline.
//
// Terminal width counts East-Asian wide runes as 2 columns (go-runewidth).
// Break opportunities: after a space, and between two runes when either
// side is CJK (Han/Kana/Hangul/CJK punctuation). Latin words never split
// unless a single word exceeds the width (hard break). Kinsoku (禁則):
// a line never ends with an opening bracket nor starts with closing
// punctuation / commas — enforced by pull-up, then push-down (追い出し).
package render

import (
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// noStart lists runes that may not begin a line (kinsoku).
const noStart = "。，、·：；！？％）］｝》〉】〕」』…‥ー,.;:!?)]}»"

// noEnd lists runes that may not end a line.
const noEnd = "（［｛《〈【〔「『([{<\"'"

func inSet(r rune, set string) bool { return strings.ContainsRune(set, r) }

// isCJK reports whether r may take a break on either side: Han, Kana,
// Hangul, or CJK punctuation.
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r) ||
		isCJKPunct(r)
}

func isCJKPunct(r rune) bool {
	switch r {
	case '。', '，', '、', '：', '；', '！', '？', '（', '）',
		'《', '》', '〈', '〉', '【', '】', '〔', '〕', '「', '」',
		'『', '』', '…', '‥', 'ー', '·', '％':
		return true
	}
	return false
}

// cell is one rune carrying its style and link number.
type cell struct {
	r     rune
	style SpanStyle
	link  int
}

func (c cell) width() int { return runewidth.RuneWidth(c.r) }

// wrapSpans wraps spans to the given terminal width. width <= 0 returns
// the input as a single unwrapped line.
func wrapSpans(spans []Span, width int) []Line {
	cells := flatten(spans)
	if width <= 0 {
		return []Line{{Spans: spans}}
	}
	rows := greedyWrap(cells, width)
	rows = kinsoku(rows, width)
	lines := make([]Line, len(rows))
	for i, row := range rows {
		lines[i] = Line{Spans: groupSpans(row)}
	}
	return lines
}

func flatten(spans []Span) []cell {
	var cells []cell
	for _, s := range spans {
		for _, r := range s.Text {
			cells = append(cells, cell{r: r, style: s.Style, link: s.Link})
		}
	}
	return cells
}

func groupSpans(row []cell) []Span {
	var spans []Span
	for _, c := range row {
		if len(spans) > 0 {
			last := &spans[len(spans)-1]
			if last.Style == c.style && last.Link == c.link {
				last.Text += string(c.r)
				continue
			}
		}
		spans = append(spans, Span{Text: string(c.r), Style: c.style, Link: c.link})
	}
	return spans
}

func rowWidth(row []cell) int {
	w := 0
	for _, c := range row {
		w += c.width()
	}
	return w
}

// greedyWrap fills lines left to right; on overflow it backtracks to the
// last break opportunity, or hard-breaks mid-word when none exists.
func greedyWrap(cells []cell, width int) [][]cell {
	var rows [][]cell
	var row []cell

	for i := 0; i < len(cells); {
		c := cells[i]
		// A space at the break point is dropped, never carried over.
		if c.r == ' ' && len(row) > 0 && rowWidth(row)+1 > width {
			rows = append(rows, trimRight(row))
			row = nil
			i++
			continue
		}
		if len(row) > 0 && rowWidth(row)+c.width() > width {
			// Prefer breaking right here when the row ends with a space
			// (the space is dropped): keeps words together.
			brk := -1
			if row[len(row)-1].r == ' ' || canBreakBetween(row[len(row)-1].r, c.r) {
				brk = len(row)
			} else {
				// Backtrack to the last opportunity inside the row.
				for j := len(row) - 1; j > 0; j-- {
					if canBreakBetween(row[j-1].r, row[j].r) {
						brk = j
						break
					}
				}
			}
			if brk > 0 {
				rows = append(rows, trimRight(row[:brk]))
				row = append(row[:0:0], row[brk:]...)
				continue // re-evaluate the same cell against the shorter row
			}
			// No opportunity: hard break (row is full width by now).
			rows = append(rows, row)
			row = nil // fresh backing: rows must not alias future writes
			continue
		}
		row = append(row, c)
		i++
	}
	if len(row) > 0 {
		rows = append(rows, trimRight(row))
	}
	return rows
}

func canBreakBetween(prev, cur rune) bool {
	if prev == ' ' {
		return true
	}
	if cur == ' ' {
		return false
	}
	return isCJK(prev) || isCJK(cur)
}

func trimRight(row []cell) []cell {
	for len(row) > 0 && row[len(row)-1].r == ' ' {
		row = row[:len(row)-1]
	}
	return row
}

// kinsoku fixes forbidden line boundaries:
//
//	a. pull-up: next line starts with a noStart rune that still fits;
//	b. push-down (追い出し): next line starts with a noStart rune that
//	   does not fit → move the current line's last rune down;
//	c. noEnd: current line ends with an opening bracket → move it down.
func kinsoku(rows [][]cell, width int) [][]cell {
	for i := 0; i+1 < len(rows); i++ {
		for pass := 0; pass < 4; pass++ {
			cur, next := rows[i], rows[i+1]
			if len(next) == 0 {
				break
			}
			// a. pull up a fitting noStart rune.
			first := next[0]
			if inSet(first.r, noStart) && rowWidth(cur)+first.width() <= width {
				rows[i] = append(cur, first)
				rows[i+1] = next[1:]
				continue
			}
			// b. push the last rune of the current line down.
			if inSet(first.r, noStart) && len(cur) > 1 {
				moved := cur[len(cur)-1]
				rows[i] = cur[:len(cur)-1]
				rows[i+1] = append([]cell{moved}, next...)
				continue
			}
			// c. current line ends with an opening bracket.
			if len(cur) > 1 && inSet(cur[len(cur)-1].r, noEnd) {
				moved := cur[len(cur)-1]
				rows[i] = cur[:len(cur)-1]
				rows[i+1] = append([]cell{moved}, next...)
				continue
			}
			break
		}
	}
	// Drop rows that became empty (only possible via aggressive pushes).
	out := rows[:0]
	for _, r := range rows {
		if len(r) > 0 {
			out = append(out, r)
		}
	}
	return out
}

// hardSplit splits preformatted code lines at width without reflowing.
// Empty span slices still produce one (empty) line: blank lines inside
// code blocks are content.
func hardSplit(spans []Span, width int) []Line {
	if width <= 0 {
		return []Line{{Spans: spans}}
	}
	var lines []Line
	var cur []cell
	w := 0
	for _, s := range spans {
		for _, r := range s.Text {
			cw := runewidth.RuneWidth(r)
			if w+cw > width && len(cur) > 0 {
				lines = append(lines, Line{Spans: groupSpans(cur)})
				cur, w = nil, 0
			}
			cur = append(cur, cell{r: r, style: s.Style, link: s.Link})
			w += cw
		}
	}
	if len(cur) > 0 {
		lines = append(lines, Line{Spans: groupSpans(cur)})
	} else if len(lines) == 0 {
		lines = append(lines, Line{})
	}
	return lines
}
