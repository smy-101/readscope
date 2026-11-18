package epub_test

import (
	"testing"

	"github.com/smy-101/readscope/internal/epub"
	"github.com/smy-101/readscope/internal/epub/fixture"
)

// open is the helper every test goes through: the whole package is
// tested at its public seam — Open, ReadItem, exported Book fields.
func open(t *testing.T, files []fixture.File) *epub.Book {
	t.Helper()
	book, err := epub.Open(fixture.Write(t, files))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { book.Close() })
	return book
}

func TestOpenCanonicalEPUB2(t *testing.T) {
	book := open(t, fixture.ValidEPUB2())

	if got, want := book.Title, "合成书 · EPUB2"; got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}
	if got, want := book.Authors, []string{"作者甲", "作者乙"}; !equalStrings(got, want) {
		t.Errorf("Authors = %v, want %v", got, want)
	}
	if got, want := book.Language, "zh-CN"; got != want {
		t.Errorf("Language = %q, want %q", got, want)
	}

	// Spine: declared order, hrefs joined onto the OPF's directory,
	// linear="no" carried through.
	wantSpine := []epub.SpineItem{
		{IDRef: "ch1", Href: "OEBPS/text/ch1.xhtml", Linear: true},
		{IDRef: "ch2", Href: "OEBPS/text/ch2.xhtml", Linear: true},
		{IDRef: "ch3", Href: "OEBPS/text/ch3.xhtml", Linear: false},
	}
	if len(book.Spine) != len(wantSpine) {
		t.Fatalf("Spine length = %d, want %d (%+v)", len(book.Spine), len(wantSpine), book.Spine)
	}
	for i, want := range wantSpine {
		if book.Spine[i] != want {
			t.Errorf("Spine[%d] = %+v, want %+v", i, book.Spine[i], want)
		}
	}

	// Manifest: every declared item, id → zip path + media type.
	item, ok := book.ManifestByID("cover")
	if !ok {
		t.Fatalf("ManifestByID(cover) not found; manifest = %+v", book.Manifest)
	}
	if item.Href != "OEBPS/images/cover.jpg" || item.MediaType != "image/jpeg" {
		t.Errorf("manifest[cover] = %+v", item)
	}

	if got, want := len(book.Warnings), 0; got != want {
		t.Errorf("Warnings = %+v, want none", book.Warnings)
	}
}

func TestOpenParsesNCXTOCWithNesting(t *testing.T) {
	book := open(t, fixture.ValidEPUB2())

	want := []epub.TOCItem{
		{
			Title: "第一章 起点", Href: "OEBPS/text/ch1.xhtml",
			Children: []epub.TOCItem{{Title: "第二章 续章", Href: "OEBPS/text/ch2.xhtml"}},
		},
		{Title: "附录 非线性", Href: "OEBPS/text/ch3.xhtml"},
	}
	assertTOC(t, book.TOC, want)
}

func assertTOC(t *testing.T, got, want []epub.TOCItem) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("TOC length = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Title != w.Title || got[i].Href != w.Href {
			t.Errorf("TOC[%d] = {%q %q}, want {%q %q}", i, got[i].Title, got[i].Href, w.Title, w.Href)
		}
		assertTOC(t, got[i].Children, w.Children)
	}
}

func TestHrefNormalizationMatrix(t *testing.T) {
	book := open(t, fixture.HrefMatrix())

	// Spine hrefs: idref → manifest join with ./, sub/../ and %XX all
	// resolved onto the real entry names.
	want := []string{"text/ch1.xhtml", "text/ch2.xhtml", "text/my ch3.xhtml"}
	for i, w := range want {
		if got := book.Spine[i].Href; got != w {
			t.Errorf("Spine[%d].Href = %q, want %q", i, got, w)
		}
		if _, err := book.ReadItem(book.Spine[i].Href); err != nil {
			t.Errorf("ReadItem(Spine[%d] = %q): %v", i, book.Spine[i].Href, err)
		}
	}

	// TOC src carried a #fragment: stripped, chapter path kept.
	if len(book.TOC) != 1 || book.TOC[0].Href != "text/ch2.xhtml" {
		t.Errorf("TOC = %+v, want one item pointing at text/ch2.xhtml", book.TOC)
	}

	// Messy caller spellings land on entries too.
	for _, href := range []string{
		"./text/../text/ch1.xhtml", // dot-walk from a rooted spelling
		"text/my%20ch3.xhtml",      // percent-escaped space
		"text/ch2.xhtml#anywhere",  // fragment from a TOC-style src
	} {
		if _, err := book.ReadItem(href); err != nil {
			t.Errorf("ReadItem(%q): %v", href, err)
		}
	}
}

func TestOpenEPUB3NavTOCWinsOverNCX(t *testing.T) {
	book := open(t, fixture.ValidEPUB3())

	// The epub:type=toc nav wins over NCX; the landmarks nav that comes
	// first in the document is not mis-selected; nesting survives and
	// the #fragment is stripped.
	want := []epub.TOCItem{
		{
			Title: "第一章 起航", Href: "OEBPS/text/ch1.xhtml",
			Children: []epub.TOCItem{{Title: "1.1 小节", Href: "OEBPS/text/ch2.xhtml"}},
		},
	}
	assertTOC(t, book.TOC, want)
	if got, want := len(book.Warnings), 0; got != want {
		t.Errorf("Warnings = %+v, want none", book.Warnings)
	}
}

func TestOpenLatin1OPFMetadata(t *testing.T) {
	book := open(t, fixture.Latin1OPF())

	if got, want := book.Title, "Caf\u00e9 Test"; got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}
	if got, want := book.Authors, []string{"Fixture \u00e9"}; !equalStrings(got, want) {
		t.Errorf("Authors = %q, want %q", got, want)
	}
	if got, want := len(book.Warnings), 1; got != want || book.Warnings[0].Code != epub.CodeMissingTOC {
		t.Errorf("Warnings = %+v, want exactly [missing-toc] (no NCX, no nav)", book.Warnings)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
