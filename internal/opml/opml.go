// Package opml is the subscription interop layer: it parses the OPML
// documents mainstream readers export (folder-nested outlines, in
// document order) into flat feed lists, merges imports into
// sources.toml without duplicates, and encodes exports other readers
// accept. Import and export are pure data operations; the CLI owns
// files and localized reporting.
package opml

import (
	"encoding/xml"
	"fmt"
)

// Feed is one subscription extracted from an OPML document: the
// display string (text, falling back to title) and the feed address.
// Title may be empty — the import merge names such feeds after their
// host and records a warning.
type Feed struct {
	Title string
	URL   string
}

// node mirrors one <outline> at any depth: an outline is a feed when
// it carries xmlUrl, a folder otherwise (folders are recursed, feeds
// may nest inside folders of folders — real exports are not shallow).
type node struct {
	XMLName  xml.Name `xml:"outline"`
	Text     string   `xml:"text,attr"`
	Title    string   `xml:"title,attr"`
	XMLURL   string   `xml:"xmlUrl,attr"`
	Outlines []node   `xml:"outline"`
}

// document is the OPML skeleton; only body matters for subscriptions.
type document struct {
	XMLName xml.Name `xml:"opml"`
	Body    struct {
		Outlines []node `xml:"outline"`
	} `xml:"body"`
}

// Parse decodes an OPML document into its feeds in document order,
// flattening folder nesting. Outlines without xmlUrl (folders, empty
// categories) are skipped. Malformed XML — or a root that is not
// <opml> — is an error naming the format, so the caller can report it
// before touching sources.toml.
func Parse(data []byte) ([]Feed, error) {
	var doc document
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse opml: %w", err)
	}
	feeds := make([]Feed, 0, len(doc.Body.Outlines))
	var walk func(nodes []node)
	walk = func(nodes []node) {
		for _, n := range nodes {
			if n.XMLURL != "" {
				title := n.Text
				if title == "" {
					title = n.Title
				}
				feeds = append(feeds, Feed{Title: title, URL: n.XMLURL})
			}
			walk(n.Outlines) // feeds and folders can coexist on one node
		}
	}
	walk(doc.Body.Outlines)
	return feeds, nil
}
