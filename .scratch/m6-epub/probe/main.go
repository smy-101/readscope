package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	billdaws "github.com/billdaws/epub"
	"github.com/ABFCode/Spine"
)

// ---- fixture builders ----

type file struct {
	name string
	body string
}

func writeEpub(path string, files []file) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.Create(f.name)
		if err != nil {
			panic(err)
		}
		w.Write([]byte(f.body))
	}
	zw.Close()
	os.WriteFile(path, buf.Bytes(), 0644)
}

const chapter1 = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Ch1</title></head>
<body><h1 id="c1">Chapter One</h1><p>It was the best of times.</p></body></html>`

const chapter2 = `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Ch2</title></head>
<body><h1>Chapter Two</h1><p>It was the worst of times.</p></body></html>`

const containerXML = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
<rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`

const ncx = `<?xml version="1.0" encoding="UTF-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
<head/><docTitle><text>Test Book</text></docTitle>
<navMap><navPoint id="n1" playOrder="1"><navLabel><text>Ch1</text></navLabel><content src="chapter1.xhtml"/></navPoint></navMap>
</ncx>`

const navDoc = `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>TOC</title></head><body>
<nav epub:type="toc"><ol><li><a href="chapter1.xhtml">Ch1</a></li></ol></nav>
</body></html>`

func opf2(version, ncxItem string) string {
	ncxMan := ""
	if ncxItem != "" {
		ncxMan = `<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>`
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="` + version + `" unique-identifier="BookId">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:title>Test Book</dc:title><dc:creator>Ann Author</dc:creator>
<dc:language>en</dc:language><dc:identifier id="BookId">urn:uuid:1234</dc:identifier>
</metadata>
<manifest>` + ncxMan + `
<item id="c1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
<item id="c2" href="chapter2.xhtml" media-type="application/xhtml+xml"/>
</manifest>
<spine toc="ncx">` + func() string {
		if ncxItem == "" {
			return ""
		}
		return ""
	}() + `<itemref idref="c1"/><itemref idref="c2"/></spine>
</package>`
}

func opf3(nav bool) string {
	navMan := ""
	if nav {
		navMan = `<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="BookId">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:title>Test Book 3</dc:title><dc:creator>Ann Author</dc:creator>
<dc:language>en</dc:language><dc:identifier id="BookId">urn:uuid:5678</dc:identifier>
<meta property="dcterms:modified">2026-01-01T00:00:00Z</meta>
</metadata>
<manifest>` + navMan + `
<item id="c1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
<item id="c2" href="chapter2.xhtml" media-type="application/xhtml+xml"/>
</manifest>
<spine><itemref idref="c1"/><itemref idref="c2"/></spine>
</package>`
}

const encryptionXML = `<?xml version="1.0" encoding="UTF-8"?>
<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
<EncryptedData xmlns="http://www.w3.org/2001/04/xmlenc#">
<EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/>
<CipherData><CipherReference URI="OEBPS/chapter1.xhtml"/></CipherData>
</EncryptedData>
</encryption>`

// gibberish simulating ADEPT-encrypted chapter bytes (valid zip entry, garbage content)
const encryptedChapter = `AQIDBP/+//7//v8AAAECAwQFBgcICQ==LdECb2Z1c2NhdGVkIGRhdGEgdGhhdCBpcyBub3QgWE1MTC4gUGxhaW50ZXh0IGd1emJhbGsgc2ltdWxhdGluZyBBREVQVC9jb250ZW50LXNjcmFtYmxpbmcgZW5jcnlwdGlvbi4g`

func baseFiles(opf string) []file {
	return []file{
		{"META-INF/container.xml", containerXML},
		{"OEBPS/content.opf", opf},
		{"OEBPS/chapter1.xhtml", chapter1},
		{"OEBPS/chapter2.xhtml", chapter2},
	}
}

func main() {
	dir := os.TempDir()
	mk := func(name string, files []file) string {
		p := filepath.Join(dir, "probe-"+name)
		writeEpub(p, files)
		return p
	}

	good2 := mk("good-epub2.epub", append(baseFiles(opf2("2.0", "ncx")), file{"OEBPS/toc.ncx", ncx}))
	good3 := mk("good-epub3.epub", append(baseFiles(opf3(true)), file{"OEBPS/nav.xhtml", navDoc}))
	noNcx := mk("epub2-no-ncx.epub", baseFiles(opf2("2.0", "")))
	drm := mk("drm-encryption.epub", []file{
		{"META-INF/container.xml", containerXML},
		{"META-INF/encryption.xml", encryptionXML},
		{"OEBPS/content.opf", opf2("2.0", "ncx")},
		{"OEBPS/toc.ncx", ncx},
		{"OEBPS/chapter1.xhtml", encryptedChapter},
		{"OEBPS/chapter2.xhtml", chapter2},
	})
	noVersion := mk("opf-no-version.epub", baseFiles(strings.Replace(opf2("2.0", "ncx"), ` version="2.0"`, "", 1)))
	noVersion = mk("opf-no-version.epub", append(baseFiles(strings.Replace(opf2("2.0", "ncx"), ` version="2.0"`, "", 1)), file{"OEBPS/toc.ncx", ncx}))
	garbageOpf := mk("opf-garbage.epub", append(baseFiles("this is << not xml at all"), file{"OEBPS/toc.ncx", ncx}))
	wrongNsOpf := mk("opf-wrong-ns.epub", append(baseFiles(strings.Replace(opf2("2.0", "ncx"), `xmlns="http://www.idpf.org/2007/opf" `, "", 1)), file{"OEBPS/toc.ncx", ncx}))

	// truncated zip
	good2Bytes, _ := os.ReadFile(good2)
	trunc := filepath.Join(dir, "probe-truncated.epub")
	os.WriteFile(trunc, good2Bytes[:200], 0644)

	// latin1 OPF (declared encoding=iso-8859-1 with a non-ascii title byte)
	latin1 := strings.Replace(opf2("2.0", "ncx"), `<dc:title>Test Book</dc:title>`, "<dc:title>Caf\xe9 Test</dc:title>", 1)
	latin1 = strings.Replace(latin1, `encoding="UTF-8"`, `encoding="iso-8859-1"`, 1)
	latin1Path := mk("opf-latin1.epub", append(baseFiles(latin1), file{"OEBPS/toc.ncx", ncx}))

	cases := []string{good2, good3, noNcx, drm, noVersion, garbageOpf, wrongNsOpf, trunc, latin1Path}

	for _, p := range cases {
		fmt.Printf("\n================ %s ================\n", filepath.Base(p))
		probeBilldaws(p)
		probeSpine(p)
	}
}

func probeBilldaws(path string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("  [billdaws] PANIC:", r)
		}
	}()
	fmt.Println("  --- billdaws/epub ---")
	r, err := billdaws.Open(path)
	if err != nil {
		fmt.Printf("  Open err: %v (%T)\n", err, err)
		if r != nil {
			fmt.Printf("  partial Reader? %#v\n", r)
		}
		return
	}
	defer r.Close()
	fmt.Printf("  Open ok: version=%q title=%q authors=%v lang=%q spine=%d manifest=%d cover=%v\n",
		r.Package.Version, r.Package.Metadata.Title, r.Package.Metadata.Authors, r.Package.Metadata.Language,
		len(r.Package.Spine), len(r.Package.Manifest), r.Package.Cover != nil)
	toc, terr := billdaws.OpenTOC(path)
	if terr != nil {
		fmt.Printf("  OpenTOC err: %v (%T)\n", terr, terr)
	} else {
		fmt.Printf("  OpenTOC ok: %d entries, first=%q\n", len(toc), firstNonEmpty(toc))
	}
	// read first spine chapter raw bytes
	if len(r.Package.Spine) > 0 {
		byID := map[string]billdaws.Item{}
		for _, it := range r.Package.Manifest {
			byID[it.ID] = it
		}
		if it, ok := byID[r.Package.Spine[0].IDRef]; ok {
			b, rerr := r.ReadItem(it)
			if rerr != nil {
				fmt.Printf("  ReadItem err: %v\n", rerr)
			} else {
				preview := string(b)
				if len(preview) > 70 {
					preview = preview[:70]
				}
				fmt.Printf("  ReadItem ok: %d bytes, head=%q\n", len(b), strings.ReplaceAll(preview, "\n", " "))
			}
		}
	}
	v := billdaws.Validate(r.Package)
	fmt.Printf("  Validate: %d violations\n", len(v))
}

func probeSpine(path string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("  [spine] PANIC:", r)
		}
	}()
	fmt.Println("  --- ABFCode/Spine ---")
	b, err := spine.ParseFile(path)
	if b != nil {
		defer b.Close()
	}
	if err != nil {
		fmt.Printf("  ParseFile err: %v\n", err)
		if b == nil {
			return
		}
	}
	fmt.Printf("  book: title=%q authors=%v lang=%q spine=%d manifest=%d toc=%d warnings=%d coverHref=%q\n",
		b.Metadata.Title, b.Metadata.Authors, b.Metadata.Language, len(b.Spine), len(b.Manifest), len(b.TOC), len(b.Warnings), b.Metadata.CoverHref)
	for _, w := range b.Warnings {
		fmt.Printf("    warning[%s] %s (path=%q)\n", w.Code, w.Message, w.Path)
	}
	if len(b.Spine) > 0 {
		rc, oerr := b.OpenResource(b.Spine[0].Href)
		if oerr != nil {
			fmt.Printf("  OpenResource err: %v\n", oerr)
		} else {
			var buf bytes.Buffer
			n, _ := buf.ReadFrom(rc)
			rc.Close()
			preview := buf.String()
			if len(preview) > 70 {
				preview = preview[:70]
			}
			fmt.Printf("  OpenResource ok: %d bytes, head=%q\n", n, strings.ReplaceAll(preview, "\n", " "))
		}
	}
	if cover, cerr := b.Cover(); cerr == nil {
		fmt.Printf("  Cover ok: %d bytes %s\n", len(cover.Bytes), cover.ContentType)
	} else {
		fmt.Printf("  Cover err: %v\n", cerr)
	}
}

func firstNonEmpty(toc []billdaws.NavPoint) string {
	if len(toc) == 0 {
		return ""
	}
	return toc[0].Title
}
