// Package render is readscope's shared text pipeline: HTML (feed articles)
// and XHTML (EPUB chapters) flow through the same stages —
//
//	HTML → DOM (x/net/html) → Document (blocks + numbered links)
//	     → Layout(width) → []Line (the row model the TUI scrolls)
//	     → ANSI (theme colors, chroma for code)
//
// Lines are *logical*: spans carry style bits and link numbers, never raw
// ANSI. Golden tests pin the logical output; presentation (theme) is a
// separate leaf. This file defines the model.
package render

// SpanStyle is a bit set carried by every span. Presentation layers map
// bits to colors; the pipeline itself never emits ANSI.
type SpanStyle uint16

const (
	SpanBold SpanStyle = 1 << iota
	SpanItalic
	SpanCode      // monospace content (inline code / code block text)
	SpanLink      // text that is a link (Link number in Span)
	SpanQuote     // inside a blockquote
	SpanKw        // chroma: keyword
	SpanStr       // chroma: string
	SpanNum       // chroma: number
	SpanCom       // chroma: comment
	SpanUnderline // h1/h2
	SpanMarker    // image markers, list markers, link appendix prefix
	SpanDim       // meta-ish text (link URLs in the appendix)
)

// Span is a run of text with one uniform style and optional link number.
type Span struct {
	Text  string
	Style SpanStyle
	Link  int // 1-based index into Document.Links; 0 = not a link
}

// Line is one terminal row after layout: spans already wrapped, quote
// bars and list indentation already applied.
type Line struct {
	Spans []Span
}

// Plain returns the line's text without style (tests, search anchoring).
func (l Line) Plain() string {
	var b []byte
	for _, s := range l.Spans {
		b = append(b, s.Text...)
	}
	return string(b)
}

// Block is one element of the intermediate document.
type Block interface{ block() }

// Heading is h1–h6. Layout renders h1/h2 bold+underlined, h3–h6 bold.
type Heading struct {
	Level int
	Spans []Span
}

// Para is a text paragraph.
type Para struct{ Spans []Span }

// Quote is a blockquote; it contains whole blocks.
type Quote struct{ Blocks []Block }

// Code is a preformatted block. Lang may be empty; Src is verbatim.
type Code struct {
	Lang string
	Src  string
}

// Image is a standalone image, degraded to a marker line. Ref is the
// 1-based link number the marker points at (registered in Document.Links).
type Image struct {
	Alt string
	Ref int
}

// ListItem is one entry of a List. Blocks usually hold a single Para but
// may nest (a sub-list inside an item).
type ListItem struct {
	Blocks []Block
}

// List is ul (Ordered=false) or ol.
type List struct {
	Ordered bool
	Items   []ListItem
}

// Rule is <hr>.
type Rule struct{}

// Document is the pipeline's intermediate form: a flat block sequence
// plus the numbered link table shared by links and image markers.
type Document struct {
	Blocks []Block
	Links  []string // 1-based: Links[0] is link [1]
}

func (Heading) block() {}
func (Para) block()    {}
func (Quote) block()   {}
func (Code) block()    {}
func (Image) block()   {}
func (List) block()    {}
func (Rule) block()    {}

// linkRegistry deduplicates hrefs while walking: the same URL always gets
// the same number (duplicate links in a post share one appendix entry).
type linkRegistry struct {
	urls []string
	idx  map[string]int
}

func newLinkRegistry() *linkRegistry {
	return &linkRegistry{idx: map[string]int{}}
}

func (r *linkRegistry) add(href string) int {
	if n, ok := r.idx[href]; ok {
		return n
	}
	r.urls = append(r.urls, href)
	n := len(r.urls)
	r.idx[href] = n
	return n
}
