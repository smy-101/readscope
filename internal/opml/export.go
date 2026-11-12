package opml

import (
	"bytes"
	"encoding/xml"
	"sort"

	"github.com/smy-101/readscope/internal/core"
)

// exportHead is the fixed OPML head: a title readers display and the
// version marker. dateCreated is deliberately omitted — exports stay
// byte-stable, which keeps the roundtrip contract and diffs honest.
type exportHead struct {
	Title string `xml:"title"`
}

type exportOutline struct {
	XMLName xml.Name `xml:"outline"`
	Text    string   `xml:"text,attr"`
	Type    string   `xml:"type,attr"`
	XMLURL  string   `xml:"xmlUrl,attr"`
}

type exportBody struct {
	Outlines []exportOutline `xml:"outline"`
}

type exportDocument struct {
	XMLName xml.Name   `xml:"opml"`
	Version string     `xml:"version,attr"`
	Head    exportHead `xml:"head"`
	Body    exportBody `xml:"body"`
}

// Encode writes the feeds as an OPML 2.0 document another reader can
// import: one flat <outline type="rss"> per feed, text = label (key
// when the label is empty), xmlUrl = address, sorted by key so the
// output is stable regardless of map iteration order.
func Encode(feeds map[string]core.RSSFeed) []byte {
	keys := make([]string, 0, len(feeds))
	for k := range feeds {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	doc := exportDocument{
		Version: "2.0",
		Head:    exportHead{Title: "readscope subscriptions"},
	}
	for _, k := range keys {
		f := feeds[k]
		text := f.Label
		if text == "" {
			text = k
		}
		doc.Body.Outlines = append(doc.Body.Outlines, exportOutline{
			Text: text, Type: "rss", XMLURL: f.URL,
		})
	}

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ") // root at column 0, children indented — the shape readers emit
	if err := enc.Encode(doc); err != nil {
		panic("opml: encode: " + err.Error()) // marshal of plain strings cannot fail
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}
