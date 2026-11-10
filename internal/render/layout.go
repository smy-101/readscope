// layout.go: Document → []Line at a given width. All block shapes
// (headings, quotes, lists, code, rules, link appendix) become flat lines
// with prefixes and indentation baked in — the TUI just scrolls them.
package render

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
)

// Layout renders the document to lines. width <= 0 lays out unwrapped
// (single logical line per block line).
func Layout(doc *Document, width int) []Line {
	var lines []Line
	for i, b := range doc.Blocks {
		if i > 0 {
			lines = append(lines, Line{})
		}
		lines = append(lines, layoutBlock(b, width, 0)...)
	}
	if len(doc.Links) > 0 {
		lines = append(lines, Line{})
		for n, url := range doc.Links {
			spans := []Span{
				{Text: fmt.Sprintf("[%d] ", n+1), Style: SpanMarker | SpanDim},
				{Text: url, Style: SpanLink | SpanDim, Link: n + 1},
			}
			lines = append(lines, wrapSpans(spans, width)...)
		}
	}
	return lines
}

// layoutBlock lays out one block at the given indent (columns).
func layoutBlock(b Block, width, indent int) []Line {
	inner := width - indent
	switch v := b.(type) {
	case Heading:
		return withIndent(wrapSpans(v.Spans, inner), indent)
	case Para:
		return withIndent(wrapSpans(v.Spans, inner), indent)
	case Quote:
		var out []Line
		for i, ib := range v.Blocks {
			if i > 0 {
				out = append(out, Line{})
			}
			out = append(out, layoutBlock(ib, inner-2, indent)...)
		}
		for i := range out {
			bar := Span{Text: "│ ", Style: SpanQuote}
			spans := make([]Span, 0, len(out[i].Spans)+1)
			spans = append(spans, bar)
			spans = append(spans, out[i].Spans...)
			out[i] = Line{Spans: spans}
		}
		return out
	case Code:
		var out []Line
		for _, line := range HighlightLines(v.Lang, v.Src) {
			out = append(out, withIndent(hardSplit(line, inner), indent)...)
		}
		return out
	case Image:
		text := "[图片]"
		if v.Alt != "" {
			text = "[图片:" + v.Alt + "]"
		}
		return []Line{{Spans: []Span{{Text: text, Style: SpanMarker | SpanLink, Link: v.Ref}}}}
	case Rule:
		n := inner
		if n > 40 {
			n = 40
		}
		return []Line{{Spans: []Span{{Text: strings.Repeat("─", max(n, 1)), Style: SpanDim}}}}
	case List:
		var out []Line
		for idx, item := range v.Items {
			marker := "•"
			if v.Ordered {
				marker = strconv.Itoa(idx+1) + "."
			}
			mw := runewidth.StringWidth(marker) + 1
			var itemLines []Line
			for j, ib := range item.Blocks {
				if j > 0 {
					itemLines = append(itemLines, Line{})
				}
				itemLines = append(itemLines, layoutBlock(ib, width, indent+mw)...)
			}
			if len(itemLines) == 0 {
				continue
			}
			out = append(out, applyMarker(itemLines, indent, marker, mw)...)
			if idx+1 < len(v.Items) {
				out = append(out, Line{}) // blank between items
			}
		}
		return out
	default:
		return nil
	}
}

// applyMarker puts the list marker on the item's first line. Content was
// laid out at indent+mw, so the first line starts with that many spaces —
// the marker rewrites the prefix to indent-spaces + marker + filler,
// keeping column alignment for continuation lines.
func applyMarker(lines []Line, indent int, marker string, mw int) []Line {
	prefix := strings.Repeat(" ", indent) + marker + " "
	if len(lines) == 0 {
		return lines
	}
	first := &lines[0]
	if len(first.Spans) > 0 && first.Spans[0].Text == strings.Repeat(" ", indent+mw) {
		first.Spans[0].Text = prefix
	} else if len(first.Spans) > 0 {
		spans := append([]Span{{Text: prefix, Style: SpanMarker}}, first.Spans...)
		first.Spans = spans
	} else {
		first.Spans = []Span{{Text: prefix, Style: SpanMarker}}
	}
	return lines
}

func withIndent(lines []Line, indent int) []Line {
	if indent <= 0 {
		return lines
	}
	pad := strings.Repeat(" ", indent)
	for i := range lines {
		if len(lines[i].Spans) == 0 {
			continue
		}
		spans := make([]Span, 0, len(lines[i].Spans)+1)
		spans = append(spans, Span{Text: pad})
		spans = append(spans, lines[i].Spans...)
		lines[i] = Line{Spans: spans}
	}
	return lines
}
