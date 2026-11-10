package render

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// Goldens pin the *plain text* layout at width 60: block shapes, wrapping,
// markers, link appendix. Span styles are pinned by the span dump golden
// for the English sample.
func TestGoldenLayout(t *testing.T) {
	for _, name := range []string{"article-en", "article-zh"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", name+".html"))
			if err != nil {
				t.Fatalf("read html: %v", err)
			}
			doc, err := ParseHTML(string(raw))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := ToPlain(Layout(doc, 60)) + "\n"
			golden := filepath.Join("testdata", name+".golden")
			if *update {
				os.WriteFile(golden, []byte(got), 0o644)
				t.Logf("updated %s", golden)
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden (run go test ./internal/render -update first): %v", err)
			}
			if got != string(want) {
				t.Errorf("layout differs from golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// SpanDump golden: style bits and link numbers per span, deterministic.
func TestGoldenSpanDump(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "article-en.html"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParseHTML(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	lines := Layout(doc, 60)
	var b strings.Builder
	for i, l := range lines {
		fmt.Fprintf(&b, "%3d|", i+1)
		for _, s := range l.Spans {
			fmt.Fprintf(&b, " %s%d(%q)", styleLetters(s.Style), s.Link, s.Text)
		}
		b.WriteByte('\n')
	}
	golden := filepath.Join("testdata", "article-en.spans.golden")
	if *update {
		os.WriteFile(golden, []byte(b.String()), 0o644)
		t.Logf("updated %s", golden)
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run -update first): %v", err)
	}
	if b.String() != string(want) {
		t.Errorf("span dump differs:\n--- got ---\n%s--- want ---\n%s", b.String(), want)
	}
}

var letters = []struct {
	bit  SpanStyle
	char byte
}{
	{SpanBold, 'B'}, {SpanItalic, 'I'}, {SpanCode, 'c'}, {SpanLink, 'L'},
	{SpanQuote, 'Q'}, {SpanKw, 'K'}, {SpanStr, 'S'}, {SpanNum, 'N'},
	{SpanCom, 'M'}, {SpanUnderline, 'U'}, {SpanMarker, 'm'}, {SpanDim, 'd'},
}

func styleLetters(st SpanStyle) string {
	var sb []byte
	for _, l := range letters {
		if st&l.bit != 0 {
			sb = append(sb, l.char)
		}
	}
	if len(sb) == 0 {
		return "-"
	}
	return string(sb)
}
