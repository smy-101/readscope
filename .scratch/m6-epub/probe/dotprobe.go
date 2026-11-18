package main

import (
	"fmt"
	"archive/zip"
	"bytes"
	billdaws "github.com/billdaws/epub"
	"github.com/ABFCode/Spine"
	"os"
)

const containerXML = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
<rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`

// note href="./chapter1.xhtml" — legal relative ref, common in real books
const opfDot = `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="B">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Dot</dc:title>
<dc:language>en</dc:language><dc:identifier id="B">u</dc:identifier></metadata>
<manifest><item id="c1" href="./chapter1.xhtml" media-type="application/xhtml+xml" properties="nav"/></manifest>
<spine><itemref idref="c1"/></spine></package>`

const ch = `<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml"><body><p>hi</p></body></html>`

func main() {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range [][2]string{{"META-INF/container.xml", containerXML}, {"OEBPS/content.opf", opfDot}, {"OEBPS/chapter1.xhtml", ch}} {
		w, _ := zw.Create(f[0])
		w.Write([]byte(f[1]))
	}
	zw.Close()
	p := "/tmp/probe-dot.epub"
	os.WriteFile(p, buf.Bytes(), 0644)

	r, err := billdaws.Open(p)
	if err != nil { fmt.Println("billdaws.Open err:", err); return }
	defer r.Close()
	fmt.Printf("billdaws: spine=%d manifest=%d href=%q\n", len(r.Package.Spine), len(r.Package.Manifest), r.Package.Manifest[0].Href)
	b, rerr := r.ReadItem(r.Package.Manifest[0])
	fmt.Println("billdaws.ReadItem:", len(b), "err:", rerr)

	sb, err := spine.ParseFile(p)
	if err != nil { fmt.Println("spine err:", err); return }
	defer sb.Close()
	fmt.Printf("spine: spine=%d href=%q warn=%d\n", len(sb.Spine), sb.Spine[0].Href, len(sb.Warnings))
	rc, oerr := sb.OpenResource(sb.Spine[0].Href)
	if oerr != nil { fmt.Println("spine.OpenResource err:", oerr); return }
	n := 0; tmp := make([]byte, 64); n, _ = rc.Read(tmp); rc.Close()
	fmt.Println("spine.OpenResource bytes:", n)
}
