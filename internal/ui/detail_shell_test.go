package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/smy-101/readscope/internal/core"
)

// Reading, then Tab: the reader closes with the old source's list; the
// shell switches sources cleanly underneath.
func TestTabWhileReadingClosesDetail(t *testing.T) {
	m, _ := openRichDetail(t)
	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // rss → hn mid-read
	m = pump(t, m, cmd)
	if m.reader != nil {
		t.Fatal("switching source must close the detail layer")
	}
	view := m.View()
	if strings.Contains(view, "一个链接") {
		t.Fatalf("the article must be gone after the source switch:\n%s", view)
	}
	if !strings.Contains(view, "Hacker News") {
		t.Fatalf("source bar should show hn active:\n%s", view)
	}
}

// F5 while reading: the refresh runs, the reader stays on its snapshot.
func TestF5WhileReadingKeepsReader(t *testing.T) {
	published := time.Now().UTC().Add(-time.Hour)
	m := seededModel(t,
		map[string]core.RSSFeed{"ex": {Label: "Example", URL: "http://127.0.0.1:1/x"}},
		map[string][]core.Item{"ex": {{
			ID: "rss:ex:1", Source: "rss", Feed: "ex", Title: "Snapshot story",
			URL: "https://example.com/1", PublishedAt: &published,
			FetchedAt: time.Now().UTC(), Body: "<p>cached body</p>",
		}}},
	)
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
	m = pump(t, m, cmd)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnd}) // read to the end
	topBefore := m.reader.top

	// F5 falls through to the shared handler while reading.
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyF5})
	if !m.refreshing {
		t.Fatal("F5 should start a refresh even while reading")
	}
	res, cmd2 := m.Update(refreshDoneMsg{source: "rss", results: nil})
	m = pump(t, res.(Model), cmd2)
	if m.reader == nil {
		t.Fatal("a landed refresh must not close the reader")
	}
	if m.reader.top != topBefore || m.reader.item.Item.Title != "Snapshot story" {
		t.Fatal("the reading session must be untouched by the refresh")
	}
	if !strings.Contains(m.View(), "cached body") || !strings.Contains(m.View(), "Snapshot story") {
		t.Fatalf("the reader should still show the snapshot:\n%s", m.View())
	}
}
