// reader.go: the EPUB reader (M6 ticket 03) — an independent
// full-screen over the bookshelf (ADR #11): left TOC sidebar, right
// viewport over the continuous flow. This file holds the production
// feeder (spine chapters through the parse layer and the shared
// pipeline); the screen state, key pump and view land with it as the
// slices accrete.
package ui

import (
	"path"
	"strings"

	"github.com/smy-101/readscope/internal/epub"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/render"
)

// epubFeeder is the flow engine's production feeder: spine chapters
// parsed on demand and laid out by the engine. The chapter title
// chain (the ticket's fallback order): the first TOC item targeting
// the chapter → the chapter's first heading → the entry's base name.
// Degradation contract: a read failure or a textless chapter (SVG
// page) answers with a placeholder paragraph — never zero blocks,
// never an error (the engine's stitcher contract; the reader's status
// bar carries the failure text separately).
type epubFeeder struct {
	bk     *epub.Book
	cat    *i18n.Catalog
	titles map[int]string // spine idx → TOC title, first match wins
}

func newEpubFeeder(bk *epub.Book, cat *i18n.Catalog) *epubFeeder {
	f := &epubFeeder{bk: bk, cat: cat, titles: make(map[int]string, len(bk.TOC))}
	hrefIdx := make(map[string]int, len(bk.Spine))
	for i, s := range bk.Spine {
		if _, ok := hrefIdx[s.Href]; !ok {
			hrefIdx[s.Href] = i
		}
	}
	var walk func(items []epub.TOCItem)
	walk = func(items []epub.TOCItem) {
		for _, it := range items {
			if idx, ok := hrefIdx[it.Href]; ok && f.titles[idx] == "" {
				f.titles[idx] = it.Title
			}
			walk(it.Children)
		}
	}
	walk(bk.TOC)
	return f
}

func (f *epubFeeder) Len() int { return len(f.bk.Spine) }

// Chapter is the feeder contract: a non-empty content document plus
// the chapter's display title.
func (f *epubFeeder) Chapter(idx int) (*render.Document, string) {
	href := f.bk.Spine[idx].Href
	doc, err := f.chapterDoc(idx)
	switch {
	case err != nil:
		return f.placeholderDoc(f.cat.Get("reader.ch_err", err)), fileTitle(href)
	case len(doc.Blocks) == 0:
		return f.placeholderDoc(f.cat.Get("reader.no_text")), f.titles[idx]
	}
	if title := f.titles[idx]; title != "" {
		return doc, title
	}
	if title := firstHeadingTitle(doc); title != "" {
		return doc, title
	}
	return doc, fileTitle(href)
}

// title is the chapter's display title on its own (the TOC sidebar's
// current-chapter label rides it) — the same chain Chapter applies.
func (f *epubFeeder) title(idx int) string {
	if title := f.titles[idx]; title != "" {
		return title
	}
	href := f.bk.Spine[idx].Href
	if doc, err := f.chapterDoc(idx); err == nil {
		if title := firstHeadingTitle(doc); title != "" {
			return title
		}
	}
	return fileTitle(href)
}

// chapterDoc reads and parses the spine chapter's document.
func (f *epubFeeder) chapterDoc(idx int) (*render.Document, error) {
	data, err := f.bk.ReadItem(f.bk.Spine[idx].Href)
	if err != nil {
		return nil, err
	}
	return render.ParseHTML(string(data))
}

// placeholderDoc is the degraded chapter: one dim paragraph carrying
// the verdict (no-text or the read failure).
func (f *epubFeeder) placeholderDoc(text string) *render.Document {
	return &render.Document{Blocks: []render.Block{render.Para{Spans: []render.Span{
		{Text: text, Style: render.SpanDim},
	}}}}
}

// firstHeadingTitle is the chapter's first heading's text ("" when
// none) — the middle link of the title chain.
func firstHeadingTitle(doc *render.Document) string {
	for _, b := range doc.Blocks {
		if h, ok := b.(render.Heading); ok {
			if t := strings.TrimSpace(headingPlainText(h)); t != "" {
				return t
			}
		}
	}
	return ""
}

// fileTitle is the chain's last link: the entry's base name without
// extension.
func fileTitle(href string) string {
	return strings.TrimSuffix(path.Base(href), path.Ext(href))
}
