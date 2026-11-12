package ui

// Ticket 06 UI tests: the zhihu question's three detail layers over
// the real Update loop — the question layer (meta triple, cleaned
// description, answer floor list, load-more footer), the answer layer
// (one answer per screen, ←→ between answers), the comment layer
// (roots as rows, children flattened with @parent), the per-layer
// Ctrl+N semantics (question = feeds cursor, answer = in-memory hop,
// comments = offset), the failure translations (404 / session), and
// the offline re-open rendering the cache with zero requests. The
// fake plane mirrors the measured matrix; the real-network run is
// ticket 07's smoke.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/smy-101/readscope/internal/core"

	_ "github.com/smy-101/readscope/internal/source/zhihu" // register "zhihu", as main does
)

// qPlane is the question's whole data plane behind one fake server.
type qPlane struct {
	mu     chan struct{} // 1-buffered mutex stand-in for hit counting
	hits   int
	status int // ≠0: the v4 family answers this (404/403 fixtures)
}

func (p *qPlane) count() int {
	<-p.mu
	n := p.hits
	p.mu <- struct{}{}
	return n
}

func (p *qPlane) hit() {
	<-p.mu
	p.hits++
	p.mu <- struct{}{}
}

// zhihuPlane serves one question: meta (问主 · 3 answers · 5.6M
// visits · html description), two answers pages by cursor (甲/乙,
// then 丙), and answer 9001's comments at two offsets (根一 with a
// gap-filled child, 根二; then 末根).
func zhihuPlane(t *testing.T) (*httptest.Server, *qPlane) {
	t.Helper()
	p := &qPlane{mu: make(chan struct{}, 1)}
	p.mu <- struct{}{}
	gate := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Cookie") == "" {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"error":{"code":100}}`)
			return false
		}
		return true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/questions/1001", func(w http.ResponseWriter, r *http.Request) {
		p.hit()
		if !gate(w, r) || p.status != 0 {
			if p.status != 0 {
				w.WriteHeader(p.status)
				fmt.Fprint(w, `{"error":{"code":1}}`)
			}
			return
		}
		fmt.Fprint(w, `{"title":"问题","author":{"name":"问主"},"answer_count":3,"visit_count":5600000,
		 "detail":"<p>描述内容</p><noscript><img src=\"https://pic.zhimg.com/80/x.jpg\"></noscript>"}`)
	})
	mux.HandleFunc("/api/v4/questions/1001/feeds", func(w http.ResponseWriter, r *http.Request) {
		p.hit()
		if !gate(w, r) || p.status != 0 {
			if p.status != 0 {
				w.WriteHeader(p.status)
				fmt.Fprint(w, `{"error":{"code":1}}`)
			}
			return
		}
		if r.URL.Query().Get("cursor") == "" {
			fmt.Fprint(w, `{"data":[
			 {"target":{"id":9001,"type":"answer","author":{"name":"甲"},"voteup_count":12000,
			  "comment_count":2,"content":"<p>答一内容</p>","created_time":1700000000}},
			 {"target":{"id":9002,"type":"answer","author":{"name":"乙"},"voteup_count":0,
			  "comment_count":0,"content":"<p>答二内容</p>"}}],
			 "paging":{"next":"http://`+r.Host+`/api/v4/questions/1001/feeds?cursor=p2","is_end":false}}`)
			return
		}
		fmt.Fprint(w, `{"data":[
		 {"target":{"id":9003,"type":"answer","author":{"name":"丙"},"voteup_count":1,
		  "comment_count":0,"content":"<p>答三内容</p>"}}],
		 "paging":{"next":"","is_end":true}}`)
	})
	mux.HandleFunc("/api/v4/comment_v5/answers/9001/root_comment", func(w http.ResponseWriter, r *http.Request) {
		p.hit()
		if !gate(w, r) || p.status != 0 {
			if p.status != 0 {
				w.WriteHeader(p.status)
				fmt.Fprint(w, `{"error":{"code":1}}`)
			}
			return
		}
		if r.URL.Query().Get("offset") == "20" {
			fmt.Fprint(w, `{"data":[{"id":703,"author":{"name":"末根"},"like_count":0,"content":"末页根"}],
			 "paging":{"next":"","is_end":true}}`)
			return
		}
		fmt.Fprint(w, `{"data":[
		 {"id":701,"author":{"name":"根一"},"like_count":3,"content":"根一内容",
		  "child_comment_count":1,"child_comments":[]},
		 {"id":702,"author":{"name":"根二"},"like_count":0,"content":"根二内容"}],
		 "paging":{"next":"http://`+r.Host+`/api/v4/comment_v5/answers/9001/root_comment?order_by=score&limit=20&offset=20","is_end":false}}`)
	})
	mux.HandleFunc("/api/v4/comment_v5/comment/701/child_comment", func(w http.ResponseWriter, r *http.Request) {
		p.hit()
		if !gate(w, r) {
			return
		}
		fmt.Fprint(w, `{"data":[{"id":801,"author":{"name":"子回复"},"like_count":0,"content":"<p>子回复内容</p>"}],
		 "paging":{"next":"","is_end":true}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, p
}

// openQuestion lands the reader on the zhihu question layer, pumps
// the open chain, and returns the model ready for key asserts.
func openQuestion(t *testing.T, srv *httptest.Server) Model {
	t.Helper()
	m := seededModel(t, nil, nil) // sets READSCOPE_CONFIG to a temp dir
	if err := os.WriteFile(filepath.Join(os.Getenv("READSCOPE_CONFIG"), "zhihu.cookies"),
		[]byte("z_c0=live; d_c0=dev"), 0o600); err != nil {
		t.Fatalf("write cookie: %v", err)
	}
	t.Setenv("READSCOPE_ZHIHU_BASE", srv.URL)

	if _, _, err := m.st.SaveFeedItems(context.Background(), "zhihu", "hot", []core.Item{{
		ID: "zh:question/1001", Source: "zhihu", Feed: "hot",
		Title: "问题", URL: "https://www.zhihu.com/question/1001",
		FetchedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("seed question: %v", err)
	}
	m = size(m, 120, 40)
	for range 3 { // books → rss → hn → zhihu
		var cmd tea.Cmd
		m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
		m = pump(t, m, cmd)
	}
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // sidebar → list
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // open the question
	m = pump(t, m, cmd)
	if m.reader == nil {
		t.Fatal("Enter on the question row must open the reader")
	}
	return m
}

// The question layer: the meta triple (asked · answers · views), the
// CLEANED description (noscript gone), the answer floor list with
// floor-head triples (万-folded 赞, zero segments dropped), and the
// load-more footer counting to the server's total.
func TestZhihuQuestionLayerRenders(t *testing.T) {
	srv, _ := zhihuPlane(t)
	m := openQuestion(t, srv)

	if m.reader.layer != layerBody {
		t.Fatalf("open lands on the question layer, got %v", m.reader.layer)
	}
	view := m.View()
	for _, want := range []string{
		"问主 asked",    // zhihu.asked
		"3 answers",   // the meta triple's count (and the footer's total)
		"5.6M views",  // the visits fold
		"描述内容",        // the description through CleanHTML
		"甲",           // floor header: author
		"12K upvotes", // 万/K fold of 12000
		"2 comments",  // the floor-head third segment
		"乙",           // floor two
		"Ctrl+N more", // the footer's load-more hint (2/3 answers)
	} {
		if !strings.Contains(view, want) {
			t.Errorf("question layer missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "noscript") || strings.Contains(view, "zhimg.com/80") {
		t.Errorf("the description must ride CleanHTML:\n%s", view)
	}
	// 乙's zero segments drop entirely (无赞不占位).
	if i, j := strings.Index(view, "乙"), strings.Index(view, "0 upvotes"); i >= 0 && j > i && j-i < 40 {
		t.Errorf("a zero-赞 floor must not render a 0-upvotes segment:\n%s", view)
	}
}

// The four-layer navigation: question → answer (single answer, ←→
// between answers) → comments (roots as rows, the flattened child
// with @parent, comments_of header) → Esc walks back one layer at a
// time, each layer's scroll kept independently.
func TestZhihuFourLayerNavigation(t *testing.T) {
	srv, _ := zhihuPlane(t)
	m := openQuestion(t, srv)

	// Enter: question → the answer layer, on answer 甲.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.reader.layer != layerFloors {
		t.Fatalf("Enter should open the answer layer, got %v", m.reader.layer)
	}
	view := m.View()
	if !strings.Contains(view, "答一内容") || !strings.Contains(view, "甲") {
		t.Fatalf("answer layer should render 甲's full answer:\n%s", view)
	}

	// Scroll the answer layer, then enter the comments — coming back
	// must restore the answer's scroll (各层滚动状态独立保留).
	m = key(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	floorTop := m.reader.floorTop

	// Enter: answer → the comment layer (fetch on first entry).
	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.reader.layer != layerReplies {
		t.Fatalf("Enter should open the comment layer, got %v", m.reader.layer)
	}
	view = m.View()
	for _, want := range []string{"comments on 甲", "根一内容", "根二内容", "子回复内容", "@根一"} {
		if !strings.Contains(view, want) {
			t.Errorf("comment layer missing %q:\n%s", want, view)
		}
	}

	// Esc: back to the answer layer, scroll restored.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.reader.layer != layerFloors || m.reader.floorTop != floorTop {
		t.Fatalf("Esc should return to the answer layer with its scroll (top=%d want %d)",
			m.reader.floorTop, floorTop)
	}

	// →: the next answer, from its top.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight})
	view = m.View()
	if !strings.Contains(view, "答二内容") || m.reader.floorTop != 0 {
		t.Fatalf("→ should move to 乙's answer from the top:\n%s", view)
	}

	// ←: back to 甲, Esc ×2: answer → question → list.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.reader.layer != layerBody {
		t.Fatalf("Esc from answers should reach the question layer, got %v", m.reader.layer)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.reader != nil {
		t.Fatal("the last Esc should return to the list")
	}
}

// Ctrl+N per layer: the question layer walks the feeds cursor (page
// two lands, the total closes the walk), the answer layer hops in
// memory, the comment layer hops its loaded rows then pages by offset.
func TestZhihuCtrlNPerLayer(t *testing.T) {
	srv, _ := zhihuPlane(t)
	m := openQuestion(t, srv)

	// Question layer: page two (丙) appends, footer closes at the total.
	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyCtrlN})
	m = pump(t, m, cmd)
	view := m.View()
	if !strings.Contains(view, "丙") || !strings.Contains(view, "3 answers") {
		t.Fatalf("Ctrl+N should load page two and close at the total:\n%s", view)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN})
	if !strings.Contains(m.status, "no more") {
		t.Fatalf("the walk's end should say so, status = %q", m.status)
	}

	// Answer layer: the in-memory hop (回答层无翻页) skips to 丙.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN})
	if got := m.reader.floors[m.reader.floorIdx].cm.ID; got != "9003" {
		t.Fatalf("Ctrl+N on the answer layer should hop floors in memory, on %s", got)
	}

	// Comment layer: 甲's comments load, Ctrl+N hops the loaded rows,
	// then the offset page (末根) lands.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyHome}) // back to the top floor's screen… (clamped)
	for m.reader.floors[m.reader.floorIdx].cm.ID != "9001" {
		m = key(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	}
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.reader.layer != layerReplies {
		t.Fatalf("comment layer should be open, got %v", m.reader.layer)
	}
	// Hop the loaded rows (one Ctrl+N per row below), then the offset
	// page (末根) lands — bounded so a runaway hop loop fails, not hangs.
	for range 8 {
		var c tea.Cmd
		m, c = keyCmd(m, tea.KeyMsg{Type: tea.KeyCtrlN})
		m = pump(t, m, c)
		if strings.Contains(m.View(), "末页根") {
			break
		}
	}
	if !strings.Contains(m.View(), "末页根") {
		t.Fatalf("the hops should exhaust the loaded rows and fetch the offset page:\n%s", m.View())
	}
	// The walk ended: one more says so.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN})
	if !strings.Contains(m.status, "no more") {
		t.Fatalf("the comments walk's end should say so, status = %q", m.status)
	}
}

// A server-reported zero-comment answer: Enter is a no-op with the
// no-replies line, and NOT a wasted root_comment dial — the floor's
// verbatim NumChildren=0 is the fast path (ticket 06 review).
func TestZhihuZeroCommentAnswerEntersNowhere(t *testing.T) {
	srv, p := zhihuPlane(t)
	m := openQuestion(t, srv)

	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // → answer layer (甲)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight}) // → 乙 (0 comments)
	if got := m.reader.floors[m.reader.floorIdx].cm.ID; got != "9002" {
		t.Fatalf("setup: should sit on 乙, on %s", got)
	}
	before := p.count()
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.reader.layer != layerFloors {
		t.Fatalf("Enter on a zero-comment answer must not open a layer, got %v", m.reader.layer)
	}
	if !strings.Contains(m.status, "no replies") {
		t.Fatalf("status should say why, got %q", m.status)
	}
	if n := p.count(); n != before {
		t.Fatalf("the zero-count fast path must not dial, hits %d → %d", before, n)
	}
}

// The failure translations on open: 404 is the friendly gone line,
// 401/403 with a cookie is the Ctrl+U guidance — both instead of a
// raw HTTP error.
func TestZhihuDetailFailureTranslations(t *testing.T) {
	srv, p := zhihuPlane(t)

	p.status = 404
	m := openQuestion(t, srv)
	if !strings.Contains(m.status, "not found or deleted") {
		t.Fatalf("404 should translate to the gone line, status = %q", m.status)
	}

	// A fresh model for the 403 leg (the 404 cache attempt stored nothing).
	p.status = 403
	m = openQuestion(t, srv)
	if !strings.Contains(m.status, "Ctrl+U") {
		t.Fatalf("403 should guide to Ctrl+U, status = %q", m.status)
	}
}

// The offline half: after one successful open, a re-open with the
// plane dead renders the whole question layer from cache — floors,
// meta line, description — and the dead refresh stays silent (a
// cache exists; only a cacheless failure is loud).
func TestZhihuReopenRendersCacheOffline(t *testing.T) {
	srv, _ := zhihuPlane(t)
	m := openQuestion(t, srv)
	// Load one answer's comments while the plane is up — the replay
	// below reads this cache.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})        // → answers
	m, cmd0 := keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // → comments (fetch)
	m = pump(t, m, cmd0)
	if m.reader.layer != layerReplies {
		t.Fatalf("setup: comment layer should be open, got %v", m.reader.layer)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc}) // comments → answers
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc}) // answers → question
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc}) // question → list
	srv.Close()                                 // the plane goes dark

	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.reader == nil || m.reader.layer != layerBody {
		t.Fatal("the re-open should land on the question layer")
	}
	view := m.View()
	for _, want := range []string{"问主 asked", "描述内容", "甲", "乙"} {
		if !strings.Contains(view, want) {
			t.Errorf("offline re-open missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(m.status, "fetch") || strings.Contains(m.status, "HTTP") {
		t.Errorf("a cached open's failed refresh must stay silent, status = %q", m.status)
	}

	// The comment layer replays its cache too.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})      // → answers (甲)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // → comments (cached)
	m = pump(t, m, cmd)
	if m.reader.layer != layerReplies || !strings.Contains(m.View(), "根一内容") {
		t.Fatalf("comments should replay offline:\n%s", m.View())
	}
}
