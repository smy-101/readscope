// dom.go: HTML/XHTML → Document. A whitelist walker: known block tags map
// to blocks, known inline tags carry style bits, everything else is either
// transparent (recursed) or dropped (script/style/nav/…). Feed articles and
// EPUB chapters share this path.
package render

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ParseHTML parses an HTML fragment or document into a Document. The
// fragment path avoids html/head boilerplate when feed content is a bare
// fragment.
func ParseHTML(src string) (*Document, error) {
	nodes, err := html.ParseFragment(strings.NewReader(src), &html.Node{
		Type: html.ElementNode, Data: "div", DataAtom: atom.Div,
	})
	if err != nil {
		return nil, err
	}
	// Synthetic container: the flow pass below groups the fragment's own
	// nodes correctly (a lone <blockquote> must hit the block dispatch,
	// not have its children processed in place).
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	doc := &Document{}
	reg := newLinkRegistry()
	doc.Blocks = blocksFor(root, reg, false)
	doc.Links = reg.urls
	return doc, nil
}

// droppedTags never contribute content.
var droppedTags = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true,
	"title": true, "meta": true, "link": true, "head": true,
	"svg": true, "iframe": true, "form": true, "button": true,
	"input": true, "select": true, "textarea": true, "canvas": true,
	"audio": true, "video": true, "object": true, "embed": true,
	"nav": true, "aside": true, "footer": true,
}

// blocksFor converts a container's children into blocks with flow
// grouping: consecutive inline-level siblings (text, <b>, <a>, inline
// <img> markers, …) merge into ONE paragraph; block-level children flush
// the buffer and dispatch individually. A lone <img> becomes an Image
// block; an <img> between words becomes an inline marker.
func blocksFor(n *html.Node, reg *linkRegistry, inQuote bool) []Block {
	var out []Block
	var buf []*html.Node

	flush := func() {
		if len(buf) == 0 {
			return
		}
		var spans []Span
		for _, m := range buf {
			switch m.Type {
			case html.TextNode:
				spans = append(spans, collapse(m.Data, quoteStyle(0, inQuote), 0)...)
			case html.ElementNode:
				spans = append(spans, inlineFor(m, quoteStyle(0, inQuote), 0, reg)...)
			}
		}
		out = append(out, paraOrNone(mergeSpans(spans))...)
		buf = nil
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.TextNode || c.Type == html.CommentNode:
			if strings.TrimSpace(c.Data) != "" || len(buf) > 0 {
				buf = append(buf, c)
			}
		case c.Type == html.ElementNode && (isInlineTag[c.Data] || c.Data == "br"):
			buf = append(buf, c)
		case c.Type == html.ElementNode && c.Data == "img":
			if len(buf) > 0 { // mid-sentence image → inline marker
				buf = append(buf, c)
				continue
			}
			out = append(out, blockFor(c, reg, inQuote)...)
		default:
			flush()
			out = append(out, blockFor(c, reg, inQuote)...)
		}
	}
	flush()
	return out
}

func quoteStyle(st SpanStyle, inQuote bool) SpanStyle {
	if inQuote {
		return st | SpanQuote
	}
	return st
}

func blockFor(n *html.Node, reg *linkRegistry, inQuote bool) []Block {
	switch n.Type {
	case html.TextNode:
		spans := collapse(n.Data, 0, 0)
		if len(spans) == 0 {
			return nil
		}
		return []Block{Para{Spans: spans}}
	case html.CommentNode:
		return nil
	case html.ElementNode:
	default:
		return nil
	}

	tag := n.Data
	if droppedTags[tag] {
		return nil
	}

	switch tag {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level := int(tag[1] - '0')
		st := SpanBold
		if level <= 2 {
			st = SpanBold | SpanUnderline
		}
		spans := inlineChildren(n, 0, 0, reg, inQuote)
		var out []Block
		for _, part := range splitSpansOnNewline(spans) {
			if part = trimSpans(part); len(part) > 0 {
				out = append(out, Heading{Level: level, Spans: withStyle(part, st)})
			}
		}
		return out

	case "p", "figcaption", "caption", "dt", "dd", "summary":
		return paraOrNone(inlineChildren(n, 0, 0, reg, inQuote))

	case "blockquote", "q":
		return []Block{Quote{Blocks: blocksFor(n, reg, true)}}

	case "pre":
		lang := langOf(n)
		src := verbatim(n)
		src = strings.TrimSuffix(src, "\n")
		return []Block{Code{Lang: lang, Src: expandTabs(src)}}

	case "hr":
		return []Block{Rule{}}

	case "img":
		return imageBlock(n, reg)

	case "ul", "ol":
		list := List{Ordered: tag == "ol"}
		for li := n.FirstChild; li != nil; li = li.NextSibling {
			if li.Type != html.ElementNode || li.Data != "li" {
				continue
			}
			list.Items = append(list.Items, ListItem{Blocks: blocksFor(li, reg, inQuote)})
		}
		if len(list.Items) == 0 {
			return nil
		}
		return []Block{list}

	case "table":
		return tableBlocks(n, reg, inQuote)

	case "br":
		return nil

	case "figure":
		return blocksFor(n, reg, inQuote)

	default:
		// Transparent containers (div/section/article/main/td/…): flow.
		return blocksFor(n, reg, inQuote)
	}
}

// inlineChildren collects the inline content of n as spans. Newlines from
// <br> are emitted as standalone "\n" spans — splitParas turns them into
// separate paragraphs.
func inlineChildren(n *html.Node, st SpanStyle, link int, reg *linkRegistry, inQuote bool) []Span {
	if inQuote {
		st |= SpanQuote
	}
	var spans []Span
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			spans = append(spans, collapse(c.Data, st, link)...)
		case html.ElementNode:
			spans = append(spans, inlineFor(c, st, link, reg)...)
		}
	}
	return mergeSpans(spans)
}

func inlineFor(n *html.Node, st SpanStyle, link int, reg *linkRegistry) []Span {
	tag := n.Data
	if droppedTags[tag] {
		return nil
	}
	switch tag {
	case "br":
		return []Span{{Text: "\n"}}
	case "b", "strong":
		return inlineChildren(n, st|SpanBold, link, reg, false)
	case "i", "em", "cite", "var":
		return inlineChildren(n, st|SpanItalic, link, reg, false)
	case "u", "ins":
		return inlineChildren(n, st|SpanUnderline, link, reg, false)
	case "code", "kbd", "samp", "tt":
		return inlineChildren(n, st|SpanCode, link, reg, false)
	case "s", "del", "strike":
		return inlineChildren(n, st|SpanDim, link, reg, false)
	case "q":
		inner := inlineChildren(n, st, link, reg, false)
		return append([]Span{{Text: "「", Style: st}}, append(inner, Span{Text: "」", Style: st})...)
	case "a":
		href := attr(n, "href")
		nl := link
		if href != "" && !strings.HasPrefix(href, "#") {
			nl = reg.add(href)
			st |= SpanLink
		}
		return inlineChildren(n, st, nl, reg, false)
	case "img":
		alt := attr(n, "alt")
		src := attr(n, "src")
		text := "[图片]"
		if alt != "" {
			text = "[图片:" + alt + "]"
		}
		style := st | SpanMarker
		l := link
		if src != "" {
			l = reg.add(src)
			style |= SpanLink
		}
		return []Span{{Text: text, Style: style, Link: l}}
	case "script", "style":
		return nil
	default:
		return inlineChildren(n, st, link, reg, false)
	}
}

// imageBlock builds a standalone Image block from an <img> outside inline
// context.
func imageBlock(n *html.Node, reg *linkRegistry) []Block {
	src := attr(n, "src")
	if src == "" {
		return nil
	}
	img := Image{Alt: attr(n, "alt")}
	img.Ref = reg.add(src)
	return []Block{img}
}

// tableBlocks degrades a table to one paragraph per row, cells joined by
// " | " (full table rendering is out of scope for v1).
func tableBlocks(n *html.Node, reg *linkRegistry, inQuote bool) []Block {
	var out []Block
	var walk func(*html.Node)
	walk = func(m *html.Node) {
		for c := m.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				switch c.Data {
				case "tr":
					out = append(out, rowPara(c, reg, inQuote)...)
				case "tbody", "thead", "tfoot", "table":
					walk(c)
				}
			}
		}
	}
	walk(n)
	return out
}

func rowPara(tr *html.Node, reg *linkRegistry, inQuote bool) []Block {
	var cells []Span
	for td := tr.FirstChild; td != nil; td = td.NextSibling {
		if td.Type != html.ElementNode {
			continue
		}
		switch td.Data {
		case "td", "th":
			if len(cells) > 0 {
				cells = append(cells, Span{Text: " | "})
			}
			cells = append(cells, inlineChildren(td, 0, 0, reg, inQuote)...)
		}
	}
	return paraOrNone(cells)
}

// --- helpers -------------------------------------------------------------

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

// langOf extracts a code language from class="language-x" / "lang-x" on
// the <pre> or its child <code>.
func langOf(pre *html.Node) string {
	for _, n := range []*html.Node{pre, firstChildElement(pre)} {
		if n == nil {
			continue
		}
		for _, class := range strings.Fields(attr(n, "class")) {
			for _, prefix := range []string{"language-", "lang-"} {
				if strings.HasPrefix(class, prefix) {
					return strings.TrimPrefix(class, prefix)
				}
			}
		}
	}
	return ""
}

func firstChildElement(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			return c
		}
	}
	return nil
}

// verbatim returns raw text content (for <pre>).
func verbatim(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(m *html.Node) {
		for c := m.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case html.TextNode:
				b.WriteString(c.Data)
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}

// collapse normalizes a text node for inline context: runs of whitespace
// become one space. Edge spaces survive here — paragraph assembly trims
// them, because inter-span spaces are meaningful ("<b>a</b> <i>b</i>").
func collapse(s string, st SpanStyle, link int) []Span {
	s = wsRe.ReplaceAllString(s, " ")
	if s == "" {
		return nil
	}
	return []Span{{Text: s, Style: st, Link: link}}
}

var wsRe = regexp.MustCompile(`[ \t\n\r\f\v]+`)

// mergeSpans joins adjacent spans with identical style+link and drops
// empty ones. "\n" (br) spans stay standalone: paragraph assembly splits
// on them. Finally, spaces between CJK runes are squeezed out — they are
// source-formatting artifacts (HTML newlines), not typography.
func mergeSpans(in []Span) []Span {
	var out []Span
	for _, s := range in {
		if s.Text == "" {
			continue
		}
		if s.Text == "\n" {
			out = append(out, s)
			continue
		}
		if n := len(out); n > 0 && out[n-1].Style == s.Style && out[n-1].Link == s.Link && out[n-1].Text != "\n" {
			out[n-1].Text += s.Text
			continue
		}
		out = append(out, s)
	}
	return squeezeCJKSpaces(out)
}

// squeezeCJKSpaces removes single spaces whose neighbors on both sides are
// CJK runes (Han/Kana/Hangul/CJK punctuation). "中文 没有" from a source
// line break becomes "中文没有"; "RSS 全文" keeps its space.
func squeezeCJKSpaces(spans []Span) []Span {
	cells := flatten(spans)
	if len(cells) < 3 {
		return spans
	}
	var kept []cell
	dropped := false
	for i, c := range cells {
		if c.r == ' ' && i > 0 && i+1 < len(cells) &&
			isCJK(cells[i-1].r) && isCJK(cells[i+1].r) {
			dropped = true
			continue
		}
		kept = append(kept, c)
	}
	if !dropped {
		return spans
	}
	return groupSpans(kept)
}

// paraOrNone wraps spans as a Para, dropping whitespace-only content.
func paraOrNone(spans []Span) []Block {
	if len(spans) == 0 {
		return nil
	}
	var out []Block
	for _, part := range splitSpansOnNewline(spans) {
		if part = trimSpans(part); len(part) > 0 {
			out = append(out, Para{Spans: part})
		}
	}
	return out
}

// br inside paragraphs arrives as a "\n" span; splitSpansOnNewline cuts
// paragraphs on it.
func splitSpansOnNewline(spans []Span) [][]Span {
	parts := [][]Span{{}}
	for _, s := range spans {
		if s.Text == "\n" {
			parts = append(parts, []Span{})
			continue
		}
		last := &parts[len(parts)-1]
		*last = append(*last, s)
	}
	return parts
}

func trimSpans(spans []Span) []Span {
	if len(spans) == 0 {
		return nil
	}
	spans[0].Text = strings.TrimLeft(spans[0].Text, " ")
	spans[len(spans)-1].Text = strings.TrimRight(spans[len(spans)-1].Text, " ")
	return mergeSpans(spans)
}

func withStyle(spans []Span, st SpanStyle) []Span {
	out := make([]Span, len(spans))
	for i, s := range spans {
		out[i] = Span{Text: s.Text, Style: s.Style | st, Link: s.Link}
	}
	return out
}

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// isInlineTag reports elements that carry text flow rather than blocks.
var isInlineTag = map[string]bool{
	"a": true, "abbr": true, "b": true, "bdi": true, "bdo": true,
	"big": true, "cite": true, "code": true, "data": true, "del": true,
	"em": true, "font": true, "i": true, "ins": true, "kbd": true,
	"mark": true, "q": true, "s": true, "samp": true, "small": true,
	"span": true, "strike": true, "strong": true, "sub": true,
	"sup": true, "time": true, "tt": true, "u": true, "var": true,
	"wbr": true,
}
