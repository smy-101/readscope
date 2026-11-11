// List rendering for the active feed's cached items: the row layout
// (unread dot · title · relative time), the truncation math that keeps
// a narrow terminal intact, and the clamped cursor + scroll window.
// Moved here from app.go so the shell model changes for shell reasons
// and the list changes for list reasons (plan package layout: list.go).
package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
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
	metaW := 10     // room for "45 分钟前"
	if usable < metaW+5 {
		metaW = 0 // too narrow: drop the time column before the title
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
		// Marker column: ★ starred (star wins the slot), ● unread,
		// blank read. The title dims on read state only — a star never
		// dims.
		dot, titleStyle := " ", m.th.TitleRead
		if row.Starred {
			dot = m.th.UnreadDot.Render("★")
		} else if !row.Read {
			dot = m.th.UnreadDot.Render("●")
		}
		if !row.Read {
			titleStyle = m.th.Title
		}
		title := runewidth.FillRight(runewidth.Truncate(row.Item.Title, titleW, "…"), titleW)
		line := dot + " " + titleStyle.Render(title)
		if metaW > 0 {
			meta := runewidth.FillLeft(m.relTime(row.Item.PublishedAt), metaW)
			line += " " + m.th.Meta.Render(meta)
		}
		if i == m.listSel {
			line = m.th.ListSelected.Render(lipgloss.NewStyle().Width(usable).Render(line))
		}
		lines = append(lines, line)
	}
	return m.th.List.Width(w).Height(h).Render(strings.Join(lines, "\n"))
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
