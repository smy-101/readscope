package store

import (
	"strings"

	"golang.org/x/net/html"
)

// parseHTMLText walks an HTML fragment/document and returns its text
// nodes joined with spaces (script/style skipped). Separators keep
// adjacent blocks from fusing into one token-soup word.
func parseHTMLText(s string) (string, error) {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		if n.Type == html.TextNode {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return b.String(), nil
}

// collapseSpace squeezes whitespace runs (incl. newlines) to single
// spaces and trims the ends — FTS rows stay tidy and small.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
