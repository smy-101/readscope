package render

import (
	"strings"
	"testing"
)

func layout(t *testing.T, html string, width int) []Line {
	t.Helper()
	doc, err := ParseHTML(html)
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	return Layout(doc, width)
}

func plains(lines []Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Plain()
	}
	return out
}

func TestLayoutQuoteBarPrefix(t *testing.T) {
	lines := layout(t, `<blockquote><p>quoted text here</p></blockquote>`, 40)
	want := "│ quoted text here"
	if lines[0].Plain() != want {
		t.Fatalf("line0 = %q, want %q", lines[0].Plain(), want)
	}
	if lines[0].Spans[0].Style&SpanQuote == 0 {
		t.Fatal("quote bar missing SpanQuote style")
	}
}

func TestLayoutListMarkerAndContinuation(t *testing.T) {
	lines := layout(t,
		`<ul><li>first item</li><li>second item wraps across lines here</li></ul>`, 20)
	got := plains(lines)
	// "• second item wraps across lines here" at width 20:
	// marker(2) + content(18)
	want := []string{
		"• first item",
		"",
		"• second item wraps",
		"  across lines here",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLayoutOrderedMarkers(t *testing.T) {
	lines := layout(t, `<ol><li>one</li><li>two</li></ol>`, 40)
	got := plains(lines)
	if got[0] != "1. one" || got[2] != "2. two" {
		t.Fatalf("ordered markers wrong: %q", got)
	}
}

func TestLayoutLinkAppendix(t *testing.T) {
	lines := layout(t, `<p>see <a href="https://go.dev">go</a></p>`, 40)
	got := plains(lines)
	last := got[len(got)-1]
	if !strings.HasPrefix(last, "[1] https://go.dev") {
		t.Fatalf("appendix missing: %q", got)
	}
	if !strings.HasPrefix(got[0], "see go") {
		t.Fatalf("body wrong: %q", got[0])
	}
}

func TestLayoutCodeCarriesStyle(t *testing.T) {
	lines := layout(t, `<pre><code class="language-go">func main() {}</code></pre>`, 40)
	if len(lines) != 1 {
		t.Fatalf("lines = %d: %q", len(lines), plains(lines))
	}
	sawCode := false
	for _, s := range lines[0].Spans {
		if s.Style&SpanCode != 0 && strings.Contains(s.Text, "func") {
			if s.Style&SpanKw != 0 {
				sawCode = true
			}
		}
	}
	if !sawCode {
		t.Fatalf("keyword 'func' not highlighted: %+v", lines[0].Spans)
	}
}

func TestLayoutBlankBetweenBlocks(t *testing.T) {
	lines := layout(t, `<p>one</p><p>two</p>`, 40)
	if len(lines) != 3 || lines[1].Plain() != "" {
		t.Fatalf("expected blank separator: %q", plains(lines))
	}
}

func TestLayoutImageMarkerLine(t *testing.T) {
	lines := layout(t, `<img src="https://x/a.png" alt="cover">`, 40)
	if lines[0].Plain() != "[图片:cover]" {
		t.Fatalf("image line = %q", lines[0].Plain())
	}
	if lines[0].Spans[0].Link != 1 {
		t.Fatalf("image link = %d", lines[0].Spans[0].Link)
	}
}
