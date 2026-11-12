package zhihu

// clean.go — the zhihu package's 内容前处理 (spec: Implementation
// Decisions): zhihu serves reader-unfriendly HTML — lazy-load
// placeholders instead of image URLs, thumbnail-proxy variants of
// every picture, sticker images, video-box cards, noscript
// duplicates. CleanHTML normalizes one HTML fragment down to the
// plain subset the M1 render pipeline consumes. The pipeline itself
// never changes for a source: this front step is zhihu's, and every
// zhihu HTML body (daily story now, question details and answers in
// ticket 06) rides it before ParseHTML.

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// CleanHTML preprocesses one zhihu HTML fragment:
//
//   - <noscript> wrappers drop whole (their img is a duplicate of the
//     real one above them — the JS-off copy);
//   - an <img>'s URL resolves data-original > data-actualsrc > src,
//     zhimg thumbnail proxy segments (/50/, /80/) strip off, and a
//     resolved data: URL (a lazy-load placeholder) drops the img;
//   - sticker images (data-sticker-id) become their alt text;
//   - a.video-box cards become a bare [视频] link (the page URL from
//     href, or the lens page from data-lens-id);
//   - adjacent <img>s with the same resolved URL merge into one.
//
// Unparseable input passes through untouched — the render pipeline's
// own plain-text fallback then handles it, as it would any feed.
func CleanHTML(src string) string {
	nodes, err := html.ParseFragment(strings.NewReader(src), &html.Node{
		Type: html.ElementNode, Data: "div", DataAtom: atom.Div,
	})
	if err != nil {
		return src
	}
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	cleanChildren(root)
	var b strings.Builder
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&b, c); err != nil {
			return src
		}
	}
	return b.String()
}

// cleanChildren walks one container's children: each child maps to
// nil (dropped), itself (possibly mutated in place), or a
// replacement; the survivors come back as the container's children,
// adjacent same-image imgs merged.
func cleanChildren(n *html.Node) {
	kids := make([]*html.Node, 0, 8)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		kids = append(kids, c)
	}
	for _, k := range kids {
		n.RemoveChild(k)
	}
	outs := mergeAdjacentImages(cleanNodes(kids))
	for _, out := range outs {
		n.AppendChild(out)
	}
}

// cleanNodes maps each child through the rules, dropping the ones
// that map to nil.
func cleanNodes(kids []*html.Node) []*html.Node {
	outs := make([]*html.Node, 0, len(kids))
	for _, k := range kids {
		if out := cleanNode(k); out != nil {
			outs = append(outs, out)
		}
	}
	return outs
}

// cleanNode maps one node through the rules; nil means drop.
func cleanNode(n *html.Node) *html.Node {
	switch n.Type {
	case html.CommentNode:
		return nil
	case html.ElementNode:
	default:
		return n // text and everything else passes
	}
	switch n.Data {
	case "noscript":
		return nil // the JS-off duplicate copy
	case "img":
		return cleanImg(n)
	case "a":
		if hasClass(n, "video-box") {
			return videoLink(n)
		}
	}
	cleanChildren(n) // containers and unknown tags stay transparent
	return n
}

// cleanImg applies the img rules: stickers become their alt text,
// placeholders drop, and a survivor keeps only the resolved src and
// the alt — data-* cruft and lazy classes never reach the pipeline.
func cleanImg(n *html.Node) *html.Node {
	if isSticker(n) {
		if alt := xattr(n, "alt"); alt != "" {
			return &html.Node{Type: html.TextNode, Data: alt}
		}
		return nil
	}
	url := imageURL(n)
	if url == "" {
		return nil // nothing resolvable: a lazy placeholder, not a picture
	}
	img := &html.Node{Type: html.ElementNode, Data: "img", DataAtom: atom.Img}
	img.Attr = []html.Attribute{{Key: "src", Val: url}}
	if alt := xattr(n, "alt"); alt != "" {
		img.Attr = append(img.Attr, html.Attribute{Key: "alt", Val: alt})
	}
	return img
}

// isSticker: zhihu stickers carry data-sticker-id (or a sticker
// class) — decorative reaction images whose alt text IS the content.
func isSticker(n *html.Node) bool {
	if xattr(n, "data-sticker-id") != "" {
		return true
	}
	return hasClass(n, "sticker")
}

// videoLink replaces an a.video-box card with a bare link whose text
// is [视频]: href when the payload carries one, else the lens page
// the data-lens-id addresses.
func videoLink(n *html.Node) *html.Node {
	url := xattr(n, "href")
	if url == "" {
		if id := xattr(n, "data-lens-id"); id != "" {
			url = "https://www.zhihu.com/lens/" + id
		}
	}
	if url == "" {
		return nil // a video card with no address at all: nothing to open
	}
	a := &html.Node{Type: html.ElementNode, Data: "a", DataAtom: atom.A}
	a.Attr = []html.Attribute{{Key: "href", Val: url}}
	a.AppendChild(&html.Node{Type: html.TextNode, Data: "[视频]"})
	return a
}

// imageURL resolves an img's picture: data-original first (the
// full-size original the lazy loader would swap in), data-actualsrc
// next, src last — skipping data: placeholders (lazy-load stand-ins,
// never the real bytes) and stripping zhimg's thumbnail proxy.
func imageURL(n *html.Node) string {
	for _, key := range []string{"data-original", "data-actualsrc", "src"} {
		v := strings.TrimSpace(xattr(n, key))
		if v == "" || strings.HasPrefix(v, "data:") {
			continue
		}
		return stripThumbProxy(v)
	}
	return ""
}

// zhimgThumbRE matches zhimg's thumbnail proxy spellings — the /50/
// (and /80/) size segment pic*.zhimg.com inserts between host and
// picture path. Only zhimg hosts: other sites' /50/ paths are theirs.
var zhimgThumbRE = regexp.MustCompile(`(?i)^(https?://pic\d?\.zhimg\.com)/(50|80)/`)

func stripThumbProxy(u string) string {
	return zhimgThumbRE.ReplaceAllString(u, "${1}/")
}

// mergeAdjacentImages collapses runs of the same resolved image —
// whitespace-only text between two imgs counts as adjacency (zhihu's
// source formatting puts newlines there); anything real breaks the
// run. Whitespace held back during a would-be merge flushes back
// when the run doesn't pan out (an img's trailing space before real
// text is legitimate spacing).
func mergeAdjacentImages(nodes []*html.Node) []*html.Node {
	out := make([]*html.Node, 0, len(nodes))
	var pending []*html.Node // whitespace held back while a merge may follow
	var lastImg *html.Node
	for _, n := range nodes {
		if isWhitespaceText(n) {
			pending = append(pending, n)
			continue
		}
		if n.Type == html.ElementNode && n.Data == "img" && lastImg != nil &&
			xattr(n, "src") == xattr(lastImg, "src") {
			pending = nil // the duplicate swallows the whitespace with it
			continue
		}
		out = append(out, pending...)
		pending = nil
		if n.Type == html.ElementNode && n.Data == "img" {
			lastImg = n
		} else {
			lastImg = nil
		}
		out = append(out, n)
	}
	return append(out, pending...)
}

// isWhitespaceText reports a text node that carries nothing but
// inter-element formatting.
func isWhitespaceText(n *html.Node) bool {
	return n.Type == html.TextNode && strings.TrimSpace(n.Data) == ""
}

// hasClass reports whether one of n's classes is exactly name.
func hasClass(n *html.Node, name string) bool {
	for _, c := range strings.Fields(xattr(n, "class")) {
		if c == name {
			return true
		}
	}
	return false
}

// xattr is the local attr getter (render's stays private to it).
func xattr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}
