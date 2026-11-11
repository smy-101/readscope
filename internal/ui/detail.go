// detail.go: the overlay reader covering the list pane. Enter on a list
// row opens the item's cached snapshot through the M1 pipeline —
// DOM → blocks → wrap → theme — and Esc drops back to the list with
// focus and cursor untouched (they never move while reading). The body
// is the SQLite snapshot, so reading is offline by construction;
// Ctrl+O/Ctrl+Y hand the item URL to the system browser / clipboard.
package ui

import (
	"os"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/smy-101/readscope/internal/browser"
	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/render"
	"github.com/smy-101/readscope/internal/store"
)

// detail is the reader state for one open item: the snapshot row (kept
// by value — refreshes reloading m.items never disturb the reading
// session), the parsed document and its laid-out lines at the current
// width.
type detail struct {
	item  store.ItemRow
	doc   *render.Document
	lines []render.Line
	top   int // first visible line
}

// Side effects with seams for tests: neither spawning a browser nor
// touching the real clipboard belongs in unit tests.
var (
	openBrowser   = browser.Open
	copyClipboard = func(s string) error {
		// OSC 52: the terminal itself puts the text on the clipboard —
		// works over SSH, no xclip/wl-copy dependency. The sequence is
		// consumed by the terminal, never rendered, so interleaving
		// with Bubble Tea's frame writer is harmless.
		_, err := osc52.New(s).WriteTo(os.Stdout)
		return err
	}
)

// --- open / layout -------------------------------------------------------

// openDetail lays the selected item over the list. Returns false when
// there is nothing to open (empty list / cursor out of range).
func (m *Model) openDetail() bool {
	if m.listSel >= len(m.items) {
		return false
	}
	d := &detail{item: m.items[m.listSel]}
	d.doc = m.detailDoc(d.item.Item)
	d.layout(m.detailWidth())
	m.reader = d
	return true
}

// detailWidth is the reading column: the list pane minus its left
// padding (the reader renders inside the same List style).
func (m Model) detailWidth() int {
	w := m.listWidth() - 1
	if w < 1 {
		w = 1
	}
	return w
}

func (d *detail) layout(width int) {
	d.lines = render.Layout(d.doc, width)
}

// detailDoc builds the reading document: title heading, dim meta line,
// rule, then the body. The header carries no links, so body link
// numbers (and the appendix Layout appends from doc.Links) stay exactly
// as the parser numbered them.
func (m Model) detailDoc(it core.Item) *render.Document {
	var doc *render.Document
	switch {
	case strings.TrimSpace(it.Body) == "":
		doc = &render.Document{Blocks: []render.Block{
			render.Para{Spans: []render.Span{{Text: m.cat.Get("detail.no_body"), Style: render.SpanDim}}},
		}}
	case strings.Contains(it.Body, "<"):
		if parsed, err := render.ParseHTML(it.Body); err == nil {
			doc = parsed
		} else {
			doc = plainDoc(it.Body)
		}
	default:
		doc = plainDoc(it.Body)
	}

	header := []render.Block{
		render.Heading{Level: 1, Spans: []render.Span{
			{Text: it.Title, Style: render.SpanBold | render.SpanUnderline},
		}},
	}
	if meta := m.detailMeta(it); meta != "" {
		header = append(header, render.Para{Spans: []render.Span{
			{Text: meta, Style: render.SpanDim},
		}})
	}
	header = append(header, render.Rule{})
	doc.Blocks = append(header, doc.Blocks...)
	return doc
}

// detailMeta is the dim line under the title: author · relative age.
func (m Model) detailMeta(it core.Item) string {
	var parts []string
	if it.Author != "" {
		parts = append(parts, it.Author)
	}
	if it.PublishedAt != nil {
		parts = append(parts, m.relTime(it.PublishedAt))
	}
	return strings.Join(parts, " · ")
}

// plainDoc renders a body with no HTML in it as one paragraph per
// source line (blank lines collapse to block gaps); long lines still
// wrap through Layout.
func plainDoc(body string) *render.Document {
	doc := &render.Document{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		doc.Blocks = append(doc.Blocks, render.Para{Spans: []render.Span{{Text: line}}})
	}
	return doc
}

// --- scrolling -----------------------------------------------------------

// scrollDetail moves the reading window by delta lines (±pages from
// the key layer); clampDetail pins it to [0, last page].
func (m *Model) scrollDetail(delta int) {
	if m.reader == nil {
		return
	}
	m.reader.top += delta
	m.clampDetail()
}

func (m *Model) clampDetail() {
	d := m.reader
	if d == nil {
		return
	}
	maxTop := len(d.lines) - m.bodyHeight()
	if maxTop < 0 {
		maxTop = 0
	}
	if d.top > maxTop {
		d.top = maxTop
	}
	if d.top < 0 {
		d.top = 0
	}
}

// --- keys ----------------------------------------------------------------

// detailKeys handles keys while the reader is open. Detail-only keys
// stop here (handled=true); shell-wide keys (quit, F1 help, Ctrl+T
// theme, F5 refresh, Tab source switch) return false and fall through
// to the shared switch in Update. The help overlay (checked by the
// caller) always wins over the reader.
func (m *Model) detailKeys(msg tea.KeyMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.reader = nil // back to the list: focus and cursor never moved
		return true, nil
	case "enter": // already open; never re-open (would reset scroll)
		return true, nil
	case "up":
		m.scrollDetail(-1)
		return true, nil
	case "down":
		m.scrollDetail(1)
		return true, nil
	case "pgup":
		m.scrollDetail(-m.bodyHeight())
		return true, nil
	case "pgdown":
		m.scrollDetail(m.bodyHeight())
		return true, nil
	case "home":
		m.reader.top = 0
		return true, nil
	case "end":
		m.reader.top = len(m.reader.lines)
		m.clampDetail()
		return true, nil
	case "left", "right":
		// Region navigation is suspended under the overlay.
		return true, nil
	case "ctrl+r": // toggle the open item's read state; list follows
		// via the reload chain when Esc drops back
		m.reader.item.Read = !m.reader.item.Read
		return true, m.setReadCmd(m.reader.item.Item.ID, m.reader.item.Read)
	case "ctrl+f": // star/unstar what is on screen (snapshot at star time)
		m.reader.item.Starred = !m.reader.item.Starred
		return true, m.setStarredCmd(m.reader.item.Item, m.reader.item.Starred)
	case "ctrl+o":
		url := m.reader.item.Item.URL
		if url == "" {
			m.status = m.cat.Get("detail.no_url")
			return true, nil
		}
		return true, openBrowserCmd(url)
	case "ctrl+y":
		url := m.reader.item.Item.URL
		if url == "" {
			m.status = m.cat.Get("detail.no_url")
			return true, nil
		}
		return true, copyLinkCmd(url)
	}
	return false, nil
}

// --- side-effect commands --------------------------------------------------

type browserDoneMsg struct{ err error }
type copiedMsg struct{ err error }

func openBrowserCmd(url string) tea.Cmd {
	return func() tea.Msg { return browserDoneMsg{err: openBrowser(url)} }
}

func copyLinkCmd(url string) tea.Cmd {
	return func() tea.Msg { return copiedMsg{err: copyClipboard(url)} }
}

// --- view ----------------------------------------------------------------

// detailView renders the visible window of lines into the list pane's
// style; Height pads with blanks so the list never bleeds through.
func (m Model) detailView() string {
	d := m.reader
	w, h := m.listWidth(), m.bodyHeight()
	end := min(d.top+h, len(d.lines))
	text := render.ToANSI(d.lines[d.top:end], m.th.StyleFor)
	return m.th.List.Width(w).Height(h).Render(text)
}

// detailPercent is the reading position for the status bar: how far
// the bottom of the window is through the article.
func (m Model) detailPercent() int {
	d := m.reader
	if d == nil || len(d.lines) == 0 {
		return 0
	}
	end := min(d.top+m.bodyHeight(), len(d.lines))
	return end * 100 / len(d.lines)
}
