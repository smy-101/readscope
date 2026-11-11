// detail.go: the overlay reader covering the list pane. Enter on a
// list row opens the item's cached snapshot through the M1 pipeline —
// DOM → blocks → wrap → theme — and Esc drops back to the list with
// focus and cursor untouched (they never move while reading). The
// body is the SQLite snapshot, so reading is offline by construction.
//
// Ticket 04 extends the reader from one layer to three for sources
// with a detail layer (hn): body → floors → replies. Each layer keeps
// its own scroll state, Esc steps back exactly one layer, and the
// comment cache loads cache-first — a first open shows a loading
// footer while the fetch runs, a re-open renders the cached tree
// immediately and refreshes silently in the background.

package ui

import (
	"context"
	"os"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/smy-101/readscope/internal/browser"
	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/refresh"
	"github.com/smy-101/readscope/internal/render"
	"github.com/smy-101/readscope/internal/source"
	"github.com/smy-101/readscope/internal/store"
)

// detailLayer is the reader's position in the three-layer stack.
// Sources without a detail layer (rss) only ever see layerBody.
type detailLayer int

const (
	layerBody    detailLayer = iota // the article itself
	layerFloors                     // one top-level comment per screen
	layerReplies                    // one floor's reply tree, flattened
)

// floorSkip is how far Ctrl+N hops in the floor layer — the in-memory
// analogue of the list's "load next page": a fast-forward through
// floors that are all already cached, zero requests.
const floorSkip = 10

// detail is the reader state for one open item: the snapshot row (kept
// by value — refreshes reloading m.items never disturb the reading
// session), the parsed document and its laid-out lines at the current
// width, and — when the source provides a detail layer — the comment
// stack: floors, the current floor's laid-out screen, and the reply
// layer for the floor it was last built for.
type detail struct {
	item  store.ItemRow
	doc   *render.Document
	lines []render.Line
	top   int // first visible line (body layer)

	// Comment layers. hasDetail is false for sources without the
	// DetailSource extension (rss): Enter stays a no-op, no footer.
	hasDetail  bool
	commentURL func(id string) string // ds.CommentURL bound at open
	loading    bool                   // first open: fetch in flight, nothing cached
	loaded     bool                   // a definitive tree sits in floors (cache or net)
	loadErr    error                  // fetch failure while nothing is loaded
	floors     []floor

	layer      detailLayer
	floorIdx   int // which floor the floor layer shows
	floorLines []render.Line
	floorTop   int

	replies     []replyRow // flattened rows of floor `replyFloor`
	replyStarts []int      // each reply's first line in replyLines (ascending)
	replyLines  []render.Line
	replyTop    int
	replyFloor  int // floorIdx replyLines/replies belong to; -1 = none
}

// activeLines / activeTop point the scroll plumbing at whichever
// layer is on screen.
func (d *detail) activeLines() []render.Line {
	switch d.layer {
	case layerFloors:
		return d.floorLines
	case layerReplies:
		return d.replyLines
	default:
		return d.lines
	}
}

func (d *detail) activeTop() *int {
	switch d.layer {
	case layerFloors:
		return &d.floorTop
	case layerReplies:
		return &d.replyTop
	default:
		return &d.top
	}
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
// there is nothing to open (empty list / cursor out of range); the
// command kicks the cache-first comment load for detail sources.
func (m *Model) openDetail() (bool, tea.Cmd) {
	if m.listSel >= len(m.items) {
		return false, nil
	}
	d := &detail{item: m.items[m.listSel], replyFloor: -1}
	if m.srcs != nil {
		if src, err := source.New(d.item.Item.Source, m.srcs); err == nil {
			if ds, ok := src.(source.DetailSource); ok {
				d.hasDetail = true
				d.commentURL = ds.CommentURL
				d.loading = true // until the cache probe says otherwise
			}
		}
	}
	m.reader = d
	m.rebuildBodyDoc()
	if d.hasDetail {
		return true, m.readCommentsCmd(d.item.Item)
	}
	return true, nil
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

// relayoutReader re-wraps every layer the resize touches: the body
// always, the active comment layer in place (position kept, clamped);
// inactive layer caches are invalidated for their next entry.
func (m *Model) relayoutReader() {
	d := m.reader
	d.layout(m.detailWidth())
	switch d.layer {
	case layerFloors:
		m.layoutFloor()
	case layerReplies:
		if d.replyFloor == d.floorIdx && d.replyLines != nil {
			m.layoutReplies()
		} else {
			d.replyLines = nil
		}
	default:
		d.floorLines, d.replyLines = nil, nil
	}
	m.clampDetail()
}

// rebuildBodyDoc recomputes the body layer's document — header, body,
// and (for detail sources) the comments footer whose text tracks the
// load state — keeping the reading position clamped.
func (m *Model) rebuildBodyDoc() {
	d := m.reader
	doc := m.detailDoc(d.item.Item)
	if d.hasDetail {
		doc.Blocks = append(doc.Blocks,
			render.Rule{},
			render.Para{Spans: []render.Span{{Text: m.commentsFooter(), Style: render.SpanDim}}},
		)
	}
	d.doc = doc
	d.layout(m.detailWidth())
	m.clampDetail()
}

// commentsFooter is the one-line comment state under the article:
// loading / failed / empty / floor count.
func (m Model) commentsFooter() string {
	d := m.reader
	switch {
	case d.loading:
		return m.cat.Get("detail.loading")
	case d.loadErr != nil:
		return m.cat.Get("detail.load_failed")
	case len(d.floors) == 0:
		return m.cat.Get("detail.no_comments")
	default:
		return m.cat.Get("detail.floor_count", len(d.floors))
	}
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

// --- comment loading -----------------------------------------------------

// commentsMsg carries one leg of the cache-first comment load. The
// cache leg (fromNet=false) reports the store probe; the net leg
// (fromNet=true) reports refresh.Detail's fetch-and-save.
type commentsMsg struct {
	itemID   string
	comments []core.Comment
	opened   bool // cache leg: the opened marker was present
	fromNet  bool // this is the refresh half, not the cache probe
	err      error
}

// readCommentsCmd is the cache half: read the cached tree and the
// opened marker — local SQLite only, no network.
func (m Model) readCommentsCmd(it core.Item) tea.Cmd {
	st := m.st
	return func() tea.Msg {
		ctx := context.Background()
		comments, err := st.CommentsForItem(ctx, it.ID)
		if err != nil {
			return commentsMsg{itemID: it.ID, err: err}
		}
		opened, err := st.DetailFetched(ctx, it.ID)
		if err != nil {
			return commentsMsg{itemID: it.ID, err: err}
		}
		return commentsMsg{itemID: it.ID, comments: comments, opened: opened}
	}
}

// refreshCommentsCmd is the network half: fetch the whole tree and
// cache it (refresh.Detail), whatever the cache said.
func (m Model) refreshCommentsCmd(it core.Item) tea.Cmd {
	st, srcs := m.st, m.srcs
	return func() tea.Msg {
		c, err := refresh.Detail(context.Background(), st, srcs, it)
		return commentsMsg{itemID: it.ID, comments: c.Comments, fromNet: true, err: err}
	}
}

// commentsLanded folds a commentsMsg into the reader: cache first
// (render immediately when opened), then the background refresh —
// silent on success and on failure-when-cache-exists, loud only when
// a first open has nothing to fall back on. Stale messages (reader
// closed, another item open) are dropped.
func (m *Model) commentsLanded(msg commentsMsg) tea.Cmd {
	d := m.reader
	if d == nil || !d.hasDetail || d.item.Item.ID != msg.itemID {
		return nil
	}
	if msg.err != nil {
		if !msg.fromNet {
			return m.refreshCommentsCmd(d.item.Item) // cache probe failed; the net half decides
		}
		d.loading = false
		if !d.loaded { // first open, nothing cached: say so, keep the body readable
			d.loadErr = msg.err
			m.status = m.cat.Get("errors.detail_fetch", msg.err)
			if d.layer == layerFloors {
				m.layoutFloor()
			}
		}
		return nil
	}
	if !msg.fromNet {
		if msg.opened {
			d.loading = false
			m.setFloors(msg.comments)
		}
		return m.refreshCommentsCmd(d.item.Item) // background refresh either way
	}
	d.loading = false
	m.setFloors(msg.comments)
	return nil
}

// setFloors installs a definitive tree (cache or net) and refreshes
// whatever the reader is showing: the active comment layer re-lays
// out in place (scroll kept, clamped), inactive caches are
// invalidated for their next entry, and the body footer recounts.
func (m *Model) setFloors(comments []core.Comment) {
	d := m.reader
	d.floors = buildFloors(comments)
	d.loaded = true
	d.loadErr = nil
	if d.floorIdx >= len(d.floors) {
		d.floorIdx = max(0, len(d.floors)-1)
		d.floorTop = 0
		d.floorLines = nil
	}
	switch d.layer {
	case layerFloors:
		m.layoutFloor()
	case layerReplies:
		if d.replyFloor == d.floorIdx && d.floorIdx < len(d.floors) && len(d.floors[d.floorIdx].replies) > 0 {
			m.layoutReplies() // same floor, fresh tree; replyTop clamped
		} else {
			d.layer = layerFloors // the floor or its replies vanished: step back
			m.layoutFloor()
		}
	default:
		d.floorLines, d.replyLines = nil, nil
	}
	m.rebuildBodyDoc()
}

// --- floor / reply layer rendering ---------------------------------------

// layoutFloor renders the floor layer's current screen: the floor
// header, a rule, then the floor's own text through the pipeline —
// or, before anything is loaded, the one-line state the layer is in.
func (m *Model) layoutFloor() {
	d := m.reader
	if !d.loaded || len(d.floors) == 0 {
		d.floorLines = m.pendingLines()
		d.floorTop = 0
		return
	}
	if d.floorIdx >= len(d.floors) {
		d.floorIdx = len(d.floors) - 1
	}
	d.floorLines = render.Layout(m.floorDoc(&d.floors[d.floorIdx]), m.detailWidth())
	m.clampDetail()
}

// pendingLines is the floor layer's placeholder screen while there is
// no tree to show: loading, failed, or genuinely empty.
func (m Model) pendingLines() []render.Line {
	d := m.reader
	text := m.cat.Get("detail.no_comments")
	switch {
	case d.loading:
		text = m.cat.Get("detail.loading")
	case d.loadErr != nil:
		text = m.cat.Get("detail.load_failed")
	}
	return []render.Line{{Spans: []render.Span{{Text: text, Style: render.SpanDim}}}}
}

// floorDoc is one floor's document: "author · N replies" header, rule,
// body. The body's link table rides along (inline numbers and the
// appendix stay paired), so their appendix sits at the end of the
// floor — the nearest possible place.
func (m Model) floorDoc(f *floor) *render.Document {
	body := m.commentDoc(f.cm)
	return &render.Document{
		Blocks: append([]render.Block{
			render.Para{Spans: m.floorHeaderSpans(f)},
			render.Rule{},
		}, body.Blocks...),
		Links: body.Links,
	}
}

// floorHeaderSpans is "author · score? · N replies" with the standing
// degradation rule: absent segments drop, never an empty "· ·". Score
// is nil by construction for hn (Algolia's tree endpoint has no
// per-comment points); the segment exists for sources that have one.
func (m Model) floorHeaderSpans(f *floor) []render.Span {
	author, style := m.authorOrDeleted(f.cm.Author), render.SpanBold
	if f.cm.Author == "" {
		style = render.SpanDim
	}
	spans := []render.Span{{Text: author, Style: style}}
	if f.cm.Score != nil {
		spans = append(spans, dimSpan(" · "+m.cat.Get("hn.pts", *f.cm.Score)))
	}
	spans = append(spans, dimSpan(" · "+m.cat.Get("detail.floor_meta", len(f.replies))))
	return spans
}

// commentDoc renders one comment's text through the shared M1
// pipeline (blocks and link table together — callers keep them
// paired); an empty text (deleted placeholder) renders the dim
// [deleted] line so the node stays visible for its children.
func (m Model) commentDoc(c core.Comment) *render.Document {
	if strings.TrimSpace(c.Text) == "" {
		return &render.Document{Blocks: []render.Block{render.Para{Spans: []render.Span{
			{Text: m.cat.Get("detail.deleted"), Style: render.SpanDim},
		}}}}
	}
	if strings.Contains(c.Text, "<") {
		if doc, err := render.ParseHTML(c.Text); err == nil {
			return doc
		}
	}
	return plainDoc(c.Text)
}

// layoutReplies renders the reply layer: a dim context header naming
// the floor, then one block group per flattened reply row — direct
// replies flat, deeper replies wrapped in a quote (the pipeline's own
// nesting visual) with an @parent prefix on their header. Each
// reply's links stay local to its group, appendix and all. replyStarts
// records where each reply begins, which is how Ctrl+N (next reply)
// and Ctrl+O/Y (topmost visible reply) find their target.
func (m *Model) layoutReplies() {
	d := m.reader
	w := m.detailWidth()
	f := &d.floors[d.floorIdx]
	d.replies = flattenReplies(f)
	d.replyFloor = d.floorIdx
	author := m.authorOrDeleted(f.cm.Author)
	header := &render.Document{Blocks: []render.Block{
		render.Para{Spans: []render.Span{
			{Text: m.cat.Get("detail.replies_to", author), Style: render.SpanDim},
		}},
		render.Rule{},
	}}
	lines := render.Layout(header, w)
	d.replyStarts = make([]int, len(d.replies))
	for i, row := range d.replies {
		d.replyStarts[i] = len(lines)
		lines = append(lines, render.Layout(m.replyDoc(row), w)...)
		lines = append(lines, render.Line{}) // gap between replies
	}
	d.replyLines = lines
	m.clampDetail()
}

// replyDoc is one reply row's document: header line ("@parent author
// · age"), then the body — inside a quote when the row is a compressed
// deeper reply, so the second visual level is the pipeline's quote
// bar. The row's links stay local: appendix right below the reply.
func (m Model) replyDoc(row replyRow) *render.Document {
	var header []render.Span
	if row.depth >= 2 {
		header = append(header, render.Span{Text: "@" + m.authorOrDeleted(row.parentAuthor) + " ", Style: render.SpanDim})
	}
	author, style := m.authorOrDeleted(row.cm.Author), render.SpanBold
	if row.cm.Author == "" {
		style = render.SpanDim
	}
	header = append(header, render.Span{Text: author, Style: style})
	if row.cm.CreatedAt != nil {
		header = append(header, dimSpan(" · "+m.relTime(row.cm.CreatedAt)))
	}
	body := m.commentDoc(row.cm)
	blocks := []render.Block{render.Para{Spans: header}}
	if row.depth >= 2 {
		blocks = append(blocks, render.Quote{Blocks: body.Blocks})
	} else {
		blocks = append(blocks, body.Blocks...)
	}
	return &render.Document{Blocks: blocks, Links: body.Links}
}

func dimSpan(text string) render.Span {
	return render.Span{Text: text, Style: render.SpanDim}
}

// authorOrDeleted is a comment author's display name: the author, or
// the [deleted] placeholder for deleted nodes (no author stored).
func (m Model) authorOrDeleted(author string) string {
	if author == "" {
		return m.cat.Get("detail.deleted")
	}
	return author
}

// --- scrolling -----------------------------------------------------------

// scrollDetail moves the active layer's window by delta lines (±pages
// from the key layer); clampDetail pins it to [0, last page].
func (m *Model) scrollDetail(delta int) {
	if m.reader == nil {
		return
	}
	*m.reader.activeTop() += delta
	m.clampDetail()
}

func (m *Model) clampDetail() {
	d := m.reader
	if d == nil {
		return
	}
	top := d.activeTop()
	maxTop := len(d.activeLines()) - m.bodyHeight()
	if maxTop < 0 {
		maxTop = 0
	}
	if *top > maxTop {
		*top = maxTop
	}
	if *top < 0 {
		*top = 0
	}
}

// --- keys ----------------------------------------------------------------

// detailKeys handles keys while the reader is open. Detail-only keys
// stop here (handled=true); shell-wide keys (quit, F1 help, Ctrl+T
// theme, F5 refresh, Tab source switch) return false and fall through
// to the shared switch in Update. The help overlay (checked by the
// caller) always wins over the reader.
func (m *Model) detailKeys(msg tea.KeyMsg) (bool, tea.Cmd) {
	d := m.reader
	switch msg.String() {
	case "esc": // one layer back; from the body, back to the list
		switch d.layer {
		case layerReplies:
			d.layer = layerFloors
		case layerFloors:
			d.layer = layerBody
		default:
			m.reader = nil // focus and cursor never moved
		}
		return true, nil

	case "enter":
		switch d.layer {
		case layerBody: // into the floors (detail sources only)
			if d.hasDetail {
				m.enterFloors()
			}
			return true, nil // never re-open the body (would reset scroll)
		case layerFloors:
			if m.enterReplies() {
				return true, nil
			}
			m.status = m.cat.Get("detail.no_replies") // replyless floor: no-op, say why
			return true, nil
		default:
			return true, nil // replies is the deepest layer
		}

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
		*d.activeTop() = 0
		return true, nil
	case "end":
		*d.activeTop() = len(d.activeLines())
		m.clampDetail()
		return true, nil

	case "left", "right": // floor-to-floor; suspended elsewhere
		if d.layer == layerFloors {
			if msg.String() == "left" {
				m.moveFloor(-1)
			} else {
				m.moveFloor(1)
			}
		}
		return true, nil

	case "ctrl+n": // in-memory fast-forward: no request ever leaves
		switch d.layer {
		case layerFloors:
			m.moveFloor(floorSkip)
		case layerReplies:
			m.nextReplyStart()
		}
		return true, nil

	case "ctrl+r": // toggle the open item's read state; list follows
		// via the reload chain when Esc drops back
		m.reader.item.Read = !m.reader.item.Read
		return true, m.setReadCmd(m.reader.item.Item.ID, m.reader.item.Read)
	case "ctrl+f": // star/unstar what is on screen (snapshot at star time)
		m.reader.item.Starred = !m.reader.item.Starred
		return true, m.setStarredCmd(m.reader.item.Item, m.reader.item.Starred)
	case "ctrl+o":
		url := m.detailTargetURL()
		if url == "" {
			m.status = m.cat.Get("detail.no_url")
			return true, nil
		}
		return true, openBrowserCmd(url)
	case "ctrl+y":
		url := m.detailTargetURL()
		if url == "" {
			m.status = m.cat.Get("detail.no_url")
			return true, nil
		}
		return true, copyLinkCmd(url)
	}
	return false, nil
}

// enterFloors opens the floor layer at the current floor (0 on first
// entry; the index survives round trips through the body layer).
func (m *Model) enterFloors() {
	d := m.reader
	d.layer = layerFloors
	if d.floorLines == nil {
		m.layoutFloor()
	}
}

// enterReplies opens the reply layer for the floor on screen. False
// (and no transition) when the floor has no replies — the ticket's
// "replyless floors don't respond to Enter".
func (m *Model) enterReplies() bool {
	d := m.reader
	if !d.loaded || d.floorIdx >= len(d.floors) {
		return false
	}
	if len(d.floors[d.floorIdx].replies) == 0 {
		return false
	}
	if d.replyFloor != d.floorIdx || d.replyLines == nil {
		d.replyTop = 0
		m.layoutReplies()
	}
	d.layer = layerReplies
	return true
}

// moveFloor moves the floor cursor by delta (clamped); a real move
// resets that screen's scroll — a new floor reads from its top.
func (m *Model) moveFloor(delta int) {
	d := m.reader
	if !d.loaded || len(d.floors) == 0 {
		return
	}
	next := d.floorIdx + delta
	next = max(0, min(next, len(d.floors)-1))
	if next != d.floorIdx {
		d.floorIdx = next
		d.floorTop = 0
	}
	m.layoutFloor()
}

// nextReplyStart scrolls the reply layer to the next reply's first
// line — Ctrl+N's "one more reply", all in memory.
func (m *Model) nextReplyStart() {
	d := m.reader
	for _, s := range d.replyStarts {
		if s > d.replyTop {
			d.replyTop = s
			m.clampDetail()
			return
		}
	}
	d.replyTop = len(d.replyLines) // at the last reply already: go to the end
	m.clampDetail()
}

// detailTargetURL is Ctrl+O/Ctrl+Y's target per layer: the item URL on
// the body (a text post with no URL falls back to its discussion
// page), the floor's comment page on the floor layer, and the topmost
// visible reply's page on the reply layer.
func (m Model) detailTargetURL() string {
	d := m.reader
	switch d.layer {
	case layerFloors:
		if d.commentURL != nil && d.loaded && d.floorIdx < len(d.floors) {
			return d.commentURL(d.floors[d.floorIdx].cm.ID)
		}
	case layerReplies:
		if d.commentURL != nil && len(d.replies) > 0 {
			return d.commentURL(d.replies[d.topmostReplyIdx()].cm.ID)
		}
	default:
		if d.item.Item.URL != "" {
			return d.item.Item.URL
		}
		if d.hasDetail && d.commentURL != nil {
			return d.commentURL(d.item.Item.ID) // text post: its own HN page
		}
	}
	return ""
}

// topmostReplyIdx is which reply the scroll window starts inside —
// the reply the user is looking at.
func (d *detail) topmostReplyIdx() int {
	idx := 0
	for i, s := range d.replyStarts {
		if s <= d.replyTop {
			idx = i
		} else {
			break
		}
	}
	return idx
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

// detailView renders the visible window of the active layer's lines
// into the list pane's style; Height pads with blanks so the list
// never bleeds through.
func (m Model) detailView() string {
	d := m.reader
	w, h := m.listWidth(), m.bodyHeight()
	lines := d.activeLines()
	top := *d.activeTop()
	end := min(top+h, len(lines))
	text := render.ToANSI(lines[top:end], m.th.StyleFor)
	return m.th.List.Width(w).Height(h).Render(text)
}

// detailPercent is the reading position for the status bar: how far
// the bottom of the window is through the active layer.
func (m Model) detailPercent() int {
	d := m.reader
	lines := d.activeLines()
	if len(lines) == 0 {
		return 0
	}
	end := min(*d.activeTop()+m.bodyHeight(), len(lines))
	return end * 100 / len(lines)
}
