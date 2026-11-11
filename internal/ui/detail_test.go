package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/smy-101/readscope/internal/core"
)

// Production side effects, captured at init so tests can stub and
// restore the seams without spawning browsers or writing clipboards.
var (
	browserOpenDefault = openBrowser
	oscCopyDefault     = copyClipboard
)

// richBody is a full-feature article snapshot: headings, CJK+Latin mix,
// a link (inline [n] + appendix), quote, code, standalone image.
const richBody = `<h2>排版第一节</h2>
<p>这是一段中文正文,夹着<a href="https://example.com/target">一个链接</a>与 English Go 混排,验证折行与禁则。</p>
<blockquote><p>好的折行算法应该让人感觉不到它的存在。</p></blockquote>
<pre><code class="language-go">func main() { fmt.Println("hi") }</code></pre>
<p><img src="https://example.com/cover.png" alt="封面"></p>`

// openRichDetail drives the model to the reader over the second of two
// items whose feed URL is a dead listener, proving the whole open path
// is offline.
func openRichDetail(t *testing.T) (Model, *bool) {
	t.Helper()
	published := time.Now().UTC().Add(-90 * time.Minute)
	url, dialed := deadFeedURL(t)
	m := seededModel(t,
		map[string]core.RSSFeed{"ex": {Label: "Example", URL: url}},
		map[string][]core.Item{"ex": {
			{ID: "rss:ex:1", Source: "rss", Feed: "ex", Title: "第一条", URL: "https://example.com/1",
				FetchedAt: time.Now().UTC(), Body: richBody},
			{ID: "rss:ex:2", Source: "rss", Feed: "ex", Title: "第二条", URL: "https://example.com/2",
				FetchedAt: time.Now().UTC(), PublishedAt: &published, Author: "作者", Body: richBody},
		}},
	)
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab}) // books → rss
	m = pump(t, m, cmd)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnter}) // sidebar → list focus
	// Move to the second item, then open it.
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.listSel != 1 {
		t.Fatalf("setup: listSel = %d, want 1", m.listSel)
	}
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("opening the detail should not kick a command")
	}
	if m.reader == nil {
		t.Fatal("Enter on the list must open the detail layer")
	}
	return m, dialed
}

func TestDetailRendersPipelineOffline(t *testing.T) {
	m, dialed := openRichDetail(t)

	view := m.View()
	// The list is covered: the other item's title must not show.
	if strings.Contains(view, "第一条") {
		t.Fatalf("the list should be covered by the detail layer:\n%s", view)
	}
	for _, want := range []string{
		"第二条",         // title heading
		"作者 · 1h",     // meta line (author · relative age)
		"排版第一节",       // h2 from the body
		"一个链接 [1]",    // inline link marker
		"│ 好的折行算法",    // quote bar
		"func main()", // code block
		"[图片:封面]",     // image degradation
		"English Go",  // CJK+Latin mix
	} {
		if !strings.Contains(view, want) {
			t.Errorf("detail view missing %q:\n%s", want, view)
		}
	}
	// Status bar: reading position + title.
	status := strings.Split(view, "\n")[m.height-1]
	if !strings.Contains(status, "第二条") || !strings.Contains(status, "%") {
		t.Errorf("status bar should show title + percent: %q", status)
	}
	if *dialed {
		t.Fatal("opening the detail dialed the network — the body is a cache snapshot")
	}

	// Scroll to the end: the link appendix is listed there.
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnd})
	view = m.View()
	if !strings.Contains(view, "[1] https://example.com/target") {
		t.Errorf("link appendix missing at the end of the article:\n%s", view)
	}
	if !strings.Contains(view, "100%") {
		t.Errorf("status should read 100%% at the end: %q", strings.Split(view, "\n")[m.height-1])
	}
}

func TestDetailEscReturnsToListWithCursor(t *testing.T) {
	m, _ := openRichDetail(t)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnd}) // scroll somewhere
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.reader != nil {
		t.Fatal("Esc must close the detail layer")
	}
	if m.focus != regionList {
		t.Fatalf("focus must return to the list, got %v", m.focus)
	}
	if m.listSel != 1 {
		t.Fatalf("cursor must survive the round trip: listSel = %d", m.listSel)
	}
	view := m.View()
	if !strings.Contains(view, "第一条") || !strings.Contains(view, "第二条") {
		t.Fatalf("the list must be back after Esc:\n%s", view)
	}

	// Reopening starts fresh at the top with the same item.
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.reader == nil || m.reader.top != 0 {
		t.Fatalf("reopen should reset scroll: reader=%v top=%d", m.reader, m.reader.top)
	}
	if m.reader.item.Item.Title != "第二条" {
		t.Fatalf("reopen keeps the selected item, got %q", m.reader.item.Item.Title)
	}
}

func TestDetailScrollClamps(t *testing.T) {
	var b strings.Builder
	for i := range 80 {
		fmt.Fprintf(&b, "<p>第 %d 段:这一段中文正文足够长,折行后占据两行,让文章超过一屏,验证滚动窗口的钳制。</p>\n", i)
	}
	m := seededModel(t,
		map[string]core.RSSFeed{"long": {Label: "Long", URL: "http://127.0.0.1:1/x"}},
		map[string][]core.Item{"long": {{
			ID: "rss:long:1", Source: "rss", Feed: "long", Title: "长文",
			FetchedAt: time.Now().UTC(), Body: b.String(),
		}}},
	)
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
	m = pump(t, m, cmd)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnter}) // sidebar → list
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnter}) // open
	if m.reader == nil {
		t.Fatal("detail did not open")
	}
	total := len(m.reader.lines)
	if total <= m.bodyHeight() {
		t.Fatalf("setup: article too short to scroll (%d lines)", total)
	}
	maxTop := total - m.bodyHeight()

	m = updateKey(m, tea.KeyMsg{Type: tea.KeyUp}) // clamps at the top
	if m.reader.top != 0 {
		t.Fatalf("up at top must clamp, top = %d", m.reader.top)
	}
	for range 40 { // hammer pgdown far past the end
		m = updateKey(m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if m.reader.top != maxTop {
		t.Fatalf("pgdown must clamp at the last page: top = %d, want %d", m.reader.top, maxTop)
	}
	if !strings.Contains(m.View(), "第 79 段") {
		t.Fatalf("last paragraph should be on screen at the bottom:\n%s", m.View())
	}
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyHome})
	if m.reader.top != 0 {
		t.Fatalf("home must jump to the top, top = %d", m.reader.top)
	}
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnd})
	if m.reader.top != maxTop {
		t.Fatalf("end must jump to the last page: top = %d, want %d", m.reader.top, maxTop)
	}
}

func TestDetailResizeRelayouts(t *testing.T) {
	m, _ := openRichDetail(t)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnd}) // read to the bottom
	wideLines := len(m.reader.lines)

	m = size(m, 56, 20) // narrow + short
	if m.reader == nil {
		t.Fatal("resize must not close the reader")
	}
	narrowLines := len(m.reader.lines)
	if narrowLines <= wideLines {
		t.Fatalf("narrower terminal must re-wrap into more lines: %d → %d", wideLines, narrowLines)
	}
	maxTop := narrowLines - m.bodyHeight()
	if m.reader.top > maxTop {
		t.Fatalf("position must clamp after re-wrap: top = %d, max = %d", m.reader.top, maxTop)
	}
	for i, ln := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(ln); w > 56 {
			t.Fatalf("line %d overflows after resize: %d > 56: %q", i, w, ln)
		}
	}
	if !strings.Contains(m.View(), "第二条") { // header still readable
		t.Fatalf("title should survive the resize:\n%s", m.View())
	}

	// Growing back re-wraps again without losing the reader.
	m = size(m, 100, 30)
	if m.reader == nil || len(m.reader.lines) != wideLines {
		t.Fatalf("resize back must restore the wide layout: %d ≠ %d", len(m.reader.lines), wideLines)
	}
}

func TestDetailPlainBodyKeepsLines(t *testing.T) {
	long := "这一行纯文本很长很长很长很长很长很长很长很长很长很长很长很长很长,必须在窄处折成两行而不是溢出。"
	open := func(t *testing.T, body string) Model {
		t.Helper()
		m := seededModel(t,
			map[string]core.RSSFeed{"plain": {Label: "Plain", URL: "http://127.0.0.1:1/x"}},
			map[string][]core.Item{"plain": {{
				ID: "rss:plain:1", Source: "rss", Feed: "plain", Title: "纯文本正文",
				FetchedAt: time.Now().UTC(), Body: body,
			}}},
		)
		m = size(m, 60, 24)
		var cmd tea.Cmd
		m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
		m = pump(t, m, cmd)
		m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnter})
		m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnter})
		if m.reader == nil {
			t.Fatal("detail did not open")
		}
		return m
	}

	m := open(t, "第一行内容\n第二行内容\n\n第三段在空行之后。")
	view := m.View()
	for _, want := range []string{"第一行内容", "第二行内容", "第三段在空行之后。"} {
		if !strings.Contains(view, want) {
			t.Errorf("plain body line %q missing:\n%s", want, view)
		}
	}
	// No links appendix for linkless bodies.
	if strings.Contains(view, "[1] http") {
		t.Errorf("plain body must not grow a link appendix:\n%s", view)
	}

	// A long plain line wraps instead of overflowing.
	m2 := open(t, long)
	for i, ln := range strings.Split(m2.View(), "\n") {
		if w := lipgloss.Width(ln); w > 60 {
			t.Fatalf("plain long line overflows: line %d width %d > 60: %q", i, w, ln)
		}
	}
}

func TestCtrlOAndCtrlY(t *testing.T) {
	t.Cleanup(func() {
		openBrowser = browserOpenDefault
		copyClipboard = oscCopyDefault
	})
	m, _ := openRichDetail(t)

	// Ctrl+O: happy path.
	var openedURL string
	openBrowser = func(url string) error { openedURL = url; return nil }
	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	m = pump(t, m, cmd)
	if openedURL != "https://example.com/2" {
		t.Fatalf("Ctrl+O should open the item URL, got %q", openedURL)
	}
	if !strings.Contains(m.status, "opened in browser") {
		t.Fatalf("status should confirm the browser open: %q", m.status)
	}

	// Ctrl+O: failure surfaces in the status bar.
	openBrowser = func(string) error { return errors.New("no xdg-open") }
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	m = pump(t, m, cmd)
	if !strings.Contains(m.status, "cannot open browser") {
		t.Fatalf("status should surface the browser error: %q", m.status)
	}

	// Ctrl+Y: clipboard gets the URL, status confirms.
	var copiedURL string
	copyClipboard = func(s string) error { copiedURL = s; return nil }
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyCtrlY})
	m = pump(t, m, cmd)
	if copiedURL != "https://example.com/2" {
		t.Fatalf("Ctrl+Y should copy the item URL, got %q", copiedURL)
	}
	if !strings.Contains(m.status, "link copied") {
		t.Fatalf("status should confirm the copy: %q", m.status)
	}

	// Ctrl+Y: failure surfaces too.
	copyClipboard = func(string) error { return errors.New("clip broken") }
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyCtrlY})
	m = pump(t, m, cmd)
	if !strings.Contains(m.status, "copy failed") {
		t.Fatalf("status should surface the copy error: %q", m.status)
	}
}

func TestCtrlYWithoutURL(t *testing.T) {
	m := seededModel(t,
		map[string]core.RSSFeed{"nourl": {Label: "NoURL", URL: "http://127.0.0.1:1/x"}},
		map[string][]core.Item{"nourl": {{
			ID: "rss:nourl:1", Source: "rss", Feed: "nourl", Title: "无链接条目",
			FetchedAt: time.Now().UTC(), Body: "正文",
		}}},
	)
	m = size(m, 100, 30)
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
	m = pump(t, m, cmd)
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = updateKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	called := false
	copyClipboard = func(string) error { called = true; return nil }
	t.Cleanup(func() { copyClipboard = oscCopyDefault })

	m = updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlY})
	if called {
		t.Fatal("no clipboard write for an item without a URL")
	}
	if !strings.Contains(m.status, "no URL") {
		t.Fatalf("status should explain there is no link: %q", m.status)
	}
}
