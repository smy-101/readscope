// List rendering for the active feed's cached items: the row layout
// (unread dot · title · source-formatted meta), the truncation math
// that keeps a narrow terminal intact, and the clamped cursor + scroll
// window. Moved here from app.go so the shell model changes for shell
// reasons and the list changes for list reasons (plan package layout:
// list.go).
package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source/zhihu"
	"github.com/smy-101/readscope/internal/store"
)

func (m Model) listWidth() int {
	w := m.width - sidebarWidth - 2
	if w < 1 {
		w = 1
	}
	return w
}

// listWindow is the visible slice of m.items given the body height.
func (m Model) listWindow(h int) (top, end int) {
	top = m.listTop
	if top > len(m.items)-1 {
		top = len(m.items) - 1
	}
	if top < 0 {
		top = 0
	}
	end = top + h
	if end > len(m.items) {
		end = len(m.items)
	}
	return top, end
}

// moveListSel moves the cursor by delta (rows or pages), clamped to the
// items, and keeps the cursor inside the scroll window.
func (m *Model) moveListSel(delta int) {
	if len(m.items) == 0 {
		m.listSel, m.listTop = 0, 0
		return
	}
	sel := m.listSel + delta
	if sel < 0 {
		sel = 0
	}
	if sel > len(m.items)-1 {
		sel = len(m.items) - 1
	}
	m.listSel = sel
	m.clampListCursor()
}

// clampListCursor keeps the cursor valid for the current items and
// scrolls the window to keep it visible (also the same-feed reload
// path, where the cursor must survive).
func (m *Model) clampListCursor() {
	if len(m.items) == 0 {
		m.listSel, m.listTop = 0, 0
		return
	}
	if m.listSel > len(m.items)-1 {
		m.listSel = len(m.items) - 1
	}
	h := m.bodyHeight()
	if h < 1 {
		h = 1
	}
	if m.listSel < m.listTop {
		m.listTop = m.listSel
	}
	if m.listSel >= m.listTop+h {
		m.listTop = m.listSel - h + 1
	}
	if m.listTop < 0 {
		m.listTop = 0
	}
}

func (m Model) listView() string {
	w, h := m.listWidth(), m.bodyHeight()
	if len(m.items) == 0 {
		empty := m.cat.Get("list.empty")
		if m.favorites {
			empty = m.cat.Get("list.favorites_empty")
		}
		return m.th.List.Width(w).Height(h).
			Render(m.th.Dim.Render(empty))
	}
	usable := w - 1 // List has PaddingLeft(1)
	// Meta column: content-formatted per source (rowMeta), sized to the
	// widest meta across the whole list so scrolling never makes the
	// column jump, clamped to [metaMinW, metaMaxW]. A too-narrow
	// terminal still drops the column before squeezing the title — the
	// standing rule from the time-only days.
	metas := make([]string, len(m.items))
	metaW := metaMinW
	for i, row := range m.items {
		metas[i] = m.rowMeta(row)
		if dw := runewidth.StringWidth(metas[i]); dw > metaW {
			metaW = dw
		}
	}
	if metaW > metaMaxW {
		metaW = metaMaxW
	}
	if usable < metaW+5 {
		metaW = 0 // too narrow: drop the meta column before the title
	}
	titleW := usable - 2 // dot + its gap
	if metaW > 0 {
		titleW = usable - 3 - metaW // + meta gap
	}
	if titleW < 1 {
		titleW = 1
	}
	top, end := m.listWindow(h)
	lines := make([]string, 0, end-top)
	for i := top; i < end; i++ {
		row := m.items[i]
		selected := i == m.listSel
		// Marker column: ★ starred (star wins the slot), ● unread,
		// blank read. The title dims on read state only — a star never
		// dims.
		marker, titleStyle := " ", m.th.TitleRead
		if row.Starred {
			marker = "★"
		} else if !row.Read {
			marker = "●"
		}
		if !row.Read {
			titleStyle = m.th.Title
		}
		// The selected row re-renders every segment through
		// ListSelected (the sidebar's badge trick): wrapping an
		// already-styled line in ListSelected dies at the first
		// embedded reset, which lit up only the marker column — the
		// cursor looked like a lone dot instead of a full-row bar.
		markerStyle, metaStyle := m.th.UnreadDot, m.th.Meta
		if selected {
			markerStyle, titleStyle, metaStyle = m.th.ListSelected, m.th.ListSelected, m.th.ListSelected
		}
		title := runewidth.FillRight(runewidth.Truncate(row.Item.Title, titleW, "…"), titleW)
		// Inter-segment gaps ride inside the neighbouring render, so
		// the selection bar has no unstyled holes.
		line := markerStyle.Render(marker+" ") + titleStyle.Render(title)
		if metaW > 0 {
			meta := runewidth.FillLeft(runewidth.Truncate(metas[i], metaW, "…"), metaW)
			line += metaStyle.Render(" " + meta)
		}
		if selected {
			// Pad the bar to the full usable width in the selection
			// style — the row reads as one highlighted line.
			if pad := usable - lipgloss.Width(line); pad > 0 {
				line += m.th.ListSelected.Render(strings.Repeat(" ", pad))
			}
		}
		lines = append(lines, line)
	}
	return m.th.List.Width(w).Height(h).Render(strings.Join(lines, "\n"))
}

// The meta column's width clamps: metaMinW is the old fixed
// time-column width (room for "45 分钟前"), kept as the floor so feeds
// whose metas stay short — every relative-time row — render
// byte-identically to before. metaMaxW caps how far a wide meta (hn's
// heat radar) may grow, so a long author tail truncates with "…"
// instead of eating the title.
const (
	metaMinW = 10
	metaMaxW = 32
)

// rowMeta formats the meta column by source: hn rows carry the heat
// radar, zhihu rows the hot board's heat reading, every other source
// keeps the relative age. The favorites list mixes sources row by
// row, so the choice is per row, not per feed.
func (m Model) rowMeta(row store.ItemRow) string {
	if row.Item.Source == "hn" {
		return m.hnMeta(row.Item)
	}
	if row.Item.Source == "zhihu" {
		return m.zhihuMeta(row.Item)
	}
	return m.relTime(row.Item.PublishedAt)
}

// zhihuMeta renders the row's zhihu meta column, routed per feed
// (the 02 review's transfer: the daily hint was the first
// source-internal per-feed case — route the feed, don't pile field
// ifs): the hot board's heat reading "N 万热度" from the folded heat
// snapshot (the fold lives in zhihu.heatPoints, each side pinned by
// its own test; the fold only ever stores whole 万, so the division
// restores the exact number); the follow and recommend feeds' card
// author (the answerer on an answer card, the asker on a question
// card); the daily digest's hint, carried verbatim in the meta
// column — the source serves it as one atomic string. A row without
// its feed's segment keeps the meta blank like the date-less rows
// always have.
func (m Model) zhihuMeta(it core.Item) string {
	switch it.Feed {
	case zhihu.HotFeedKey:
		if it.Points != nil {
			return m.cat.Get("zhihu.heat", *it.Points/10000)
		}
		return "" // pinned card, or a heat format zhihu invented later
	case zhihu.DailyFeedKey:
		return it.Meta // the hint, verbatim
	default: // follow, recommend
		return it.Author
	}
}

// hnMeta renders the heat radar "N pts · N comments · author" from the
// fetched-at snapshot. Each segment drops when the record lacks it — a
// hit without points renders "N comments · author", never an empty
// "· ·" run; a completely bare record renders an empty meta like the
// date-less rows always have.
func (m Model) hnMeta(it core.Item) string {
	segs := make([]string, 0, 3)
	if it.Points != nil {
		segs = append(segs, m.cat.Get("hn.pts", *it.Points))
	}
	if it.NumComments != nil {
		segs = append(segs, m.cat.Get("hn.comments", *it.NumComments))
	}
	if it.Author != "" {
		segs = append(segs, it.Author)
	}
	return strings.Join(segs, " · ")
}

// relTime renders a coarse relative age for the meta column; items
// without a date keep the column blank.
func (m Model) relTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	d := time.Since(*t)
	switch {
	case d < time.Minute:
		return m.cat.Get("time.now")
	case d < time.Hour:
		return m.cat.Get("time.minutes", int(d.Minutes()))
	case d < 24*time.Hour:
		return m.cat.Get("time.hours", int(d.Hours()))
	default:
		return m.cat.Get("time.days", int(d.Hours()/24))
	}
}
