package ui

// Ticket 03 UI tests for the heybox post's two-layer detail: Enter on
// a post card opens the body layer with the comment-count footer,
// Enter again drops into the floors layer (root comments one per
// screen, the pager's floor-head triple), Enter on a floor opens the
// flattened 楼中楼 with @parent on second-level rows, Esc walks the
// stack back one layer at a time, Ctrl+N at the floors tail pages by
// offset, and a zero-comment post prompts without a single sub dial.
// The Ctrl+O door is the share_url on every layer.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source/heybox"
)

// postPlane serves one heybox post's detail doors: the tree at two
// offsets (total_page 2) and one floor's 楼中楼. Floor 101 carries a
// hot preview chain (direct sub + sub-of-sub, the @parent case) with
// its child_num already served; floor 102 is childless; page 2 adds
// floor 103.
type postPlane struct {
	mu      sync.Mutex
	treeGot []string // offsets
	subGot  int
}

func (p *postPlane) tree(offset string) string {
	// group builds one floor group: the root plus optional inline
	// sub-comments (the hot previews).
	group := func(id int, kids int, subs string) string {
		return fmt.Sprintf(`{"comment":[
		 {"commentid":%d,"userid":7,"create_at":1794386229,"text":"楼 %d 的正文[cube_滑稽]","child_num":%d,"up":%d,
		  "floor_num":%d,"user":{"username":"楼长%d"}}%s]}`,
			id, id, kids, id, id, id, subs)
	}
	switch offset {
	case "0":
		subs := `,
			 {"commentid":5101,"userid":8,"create_at":1794386229,"text":"直接回复","child_num":0,"up":0,
			  "replyid":101,"user":{"username":"层客甲"}},
			 {"commentid":6101,"userid":9,"create_at":1794386229,"text":"二层回复","child_num":0,"up":0,
			  "replyid":5101,"user":{"username":"层客乙"}}`
		return fmt.Sprintf(`{"msg":"","status":"ok","result":{"link":{"text":"帖子全文[cube_滑稽]",
			"share_url":"https://api.xiaoheihe.cn/v3/bbs/app/api/web/share?link_id=aaaa00000001"},
		 "comments":[%s,%s],"total_page":2}}`,
			group(101, 2, subs), group(102, 0, ""))
	default: // offset 1
		return fmt.Sprintf(`{"msg":"","status":"ok","result":{"link":{"text":"…"},
		 "comments":[%s],"total_page":2}}`, group(103, 0, ""))
	}
}

func (p *postPlane) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bbs/app/link/tree":
			p.mu.Lock()
			p.treeGot = append(p.treeGot, r.URL.Query().Get("offset"))
			p.mu.Unlock()
			fmt.Fprint(w, p.tree(r.URL.Query().Get("offset")))
		case "/bbs/app/comment/sub/comments":
			p.mu.Lock()
			p.subGot++
			p.mu.Unlock()
			fmt.Fprint(w, `{"msg":"","status":"ok","result":{"has_more":false,"lastval":0,"comments":[]}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// openPost seeds one post card (comment_num n), lands the list on
// heybox, and opens the reader (the open chain pumped to quiescence
// — cache probe plus the background detail fetch).
func openPost(t *testing.T, srv *httptest.Server, n int) Model {
	t.Helper()
	m := seededModel(t, nil, nil)
	t.Setenv("READSCOPE_HEYBOX_BASE", srv.URL)
	t.Setenv("READSCOPE_HEYBOX_MINGAP", "0")
	t.Setenv("READSCOPE_HEYBOX_DEVICE", "0123456789abcdef0123456789abcdef")
	if _, _, err := m.st.SaveFeedItems(context.Background(), "heybox", "recommend", []core.Item{{
		ID: heybox.ItemID("aaaa00000001"), Source: "heybox", Feed: "recommend",
		Title: "测试帖子", Author: "楼主",
		URL:         "https://api.xiaoheihe.cn/v3/bbs/app/api/web/share?link_id=aaaa00000001",
		Body:        "列表摘要",
		NumComments: &n,
		FetchedAt:   time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("seed post: %v", err)
	}
	m = size(m, 120, 40)
	for range 4 { // books → rss → hn → zhihu → heybox
		var cmd tea.Cmd
		m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
		m = pump(t, m, cmd)
	}
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // sidebar → list
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // open the post
	m = pump(t, m, cmd)
	if m.reader == nil {
		t.Fatal("Enter on the post card must open the reader")
	}
	return m
}

// The post layer: the fetched body renders (cube degraded), the
// footer counts to the server's own comment total, and no floor list
// sits inline under the body (the floors live one layer down).
func TestHeyboxPostLayerRenders(t *testing.T) {
	p := &postPlane{}
	m := openPost(t, p.server(t), 18)

	if m.reader.layer != layerBody {
		t.Fatalf("open lands on the post layer, got %v", m.reader.layer)
	}
	if !m.reader.post || m.reader.article {
		t.Fatal("a heybox post's open is post-shaped, never article-shaped")
	}
	view := m.View()
	if !strings.Contains(view, "帖子全文[滑稽]") {
		t.Errorf("post layer missing the degraded body:\n%s", view)
	}
	if !strings.Contains(view, "18 comments") {
		t.Errorf("footer must count to the server total (18 comments):\n%s", view)
	}
	if strings.Contains(view, "楼长101") {
		t.Errorf("the floor list must not render inline under a post body:\n%s", view)
	}
	// Ctrl+O from the body: the share_url.
	if got := m.detailTargetURL(); got != "https://api.xiaoheihe.cn/v3/bbs/app/api/web/share?link_id=aaaa00000001" {
		t.Errorf("body Ctrl+O target = %q, want the share_url", got)
	}
	// Ctrl+N at the body layer pages nothing.
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyCtrlN})
	m = pump(t, m, cmd)
	if m.reader.layer != layerBody || m.status != m.cat.Get("status.ready") && m.status != "" {
		t.Errorf("body Ctrl+N must be a no-op (layer %v, status %q)", m.reader.layer, m.status)
	}
}

// The two-layer navigation: Enter drops into the floors layer (one
// root per screen, the pager floor-head triple 赞 N · N 条评论),
// Enter on a floor opens the flattened 楼中楼 (@parent on the
// second level), Esc walks back one layer at a time, and each
// layer's Ctrl+O is the share_url.
func TestHeyboxTwoLayerNavigation(t *testing.T) {
	p := &postPlane{}
	m := openPost(t, p.server(t), 18)

	// Enter: into the floors.
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.reader.layer != layerFloors || m.reader.floorIdx != 0 {
		t.Fatalf("Enter from the post body must open floor 0, got layer %v idx %d", m.reader.layer, m.reader.floorIdx)
	}
	view := m.View()
	for _, want := range []string{"楼长101", "楼 101 的正文[滑稽]", "2 comments"} {
		if !strings.Contains(view, want) {
			t.Errorf("floor 0 missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "101 upvotes") {
		t.Errorf("floor 0 header missing the 赞 segment:\n%s", view)
	}
	if got := m.detailTargetURL(); !strings.Contains(got, "link_id=aaaa00000001") {
		t.Errorf("floors Ctrl+O target = %q, want the share_url", got)
	}

	// → pinned at the loaded tail while a page remains: the heybox
	// hint names floors, not answers.
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyRight})
	m = pump(t, m, cmd)
	if m.reader.floorIdx != 1 || m.status != m.cat.Get("heybox.floors_tail") {
		t.Fatalf("→ at the tail = idx %d status %q, want the floors-tail hint", m.reader.floorIdx, m.status)
	}
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyLeft}) // back to floor 101
	m = pump(t, m, cmd)

	// Enter: into floor 101's 楼中楼 — the hot preview chain, the
	// sub-of-sub carrying @parent.
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.reader.layer != layerReplies {
		t.Fatalf("Enter on a floor with replies must open the 楼中楼, got %v", m.reader.layer)
	}
	view = m.View()
	if !strings.Contains(view, "直接回复") || !strings.Contains(view, "二层回复") {
		t.Errorf("楼中楼 missing its rows:\n%s", view)
	}
	if !strings.Contains(view, "@层客甲") {
		t.Errorf("the second-level row must carry @parent:\n%s", view)
	}
	if got := m.detailTargetURL(); !strings.Contains(got, "link_id=aaaa00000001") {
		t.Errorf("replies Ctrl+O target = %q, want the share_url", got)
	}

	// Esc: one layer at a time — replies → floors → body → list.
	for _, want := range []detailLayer{layerFloors, layerBody} {
		m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEsc})
		m = pump(t, m, cmd)
		if m.reader == nil || m.reader.layer != want {
			t.Fatalf("Esc must step back to %v, got %v", want, m.reader)
		}
	}
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = pump(t, m, cmd)
	if m.reader != nil {
		t.Fatal("the last Esc must close the reader")
	}

	// A childless floor refuses its reply layer with the no-replies
	// line (floor 102: child_num 0, the zero-request promise).
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // reopen (cached, offline)
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // body → floors
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyRight}) // floor 101 → 102
	m = pump(t, m, cmd)
	if m.reader.floorIdx != 1 {
		t.Fatalf("→ must move to floor 102, got idx %d", m.reader.floorIdx)
	}
	p.mu.Lock()
	subs := p.subGot
	p.mu.Unlock()
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.reader.layer != layerFloors || m.status != m.cat.Get("detail.no_replies") {
		t.Fatalf("childless floor Enter = layer %v status %q, want the no-replies refusal", m.reader.layer, m.status)
	}
	p.mu.Lock()
	if p.subGot != subs {
		t.Error("a childless floor's Enter must dial nothing (child_num 0 read off the cache)")
	}
	p.mu.Unlock()
}

// The floors walk: Ctrl+N at the loaded tail fetches the next offset
// page in place (the status counts floors the heybox way), and the
// second Ctrl+N at the walk's end says no-more.
func TestHeyboxFloorsPaging(t *testing.T) {
	p := &postPlane{}
	m := openPost(t, p.server(t), 18)

	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // body → floors
	m = pump(t, m, cmd)
	if len(m.reader.floors) != 2 {
		t.Fatalf("floors after open = %d, want 2", len(m.reader.floors))
	}
	if m.reader.feedsCursor != "1" {
		t.Fatalf("feedsCursor = %q, want 1 (total_page 2)", m.reader.feedsCursor)
	}

	// Ctrl+N mid-list hops in memory (floor 0 → 1); the Ctrl+N at the
	// loaded tail fetches page 2 in place — the hop-then-fetch the
	// zhihu answer layer rides (ticket 10).
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyCtrlN})
	m = pump(t, m, cmd)
	if m.reader.floorIdx != 1 || len(m.reader.floors) != 2 {
		t.Fatalf("mid-list Ctrl+N = idx %d of %d floors, want the in-memory hop to idx 1", m.reader.floorIdx, len(m.reader.floors))
	}
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyCtrlN})
	m = pump(t, m, cmd)
	if len(m.reader.floors) != 3 || m.reader.floors[2].cm.ID != "103" {
		t.Fatalf("Ctrl+N floors = %+v, want 103 appended", m.reader.floors)
	}
	if m.status != m.cat.Get("heybox.floors_loaded", 3) {
		t.Errorf("status = %q, want the floors-loaded line", m.status)
	}
	p.mu.Lock()
	trees := append([]string(nil), p.treeGot...)
	p.mu.Unlock()
	if len(trees) != 2 || trees[1] != "1" {
		t.Fatalf("tree dials = %v, want offset 1 paged in", trees)
	}

	// → at the (now exhausted) tail: the no-more line, not a fetch.
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyRight})
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyCtrlN})
	m = pump(t, m, cmd)
	if m.status != m.cat.Get("status.more_end") {
		t.Errorf("status = %q, want the no-more line", m.status)
	}
}

// A zero-comment post: the post layer's footer says no comments, and
// Enter lands on the no-comments line — with the detail fetched and
// cached, the re-open renders offline (the server down).
func TestHeyboxZeroCommentPost(t *testing.T) {
	p := &postPlane{}
	srv := p.server(t)
	empty := `{"msg":"","status":"ok","result":{"link":{"text":"无评论帖","share_url":"https://api.xiaoheihe.cn/v3/bbs/app/api/web/share?link_id=aaaa00000001"},
	 "comments":[],"total_page":1}}`
	// Serve the empty tree once, then die: the re-open must ride the
	// cache alone.
	var opens int
	var mu sync.Mutex
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		opens++
		n := opens
		mu.Unlock()
		if n > 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, empty)
	})

	m := openPost(t, srv, 0)
	if m.reader.layer != layerBody {
		t.Fatal("open lands on the post layer")
	}
	if footer := m.commentsFooter(); footer != m.cat.Get("detail.no_comments") {
		t.Errorf("footer = %q, want the no-comments line", footer)
	}
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // into the (empty) floors
	m = pump(t, m, cmd)
	if m.reader.layer != layerFloors {
		t.Fatalf("Enter must land on the comment layer, got %v", m.reader.layer)
	}
	if view := m.View(); !strings.Contains(view, m.cat.Get("detail.no_comments")) {
		t.Errorf("the comment layer must prompt no-comments directly:\n%s", view)
	}
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = pump(t, m, cmd)

	// Re-open with the host dead: the cached tree renders, zero dials
	// answered, the reader survives (the opened marker's promise).
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.reader == nil || m.reader.layer != layerBody {
		t.Fatal("the re-open must render from cache")
	}
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.reader == nil || m.reader.layer != layerFloors {
		t.Fatalf("the cached empty comment layer must open offline, got %v", m.reader)
	}
}

// A risk-control refusal on the first open: the 稍后再试 line, the
// reader stays readable (the body renders), nothing crashes.
func TestHeyboxDetailRiskRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"msg":"","status":"show_captcha","result":{}}`)
	}))
	t.Cleanup(srv.Close)

	m := seededModel(t, nil, nil)
	t.Setenv("READSCOPE_HEYBOX_BASE", srv.URL)
	t.Setenv("READSCOPE_HEYBOX_MINGAP", "0")
	t.Setenv("READSCOPE_HEYBOX_DEVICE", "0123456789abcdef0123456789abcdef")
	if _, _, err := m.st.SaveFeedItems(context.Background(), "heybox", "recommend", []core.Item{{
		ID: heybox.ItemID("aaaa00000001"), Source: "heybox", Feed: "recommend",
		Title: "风控帖", URL: "https://api.xiaoheihe.cn/v3/bbs/app/api/web/share?link_id=aaaa00000001",
		Body: "缓存正文", FetchedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	m = size(m, 120, 40)
	for range 4 {
		var cmd tea.Cmd
		m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
		m = pump(t, m, cmd)
	}
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.reader == nil {
		t.Fatal("the reader must open even when the detail fetch is refused")
	}
	if m.status != m.cat.Get("heybox.risk_later") {
		t.Errorf("status = %q, want the try-later line", m.status)
	}
	if !strings.Contains(m.View(), "缓存正文") {
		t.Error("the cached body must keep rendering through the refusal")
	}
}
