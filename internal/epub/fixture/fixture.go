// Package fixture builds synthetic EPUB archives in memory and writes
// them to temp directories, so the epub layer's tests depend on neither
// binary blobs in git (every sample is reviewable Go) nor the network.
// Every sample is deterministic: same builder call, same bytes.
package fixture

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixedModTime keeps generated zips byte-identical across runs.
var fixedModTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// File is one zip entry. Data holds the entry's bytes; entries needing a
// hand-built header (corrupt data, encryption flag) use raw instead.
type File struct {
	Name string
	Data string
	raw  *rawEntry
}

type rawEntry struct {
	flags       uint16 // zip general-purpose flag bits written verbatim
	method      uint16
	data        string
	uncompSize  uint64
	description string
}

// Write packs files into an .epub at a fresh temp path and returns that
// path. The caller gets a t.Cleanup-free life: t.TempDir is reclaimed
// automatically.
func Write(t *testing.T, files []File) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.epub")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("fixture: create: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for _, file := range files {
		if err := writeEntry(zw, file); err != nil {
			t.Fatalf("fixture: write %s: %v", file.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("fixture: close zip: %v", err)
	}
	return path
}

func writeEntry(zw *zip.Writer, file File) error {
	if file.raw != nil {
		fh := &zip.FileHeader{
			Name:               file.Name,
			Flags:              file.raw.flags,
			Method:             file.raw.method,
			Modified:           fixedModTime,
			CompressedSize64:   uint64(len(file.raw.data)),
			UncompressedSize64: file.raw.uncompSize,
		}
		w, err := zw.CreateRaw(fh)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, file.raw.data)
		return err
	}

	fh := &zip.FileHeader{Name: file.Name, Method: zip.Deflate, Modified: fixedModTime}
	w, err := zw.CreateHeader(fh)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, file.Data)
	return err
}

// Corrupt returns an entry whose stored bytes are not a valid deflate
// stream: the zip header reads fine, but reading the content fails
// (flate corrupt input). This is how a truncated or damaged chapter
// looks from archive/zip.
func Corrupt(name string) File {
	return File{Name: name, raw: &rawEntry{
		method:     zip.Deflate,
		data:       "\x0d\xc3\x5c\x77\x1a\x0b\xff garbage not deflate \x99\x88\x77",
		uncompSize: 4096,
	}}
}

// Encrypted returns an entry carrying the zip encryption flag with
// garbage data. Go's archive/zip ignores the flag, so reads fail exactly
// as they would for a real password-protected entry: open succeeds,
// content read errors.
func Encrypted(name string) File {
	return File{Name: name, raw: &rawEntry{
		flags:      0x1,
		method:     zip.Deflate,
		data:       "\x0d\xc3\x5c\x77\x1a\x0b pseudo-ciphertext \x99\x88\x77",
		uncompSize: 4096,
	}}
}

// without returns files minus the entries named names — the standard way
// to derive a broken-book sample from a healthy one.
func without(files []File, names ...string) []File {
	var out []File
	for _, f := range files {
		drop := false
		for _, name := range names {
			if f.Name == name {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, f)
		}
	}
	return out
}

// ValidEPUB2 is a canonical OEBPS-layout EPUB2: container.xml, NCX toc,
// three chapters under text/ (ch3 marked non-linear), one cover image.
// Metadata and TOC are non-trivial on purpose: CJK text, two authors,
// two-level navMap.
func ValidEPUB2() []File {
	return []File{
		{Name: "mimetype", Data: "application/epub+zip"},
		{Name: "META-INF/container.xml", Data: `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`},
		{Name: "OEBPS/content.opf", Data: `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="uid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>合成书 · EPUB2</dc:title>
    <dc:creator>作者甲</dc:creator>
    <dc:creator>作者乙</dc:creator>
    <dc:language>zh-CN</dc:language>
    <dc:identifier id="uid">urn:uuid:11111111-2222-3333-4444-555555555555</dc:identifier>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="ch1" href="text/ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="ch2" href="text/ch2.xhtml" media-type="application/xhtml+xml"/>
    <item id="ch3" href="text/ch3.xhtml" media-type="application/xhtml+xml"/>
    <item id="cover" href="images/cover.jpg" media-type="image/jpeg"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="ch1"/>
    <itemref idref="ch2"/>
    <itemref idref="ch3" linear="no"/>
  </spine>
</package>`},
		{Name: "OEBPS/toc.ncx", Data: `<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <navMap>
    <navPoint id="p1" playOrder="1">
      <navLabel><text>第一章 起点</text></navLabel>
      <content src="text/ch1.xhtml"/>
      <navPoint id="p1-1" playOrder="2">
        <navLabel><text>第二章 续章</text></navLabel>
        <content src="text/ch2.xhtml"/>
      </navPoint>
    </navPoint>
    <navPoint id="p3" playOrder="3">
      <navLabel><text>附录 非线性</text></navLabel>
      <content src="text/ch3.xhtml"/>
    </navPoint>
  </navMap>
</ncx>`},
		{Name: "OEBPS/text/ch1.xhtml", Data: chapterXHTML("第一章 起点", "<p>开篇。中文折行与禁则由渲染管线负责。</p>")},
		{Name: "OEBPS/text/ch2.xhtml", Data: chapterXHTML("第二章 续章", "<pre><code>func main() {}</code></pre><ul><li>一<ul><li>嵌套</li></ul></li></ul>")},
		{Name: "OEBPS/text/ch3.xhtml", Data: chapterXHTML("附录 非线性", "<p><img src=\"../images/cover.jpg\" alt=\"封面图\"/></p>")},
		{Name: "OEBPS/images/cover.jpg", Data: "\xff\xd8\xff\xe0 fake jpeg bytes"},
	}
}

// ValidEPUB2 exercises dirty href spellings against a root-level OPF:
// ./ prefixes, sub/../ dot-dot walks, %XX escapes, TOC #fragments. Every
// href must resolve onto a real zip entry.
func HrefMatrix() []File {
	mk := func(name, href string) File {
		return File{Name: name, Data: chapterXHTML(name, "<p>"+name+"</p>")}
	}
	return []File{
		{Name: "mimetype", Data: "application/epub+zip"},
		{Name: "META-INF/container.xml", Data: `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`},
		{Name: "content.opf", Data: `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>脏 href 矩阵书</dc:title>
    <dc:creator>fixture</dc:creator>
    <dc:language>zh-CN</dc:language>
  </metadata>
  <manifest>
    <item id="ch1" href="./text/ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="ch2" href="text/sub/../ch2.xhtml" media-type="application/xhtml+xml"/>
    <item id="ch3" href="text/my%20ch3.xhtml" media-type="application/xhtml+xml"/>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="ch1"/>
    <itemref idref="ch2"/>
    <itemref idref="ch3"/>
  </spine>
</package>`},
		{Name: "toc.ncx", Data: `<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <navMap>
    <navPoint id="p1"><navLabel><text>带锚点</text></navLabel><content src="text/ch2.xhtml#section-2"/></navPoint>
  </navMap>
</ncx>`},
		mk("text/ch1.xhtml", "ch1"),
		mk("text/ch2.xhtml", "ch2"),
		mk("text/my ch3.xhtml", "ch3"),
	}
}

// ValidEPUB3 is an EPUB3 that also carries an NCX (compat book): the
// nav document must win. Its first nav is landmarks (the mis-selection
// trap), the toc nav nests two levels and one entry carries a
// #fragment. NCX labels deliberately differ from nav labels so the
// wrong source is loud.
func ValidEPUB3() []File {
	mk := func(name string) File {
		return File{Name: name, Data: chapterXHTML(name, "<p>"+name+"</p>")}
	}
	return []File{
		{Name: "mimetype", Data: "application/epub+zip"},
		{Name: "META-INF/container.xml", Data: `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`},
		{Name: "OEBPS/content.opf", Data: `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>合成书 · EPUB3</dc:title>
    <dc:creator>作者甲</dc:creator>
    <dc:language>zh-CN</dc:language>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="ch1" href="text/ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="ch2" href="text/ch2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="ch1"/>
    <itemref idref="ch2"/>
  </spine>
</package>`},
		{Name: "OEBPS/nav.xhtml", Data: `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>导航</title></head>
<body>
  <nav epub:type="landmarks" hidden="">
    <ol><li><a epub:type="bodymatter" href="text/ch1.xhtml">正文起点</a></li></ol>
  </nav>
  <nav epub:type="toc">
    <ol>
      <li><a href="text/ch1.xhtml">第一章 起航</a>
        <ol><li><a href="text/ch2.xhtml#sec-1">1.1 小节</a></li></ol>
      </li>
    </ol>
  </nav>
</body>
</html>`},
		{Name: "OEBPS/toc.ncx", Data: `<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <navMap>
    <navPoint id="p1"><navLabel><text>NCX 不该被选中</text></navLabel><content src="text/ch1.xhtml"/></navPoint>
  </navMap>
</ncx>`},
		mk("OEBPS/text/ch1.xhtml"),
		mk("OEBPS/text/ch2.xhtml"),
	}
}

// Latin1OPF declares encoding="ISO-8859-1" and carries a high byte
// (0xE9 é) in its title: the OPF decoder must convert it to UTF-8
// instead of failing the whole package parse (the probe's Café case).
func Latin1OPF() []File {
	opf := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>\n" +
		"<package xmlns=\"http://www.idpf.org/2007/opf\" version=\"2.0\">\n" +
		"  <metadata xmlns:dc=\"http://purl.org/dc/elements/1.1/\">\n" +
		"    <dc:title>Caf\xe9 Test</dc:title>\n" +
		"    <dc:creator>Fixture \xe9</dc:creator>\n" +
		"    <dc:language>fr</dc:language>\n" +
		"  </metadata>\n" +
		"  <manifest>\n" +
		"    <item id=\"ch1\" href=\"ch1.xhtml\" media-type=\"application/xhtml+xml\"/>\n" +
		"  </manifest>\n" +
		"  <spine><itemref idref=\"ch1\"/></spine>\n" +
		"</package>"
	return []File{
		{Name: "mimetype", Data: "application/epub+zip"},
		{Name: "META-INF/container.xml", Data: `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`},
		{Name: "content.opf", Data: opf},
		{Name: "ch1.xhtml", Data: chapterXHTML("Caf\xe9", "<p>Bonjour</p>")},
	}
}

// chapterXHTML wraps body into a minimal XHTML 1.1 chapter document.
func chapterXHTML(title, body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN" "http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd">
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>` + title + `</title></head>
<body><h1>` + title + `</h1>` + body + `</body>
</html>`
}
