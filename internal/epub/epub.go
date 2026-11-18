// Package epub is readscope's self-written EPUB parsing layer (ADR #17):
// zip + OPF + NCX/Nav TOC down to raw item bytes, feeding the existing
// render pipeline. It is deliberately tolerant — the only hard failures
// are a corrupt zip and real DRM; every lesser defect (missing
// container, malformed OPF, missing spine or TOC) still opens the book,
// degraded, with a typed warning the bookshelf and doctor can consume.
// Behavior follows the Spine semantics and readium's DRM criteria (see
// .scratch/m6-epub/research-epub-libs.md); no third-party code is used.
package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"golang.org/x/net/html"
)

// Hard failures: the only conditions under which Open refuses the book.
var (
	// ErrInvalidZip wraps archive/zip's verdict that the file is not a
	// readable zip archive at all.
	ErrInvalidZip = errors.New("epub: not a valid zip archive")
	// ErrDRMUnsupported means encryption.xml declares an encryption
	// algorithm outside the font-obfuscation whitelist: the book is DRM'd
	// and readscope will not silently serve ciphertext.
	ErrDRMUnsupported = errors.New("epub: DRM not supported")
)

// fontObfuscationAlgorithms are the encryption schemes readscope lets
// through: both obfuscate embedded fonts, not content (readium's split).
var fontObfuscationAlgorithms = map[string]bool{
	"http://www.idpf.org/2008/obfuscation": true,
	"http://www.adobe.com/pdf/enc#RC":      true,
}

// WarningCode identifies a degradation Open recovered from. Codes are
// stable strings so the bookshelf and doctor can branch on them.
type WarningCode string

const (
	CodeMissingContainer WarningCode = "missing-container" // container.xml absent; OPF found by zip scan
	CodeMissingOPF       WarningCode = "missing-opf"       // no OPF anywhere; structure inferred from the zip
	CodeMalformedOPF     WarningCode = "malformed-opf"     // OPF unparseable; structure inferred from the zip
	CodeMissingSpine     WarningCode = "missing-spine"     // spine absent; reading order inferred from the manifest
	CodeMissingTOC       WarningCode = "missing-toc"       // no NCX, no nav; TOC generated from chapter titles
)

// Warning is one typed degradation: what was wrong and how Open coped.
type Warning struct {
	Code    WarningCode
	Message string
}

// SpineItem is one reading-order entry with the idref resolved onto the
// manifest item's zip path. Linear=false marks itemref linear="no".
type SpineItem struct {
	IDRef  string
	Href   string
	Linear bool
}

// ManifestItem is one declared resource; Href is the normalized zip
// path (OPF-relative join, %XX decoded, fragment stripped).
type ManifestItem struct {
	ID         string
	Href       string
	MediaType  string
	Properties string
}

// TOCItem is one table-of-contents node; Href is the chapter's zip path
// (jump target is the chapter head, so anchor fragments are dropped).
type TOCItem struct {
	Title    string
	Href     string
	Children []TOCItem
}

// Book is a parsed, opened EPUB. The zero value is not usable; call
// Open. Warnings lists every degradation recovered from, in the order
// they were encountered.
type Book struct {
	Title     string
	Authors   []string
	Language  string
	Spine     []SpineItem
	Manifest  []ManifestItem
	TOC       []TOCItem
	Warnings  []Warning
	zr        *zip.ReadCloser
	files     map[string]*zip.File // normalized entry name → file
	opfDir    string               // directory of the OPF inside the zip ("" for root)
	manifest  map[string]ManifestItem
	hasNavTOC bool // EPUB3 nav document with a usable toc nav
}

// Open parses the EPUB at path. It fails only on a corrupt zip
// (ErrInvalidZip) or unsupported DRM (ErrDRMUnsupported); everything
// else degrades to a partial Book plus warnings.
func Open(path string) (*Book, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidZip, err)
	}
	b := &Book{zr: zr, files: make(map[string]*zip.File, len(zr.File))}
	for _, f := range zr.File {
		name := normalizeZipName(f.Name)
		if name != "" {
			b.files[name] = f
		}
	}

	if err := b.precheckDRM(); err != nil {
		zr.Close()
		return nil, err
	}

	b.parse()
	return b, nil
}

// Close releases the underlying zip file handle.
func (b *Book) Close() error {
	return b.zr.Close()
}

// precheckDRM reads META-INF/encryption.xml when present: every
// EncryptedData algorithm must be a font-obfuscation scheme, anything
// else (or an unparseable encryption.xml) is treated as DRM.
func (b *Book) precheckDRM() error {
	f, ok := b.files["META-INF/encryption.xml"]
	if !ok {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("%w: cannot read encryption.xml: %v", ErrDRMUnsupported, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("%w: cannot read encryption.xml: %v", ErrDRMUnsupported, err)
	}

	var doc struct {
		EncryptedData []struct {
			EncryptionMethod struct {
				Algorithm string `xml:"Algorithm,attr"`
			} `xml:"EncryptionMethod"`
		} `xml:"EncryptedData"`
	}
	if err := xmlDecodeBytes(data, &doc); err != nil {
		return fmt.Errorf("%w: malformed encryption.xml: %v", ErrDRMUnsupported, err)
	}
	for _, ed := range doc.EncryptedData {
		if !fontObfuscationAlgorithms[ed.EncryptionMethod.Algorithm] {
			return fmt.Errorf("%w: encryption algorithm %q", ErrDRMUnsupported, ed.EncryptionMethod.Algorithm)
		}
	}
	return nil
}

// parse fills the Book from container.xml → OPF, degrading to
// zip-inferred structure when either step cannot complete.
func (b *Book) parse() {
	opfPath := b.locateOPF()
	if opfPath == "" {
		b.warn(CodeMissingOPF, "no container.xml and no *.opf entry; structure inferred from zip")
		b.inferFromZip()
		b.loadTOC("")
		return
	}

	data, err := b.readEntry(opfPath)
	if err != nil {
		b.warn(CodeMalformedOPF, fmt.Sprintf("OPF %s unreadable: %v; structure inferred from zip", opfPath, err))
		b.inferFromZip()
		b.loadTOC("")
		return
	}
	var pkg opfPackage
	if err := xmlDecodeBytes(data, &pkg); err != nil {
		b.warn(CodeMalformedOPF, fmt.Sprintf("OPF %s malformed: %v; structure inferred from zip", opfPath, err))
		b.inferFromZip()
		b.loadTOC("")
		return
	}

	b.opfDir = path.Dir(opfPath)
	if b.opfDir == "." || b.opfDir == "/" {
		b.opfDir = ""
	}
	b.fillFromOPF(pkg)
	b.loadTOC(pkg.Spine.Toc)
}

// locateOPF finds the OPF: container.xml's first rootfile, else a zip
// scan for *.opf (shortest path wins, for single-book archives).
func (b *Book) locateOPF() string {
	if f, ok := b.files["META-INF/container.xml"]; ok {
		if data, err := readZipFile(f); err == nil {
			var c struct {
				Rootfiles []struct {
					FullPath string `xml:"full-path,attr"`
				} `xml:"rootfiles>rootfile"`
			}
			if err := xmlDecodeBytes(data, &c); err == nil {
				for _, rf := range c.Rootfiles {
					if p := normalizeZipName(rf.FullPath); p != "" {
						if _, ok := b.files[p]; ok {
							return p
						}
					}
				}
				// container.xml exists but points nowhere useful: fall
				// through to the zip scan with a warning.
			}
		}
		b.warn(CodeMissingContainer, "container.xml unusable; OPF found by zip scan")
		return b.scanForOPF()
	}
	b.warn(CodeMissingContainer, "container.xml missing; OPF found by zip scan")
	return b.scanForOPF()
}

func (b *Book) scanForOPF() string {
	best := ""
	for name := range b.files {
		if strings.HasSuffix(name, ".opf") && (best == "" || len(name) < len(best)) {
			best = name
		}
	}
	return best
}

// fillFromOPF maps a decoded OPF onto the Book, resolving manifest
// hrefs to zip paths and spine idrefs onto manifest items.
func (b *Book) fillFromOPF(pkg opfPackage) {
	if len(pkg.Metadata.Titles) > 0 {
		b.Title = pkg.Metadata.Titles[0]
	}
	b.Authors = pkg.Metadata.Creators
	if len(pkg.Metadata.Languages) > 0 {
		b.Language = pkg.Metadata.Languages[0]
	}

	b.manifest = make(map[string]ManifestItem, len(pkg.Manifest.Items))
	for _, it := range pkg.Manifest.Items {
		item := ManifestItem{
			ID:         it.ID,
			Href:       b.resolveHref(it.Href),
			MediaType:  it.MediaType,
			Properties: it.Properties,
		}
		b.Manifest = append(b.Manifest, item)
		if it.ID != "" {
			b.manifest[it.ID] = item
		}
	}

	for _, ref := range pkg.Spine.ItemRefs {
		item, ok := b.manifest[ref.IDRef]
		if !ok {
			continue // dangling idref: dropped silently, the zip-infer path would be worse
		}
		b.Spine = append(b.Spine, SpineItem{IDRef: ref.IDRef, Href: item.Href, Linear: ref.Linear != "no"})
	}
	if len(b.Spine) == 0 && len(b.Manifest) > 0 {
		b.warn(CodeMissingSpine, "spine missing or empty; reading order inferred from manifest")
		b.inferSpineFromManifest()
	}
}

// inferFromZip rebuilds manifest and spine from the archive itself:
// every document-ish entry becomes a manifest item, XHTML/HTML entries
// in zip order become the spine. Used when the OPF is missing or
// unparseable.
func (b *Book) inferFromZip() {
	for name := range b.files {
		if mt, ok := inferredMediaType(name); ok {
			item := ManifestItem{ID: path.Base(name), Href: name, MediaType: mt}
			b.Manifest = append(b.Manifest, item)
			b.manifest[item.ID] = item
		}
	}
	b.inferSpineFromManifest()
}

func (b *Book) inferSpineFromManifest() {
	for _, item := range b.Manifest {
		if isDocumentType(item.MediaType) {
			b.Spine = append(b.Spine, SpineItem{IDRef: item.ID, Href: item.Href, Linear: true})
		}
	}
}

func isDocumentType(mediaType string) bool {
	return mediaType == "application/xhtml+xml" || mediaType == "text/html"
}

// resolveHref turns a manifest/TOC href into a zip path: fragment
// stripped, %XX decoded, OPF-relative, cleaned.
func (b *Book) resolveHref(href string) string {
	return resolveRelative(b.opfDir, href)
}

// stripFragment cuts href at the first '#'.
func stripFragment(href string) string {
	if i := strings.IndexByte(href, '#'); i >= 0 {
		return href[:i]
	}
	return href
}

// normalizeZipName canonicalizes a zip entry name or joined path:
// absolute slash, no dot segments, no trailing slash.
func normalizeZipName(name string) string {
	name = strings.TrimPrefix(name, "/")
	if name == "" {
		return ""
	}
	cleaned := path.Clean(name)
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func (b *Book) warn(code WarningCode, message string) {
	b.Warnings = append(b.Warnings, Warning{Code: code, Message: message})
}

// ReadItem returns the raw bytes of the resource at href. Href may be
// any spelling the manifest, spine or TOC carry or a caller-derived
// variant of one: fragments are stripped, %XX decoded, ./ and ../
// resolved against the OPF's directory (a zip-root spelling also hits).
func (b *Book) ReadItem(href string) ([]byte, error) {
	name := b.resolveHref(href)
	if f, ok := b.files[name]; ok {
		return readZipFile(f)
	}
	// A caller may pass an already zip-rooted spelling that only differs
	// from the OPF-relative join; try the literal cleaned path too.
	if alt := normalizeZipName(stripFragment(href)); alt != name {
		if f, ok := b.files[alt]; ok {
			return readZipFile(f)
		}
	}
	return nil, fmt.Errorf("epub: item %q not found in archive", href)
}

// ManifestByID returns the manifest item with the given id.
func (b *Book) ManifestByID(id string) (ManifestItem, bool) {
	if b.manifest == nil {
		b.manifest = make(map[string]ManifestItem, len(b.Manifest))
		for _, item := range b.Manifest {
			b.manifest[item.ID] = item
		}
	}
	item, ok := b.manifest[id]
	return item, ok
}

func (b *Book) readEntry(name string) ([]byte, error) {
	f, ok := b.files[name]
	if !ok {
		return nil, fmt.Errorf("entry %q not found", name)
	}
	return readZipFile(f)
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// opfPackage mirrors the OPF with namespace-tolerant tags: encoding/xml
// matches a prefix-less field tag on local name in any namespace, so
// books with missing or odd namespaces still parse (Spine semantics).
type opfPackage struct {
	Metadata struct {
		Titles    []string `xml:"title"`
		Creators  []string `xml:"creator"`
		Languages []string `xml:"language"`
	} `xml:"metadata"`
	Manifest struct {
		Items []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			MediaType  string `xml:"media-type,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"item"`
	} `xml:"manifest"`
	Spine struct {
		Toc      string `xml:"toc,attr"`
		ItemRefs []struct {
			IDRef  string `xml:"idref,attr"`
			Linear string `xml:"linear,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
}

// ncxDoc mirrors the NCX navMap; navPoint nests recursively.
type ncxDoc struct {
	NavMap struct {
		NavPoints []ncxNavPoint `xml:"navPoint"`
	} `xml:"navMap"`
}

type ncxNavPoint struct {
	NavLabel struct {
		Text string `xml:"text"`
	} `xml:"navLabel"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	NavPoints []ncxNavPoint `xml:"navPoint"`
}

// inferredMediaType guesses a media type from the entry extension; ok is
// false for entries that do not belong in an inferred manifest.
func inferredMediaType(name string) (string, bool) {
	if strings.HasPrefix(name, "META-INF/") || strings.HasSuffix(name, "/") {
		return "", false
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".xhtml":
		return "application/xhtml+xml", true
	case ".html", ".htm":
		return "text/html", true
	case ".css":
		return "text/css", true
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".svg":
		return "image/svg+xml", true
	case ".ttf":
		return "font/ttf", true
	case ".otf":
		return "font/otf", true
	case ".woff":
		return "font/woff", true
	case ".ncx":
		return "application/x-dtbncx+xml", true
	default:
		return "", false
	}
}

// loadTOC fills the TOC: EPUB3 nav wins (added alongside its test),
// NCX is the fallback, and a book with neither gets a TOC generated
// from chapter titles plus a warning.
func (b *Book) loadTOC(spineTocID string) {
	if b.loadNavTOC() {
		return
	}
	if b.loadNCXTOC(spineTocID) {
		return
	}
	b.generateTOC()
}

// loadNCXTOC locates the NCX (spine@toc id → manifest media-type scan →
// any .ncx entry) and maps its navMap onto the TOC. Src hrefs resolve
// against the NCX's own directory.
func (b *Book) loadNCXTOC(spineTocID string) bool {
	ncxPath := ""
	if item, ok := b.ManifestByID(spineTocID); ok && spineTocID != "" {
		ncxPath = item.Href
	}
	if ncxPath == "" {
		for _, item := range b.Manifest {
			if item.MediaType == "application/x-dtbncx+xml" {
				ncxPath = item.Href
				break
			}
		}
	}
	if ncxPath == "" {
		for _, item := range b.Manifest {
			if strings.HasSuffix(item.Href, ".ncx") {
				ncxPath = item.Href
				break
			}
		}
	}
	if ncxPath == "" {
		return false
	}
	data, err := b.readEntry(ncxPath)
	if err != nil {
		b.warn(CodeMissingTOC, fmt.Sprintf("NCX %s unreadable (%v); TOC generated from chapter titles", ncxPath, err))
		return false
	}
	var doc ncxDoc
	if err := xmlDecodeBytes(data, &doc); err != nil {
		b.warn(CodeMissingTOC, fmt.Sprintf("NCX %s malformed (%v); TOC generated from chapter titles", ncxPath, err))
		return false
	}
	ncxDir := path.Dir(ncxPath)
	if ncxDir == "." || ncxDir == "/" {
		ncxDir = ""
	}
	b.TOC = navPointsToTOC(doc.NavMap.NavPoints, ncxDir)
	if len(b.TOC) == 0 {
		b.warn(CodeMissingTOC, fmt.Sprintf("NCX %s has no navPoints; TOC generated from chapter titles", ncxPath))
		return false
	}
	return true
}

func navPointsToTOC(points []ncxNavPoint, dir string) []TOCItem {
	var items []TOCItem
	for _, p := range points {
		item := TOCItem{Title: p.NavLabel.Text, Href: resolveRelative(dir, p.Content.Src)}
		item.Children = navPointsToTOC(p.NavPoints, dir)
		items = append(items, item)
	}
	return items
}

// generateTOC derives a flat TOC from chapter headings (first h1–h6 of
// each spine document), falling back to the file's base name.
func (b *Book) generateTOC() {
	b.warn(CodeMissingTOC, "no NCX and no nav document; TOC generated from chapter titles")
	for _, s := range b.Spine {
		title := ""
		if data, err := b.ReadItem(s.Href); err == nil {
			title = firstHeading(data)
		}
		if title == "" {
			title = strings.TrimSuffix(path.Base(s.Href), path.Ext(s.Href))
		}
		b.TOC = append(b.TOC, TOCItem{Title: title, Href: s.Href})
	}
}

// resolveRelative resolves href against dir ("" = zip root) into a
// normalized zip path.
func resolveRelative(dir, href string) string {
	if unescaped, err := url.PathUnescape(stripFragment(href)); err == nil {
		href = unescaped
	} else {
		href = stripFragment(href)
	}
	if dir == "" {
		return normalizeZipName(href)
	}
	return normalizeZipName(dir + "/" + href)
}

// firstHeading returns the text of the first h1–h6 element in an HTML
// document, or "" if none.
func firstHeading(data []byte) string {
	node, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	var find func(n *html.Node) string
	find = func(n *html.Node) string {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "h1", "h2", "h3", "h4", "h5", "h6":
				return nodeText(n)
			case "body":
				// headings live in body; nothing earlier can match
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if s := find(c); s != "" {
				return s
			}
		}
		return ""
	}
	return find(node)
}

func nodeText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(sb.String())
}

// loadNavTOC parses the EPUB3 nav document when the book has one.
func (b *Book) loadNavTOC() bool { return false } // arrives with its test

// xmlDecodeBytes decodes v from data with the package's tolerance
// settings (charset handling is added alongside its first test).
func xmlDecodeBytes(data []byte, v any) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	return dec.Decode(v)
}
