package render

import (
	"strings"
	"testing"
)

func parseOne(t *testing.T, src string) *Document {
	t.Helper()
	doc, err := ParseHTML(src)
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	return doc
}

func TestHeadingsAndInlineStyles(t *testing.T) {
	doc := parseOne(t, `<h1>Title</h1><p>plain <b>bold</b> <i>it</i> <code>x</code> end</p>`)
	if len(doc.Blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(doc.Blocks))
	}
	h := doc.Blocks[0].(Heading)
	if h.Level != 1 {
		t.Fatalf("heading level = %d", h.Level)
	}
	if got := plainOf(h.Spans); got != "Title" {
		t.Fatalf("heading text = %q", got)
	}
	p := doc.Blocks[1].(Para)
	if got := plainOf(p.Spans); got != "plain bold it x end" {
		t.Fatalf("para = %q", got)
	}
	var sawBold, sawItalic, sawCode bool
	for _, s := range p.Spans {
		switch {
		case s.Text == "bold" && s.Style&SpanBold != 0:
			sawBold = true
		case s.Text == "it" && s.Style&SpanItalic != 0:
			sawItalic = true
		case s.Text == "x" && s.Style&SpanCode != 0:
			sawCode = true
		}
	}
	if !sawBold || !sawItalic || !sawCode {
		t.Fatalf("inline styles missing: bold=%v italic=%v code=%v", sawBold, sawItalic, sawCode)
	}
}

func TestLinksNumberedAndDeduped(t *testing.T) {
	doc := parseOne(t, `<p>See <a href="https://go.dev">Go</a> and <a href="https://go.dev">again</a>,
		plus <a href="#anchor">anchor</a>.</p>`)
	p := doc.Blocks[0].(Para)
	var text, markers []Span
	for _, s := range p.Spans {
		if s.Link > 0 && s.Style&SpanMarker != 0 {
			markers = append(markers, s)
		} else if s.Link > 0 {
			text = append(text, s)
		}
	}
	if len(text) != 2 || text[0].Link != 1 || text[1].Link != 1 {
		t.Fatalf("link numbering wrong: %+v", text)
	}
	// Every numbered link shows its number in the body: "Go [1]".
	if len(markers) != 2 || markers[0].Text != " [1]" || markers[1].Text != " [1]" {
		t.Fatalf("inline markers wrong: %+v", markers)
	}
	if markers[0].Link != 1 || markers[0].Style&SpanMarker == 0 {
		t.Fatalf("marker must carry its number and marker style: %+v", markers[0])
	}
	// The #anchor link gets no number and no marker.
	for _, s := range p.Spans {
		if s.Text == "anchor" && s.Link != 0 {
			t.Fatalf("anchor got a number: %+v", s)
		}
	}
	if len(doc.Links) != 1 || doc.Links[0] != "https://go.dev" {
		t.Fatalf("links = %v", doc.Links)
	}
}

func TestQuoteAndNestedList(t *testing.T) {
	doc := parseOne(t, `<blockquote><p>quoted words</p><ul><li>one</li><li>two <b>bold</b></li></ul></blockquote>`)
	q := doc.Blocks[0].(Quote)
	if len(q.Blocks) != 2 {
		t.Fatalf("quote blocks = %d", len(q.Blocks))
	}
	li := q.Blocks[1].(List)
	if len(li.Items) != 2 || li.Ordered {
		t.Fatalf("list = %+v", li)
	}
	got := plainOf(li.Items[1].Blocks[0].(Para).Spans)
	if got != "two bold" {
		t.Fatalf("item2 = %q", got)
	}
}

func TestPreExtractsLanguage(t *testing.T) {
	doc := parseOne(t, "<pre><code class=\"language-go\">func main() {}</code></pre>")
	c := doc.Blocks[0].(Code)
	if c.Lang != "go" || c.Src != "func main() {}" {
		t.Fatalf("code = %+v", c)
	}
}

func TestImageBlockAndInlineMarker(t *testing.T) {
	doc := parseOne(t, `<div><img src="https://x/a.png" alt="cover"></div><p>inline <img src="https://x/b.png"> shot</p>`)
	img := doc.Blocks[0].(Image)
	if img.Alt != "cover" || img.Ref != 1 {
		t.Fatalf("image block = %+v links=%v", img, doc.Links)
	}
	p := doc.Blocks[1].(Para)
	found := false
	for _, s := range p.Spans {
		if s.Text == "[图片:cover]" || s.Text == "[图片]" {
			found = true
			if s.Link == 0 {
				t.Fatalf("image marker has no link: %+v", s)
			}
		}
	}
	if !found {
		t.Fatalf("inline marker missing: %+v", p.Spans)
	}
}

func TestTableDegradesToRows(t *testing.T) {
	doc := parseOne(t, `<table><tr><th>Go</th><th>Rust</th></tr><tr><td>1.27</td><td>1.90</td></tr></table>`)
	if len(doc.Blocks) != 2 {
		t.Fatalf("rows = %d", len(doc.Blocks))
	}
	r0 := plainOf(doc.Blocks[0].(Para).Spans)
	if r0 != "Go | Rust" {
		t.Fatalf("row0 = %q", r0)
	}
	r1 := plainOf(doc.Blocks[1].(Para).Spans)
	if r1 != "1.27 | 1.90" {
		t.Fatalf("row1 = %q", r1)
	}
}

func TestBRSplitsParagraph(t *testing.T) {
	doc := parseOne(t, "<p>line one<br>line two</p>")
	if len(doc.Blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(doc.Blocks))
	}
	if got := plainOf(doc.Blocks[0].(Para).Spans); got != "line one" {
		t.Fatalf("first = %q", got)
	}
}

func TestWhitespaceCollapsed(t *testing.T) {
	doc := parseOne(t, "<p>\n  hello   world\t\nagain  </p>")
	if got := plainOf(doc.Blocks[0].(Para).Spans); got != "hello world again" {
		t.Fatalf("collapsed = %q", got)
	}
}

func TestScriptStyleNavDropped(t *testing.T) {
	doc := parseOne(t, `<p>keep</p><script>bad()</script><style>.x{}</style><nav><a href="/menu">menu</a></nav>`)
	if len(doc.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1: %+v", len(doc.Blocks), doc.Blocks)
	}
	if len(doc.Links) != 0 {
		t.Fatalf("nav link leaked into registry: %v", doc.Links)
	}
}

func TestBareInlineAtBlockLevelKeepsStyle(t *testing.T) {
	doc := parseOne(t, `<div><b>bold text</b> plain</div>`)
	if len(doc.Blocks) != 1 {
		t.Fatalf("blocks = %d", len(doc.Blocks))
	}
	p := doc.Blocks[0].(Para)
	if got := plainOf(p.Spans); got != "bold text plain" {
		t.Fatalf("para = %q", got)
	}
	if p.Spans[0].Style&SpanBold == 0 || p.Spans[0].Text != "bold text" {
		t.Fatalf("bold lost: %+v", p.Spans)
	}
}

func plainOf(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}
