package ui

// The line-assembly guard. Every line the detail layers produce is
// consumed by a fixed-width lipgloss List renderer: a line wider than
// the column gets re-wrapped one character over, breaking column
// alignment. Born from the comment-style P1 — deep reply rows laid
// out at the full width and then indented two spaces came out a
// column too wide. assertLinesFit turns that bug class red in any
// test that holds assembled lines.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/smy-101/readscope/internal/render"
)

// lineWidth is a line's display width in terminal columns (CJK and
// friends counted at their real width).
func lineWidth(ln render.Line) int {
	w := 0
	for _, s := range ln.Spans {
		w += runewidth.StringWidth(s.Text)
	}
	return w
}

// assertLinesFit fails when any line is wider than w columns. Wire it
// wherever lines are assembled for a fixed-width consumer: the detail
// body (d.lines), floorLines, replyLines.
func assertLinesFit(t *testing.T, lines []render.Line, w int) {
	t.Helper()
	for i, ln := range lines {
		if got := lineWidth(ln); got > w {
			var b strings.Builder
			for _, s := range ln.Spans {
				b.WriteString(s.Text)
			}
			t.Fatalf("line %d is %d cols wide, must fit the %d-col detail column: %q",
				i, got, w, b.String())
		}
	}
}

// The P1 class pin: deep reply rows lay out at width-2 before the
// two-space indent, so a body that wraps at the very edge still fits
// the column. Pre-fix the row came out at width+2 and lipgloss
// re-wrapped a character off it.
func TestDeepReplyWrapsInsideColumn(t *testing.T) {
	base, _ := hnDetailServer(t, hnDetailJSON())
	m := openHNDetail(t, nil, base)
	m = size(m, 100, 12)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // body → floor 1
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → floor 2 (the deep chain)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // → its reply layer
	if m.reader.layer != layerReplies {
		t.Fatalf("setup: reply layer did not open, layer=%v", m.reader.layer)
	}
	// Swap in bodies that wrap at the column edge — pure CJK for even
	// column boundaries, pure ASCII for odd ones — then re-lay the
	// layer. layoutReplies re-flattens the floor, so the edit lands
	// without a refetch.
	f := &m.reader.floors[m.reader.floorIdx]
	cjk := strings.Repeat("深", 80)
	ascii := strings.Repeat("a", 159)
	for i := range f.replies {
		if i%2 == 0 {
			f.replies[i].Text = cjk
		} else {
			f.replies[i].Text = ascii
		}
	}
	m.layoutReplies()
	assertLinesFit(t, m.reader.replyLines, m.detailWidth())
}
