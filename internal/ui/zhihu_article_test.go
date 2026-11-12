package ui

// Ticket 09 UI tests: the article's TWO-layer detail (body →
// comments; there is no floors layer) over the real Update loop —
// the body layer (title, author meta, cleaned content, the comments
// footer counting to the server's total), Enter straight into the
// comment layer (roots as rows, the flattened child with @parent),
// Esc stepping back to the BODY (never a floors layer), the
// comment layer's Ctrl+N (in-memory hops then the cursor page), the
// failure translations (404 / session), the zero-comment fast path,
// and the offline re-open replaying the cache with zero requests.
// The fake plane mirrors the measured matrix; the real-network run
// is ticket 07's smoke.

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

// aPlane is the article's whole data plane behind one fake server.
type aPlane struct {
	mu     chan struct{}
	hits   int
	status int // ≠0: both article doors answer this (404/403 fixtures)
}

func (p *aPlane) count() int {
	<-p.mu
	n := p.hits
	p.mu <- struct{}{}
	return n
}

func (p *aPlane) hit() {
	<-p.mu
	p.hits++
	p.mu <- struct{}{}
}

// articlePlane serves one article: the zhuanlan body door (title,
// author, cleaned content, comment_count 3) and the comment_v5
// articles door at two pages (roots 701 with a gap-filled child and
// 702; then 末根 on the composite cursor page).
func articlePlane(t *testing.T) (*httptest.Server, *aPlane) {
	t.Helper()
	p := &aPlane{mu: make(chan struct{}, 1)}
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
	mux.HandleFunc("/api/articles/2001", func(w http.ResponseWriter, r *http.Request) {
		p.hit()
		if !gate(w, r) || p.status != 0 {
			if p.status != 0 {
				w.WriteHeader(p.status)
				fmt.Fprint(w, `{"error":{"code":1}}`)
			}
			return
		}
		fmt.Fprint(w, `{"title":"专栏文章","author":{"name":"作者甲"},
		 "content":"<p>文章正文<b>加粗</b></p><noscript><img src=\"https://pic.zhimg.com/80/x.jpg\"></noscript>",
		 "comment_count":3,"voteup_count":1024,"created":1700000000,"updated":1700000100}`)
	})
	const composite = "5_11616058687_0"
	mux.HandleFunc("/api/v4/comment_v5/articles/2001/root_comment", func(w http.ResponseWriter, r *http.Request) {
		p.hit()
		if !gate(w, r) || p.status != 0 {
			if p.status != 0 {
				w.WriteHeader(p.status)
				fmt.Fprint(w, `{"error":{"code":1}}`)
			}
			return
		}
		if r.URL.Query().Get("offset") == composite {
			fmt.Fprint(w, `{"data":[{"id":703,"author":{"name":"末根"},"like_count":0,"content":"末页根"}],
			 "paging":{"next":"","is_end":true}}`)
			return
		}
		fmt.Fprint(w, `{"data":[
		 {"id":701,"author":{"name":"根一"},"like_count":3,"content":"文根一内容",
		  "child_comment_count":1,"child_comments":[]},
		 {"id":702,"author":{"name":"根二"},"like_count":0,"content":"文根二内容"}],
		 "paging":{"next":"http://`+r.Host+`/api/v4/comment_v5/articles/2001/root_comment?order_by=score&limit=20&offset=`+composite+`","is_end":false}}`)
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

// openArticle lands the reader on the article's body layer, pumped
// and ready for key asserts (three Tabs to zhihu, one Down to the
// recommend feed where the row is seeded).
func openArticle(t *testing.T, srv *httptest.Server) Model {
	t.Helper()
	m := seededModel(t, nil, nil) // sets READSCOPE_CONFIG to a temp dir
	if err := os.WriteFile(filepath.Join(os.Getenv("READSCOPE_CONFIG"), "zhihu.cookies"),
		[]byte("z_c0=live; d_c0=dev"), 0o600); err != nil {
		t.Fatalf("write cookie: %v", err)
	}
	t.Setenv("READSCOPE_ZHIHU_BASE", srv.URL)

	if _, _, err := m.st.SaveFeedItems(context.Background(), "zhihu", "recommend", []core.Item{{
		ID: "zh:article/2001", Source: "zhihu", Feed: "recommend",
		Title: "专栏文章", URL: "https://zhuanlan.zhihu.com/p/2001", Author: "作者甲",
		FetchedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("seed article: %v", err)
	}
	m = size(m, 120, 40)
	for range 3 { // books → rss → hn → zhihu
		var cmd tea.Cmd
		m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyTab})
		m = pump(t, m, cmd)
	}
	var cmd tea.Cmd
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyDown}) // sidebar: hot → recommend
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // sidebar → list
	m = pump(t, m, cmd)
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // open the article
	m = pump(t, m, cmd)
	if m.reader == nil {
		t.Fatal("Enter on the article row must open the reader")
	}
	return m
}

// The body layer: the title heading, the author meta, the CLEANED
// content, and the comments footer counting to the server's total —
// no answer floor list, no load-more footer (the article pages
// nothing).
func TestZhihuArticleBodyLayerRenders(t *testing.T) {
	srv, _ := articlePlane(t)
	m := openArticle(t, srv)

	if m.reader.layer != layerBody {
		t.Fatalf("open lands on the body layer, got %v", m.reader.layer)
	}
	view := m.View()
	for _, want := range []string{
		"专栏文章",       // the title heading
		"作者甲",        // the card's author on the meta line
		"文章正文",       // the content through CleanHTML
		"3 comments", // comments footer: the server's comment_count
	} {
		if !strings.Contains(view, want) {
			t.Errorf("article body layer missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "noscript") || strings.Contains(view, "zhimg.com/80") {
		t.Errorf("the content must ride CleanHTML:\n%s", view)
	}
	if strings.Contains(view, "Ctrl+N") {
		t.Errorf("the article body has no load-more footer:\n%s", view)
	}
}

// The two-layer navigation: Enter on the body goes STRAIGHT to the
// comment layer (no floors layer in between) with roots as rows and
// the flattened child carrying @parent; Esc steps back to the body
// (never a floors layer) with the body's scroll kept; the last Esc
// closes the reader.
func TestZhihuArticleTwoLayerNavigation(t *testing.T) {
	srv, _ := articlePlane(t)
	m := openArticle(t, srv)

	// Scroll the body, then Enter: straight into the comments.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	bodyTop := m.reader.top
	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.reader.layer != layerReplies {
		t.Fatalf("Enter should open the comment layer directly, got %v", m.reader.layer)
	}
	view := m.View()
	for _, want := range []string{"article comments", "文根一内容", "文根二内容", "子回复内容", "@根一"} {
		if !strings.Contains(view, want) {
			t.Errorf("comment layer missing %q:\n%s", want, view)
		}
	}

	// Esc: back to the BODY layer (never a floors layer), scroll kept.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.reader.layer != layerBody {
		t.Fatalf("Esc should return to the body layer, got %v", m.reader.layer)
	}
	if m.reader.top != bodyTop {
		t.Fatalf("the body's scroll must survive the comments round trip (top=%d want %d)",
			m.reader.top, bodyTop)
	}
	// The comments' scroll survives too: re-enter, still where it was.
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.reader.layer != layerReplies {
		t.Fatalf("re-enter should open the comment layer again, got %v", m.reader.layer)
	}
	if n := len(m.reader.replies); n != 3 {
		t.Fatalf("replies = %d rows, want 2 roots + 1 child", n)
	}

	// Esc twice: comments → body → list.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.reader != nil {
		t.Fatal("the last Esc should return to the list")
	}
}

// The comment layer's Ctrl+N: hops the loaded rows, then the cursor
// page (末根) lands; the walk's end says so.
func TestZhihuArticleCtrlNPagesComments(t *testing.T) {
	srv, _ := articlePlane(t)
	m := openArticle(t, srv)

	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // → comments
	m = pump(t, m, cmd)
	if m.reader.layer != layerReplies {
		t.Fatalf("setup: comment layer should be open, got %v", m.reader.layer)
	}
	for range 8 { // bounded: a runaway hop loop fails, not hangs
		var c tea.Cmd
		m, c = keyCmd(m, tea.KeyMsg{Type: tea.KeyCtrlN})
		m = pump(t, m, c)
		if strings.Contains(m.View(), "末页根") {
			break
		}
	}
	if !strings.Contains(m.View(), "末页根") {
		t.Fatalf("the hops should exhaust the loaded rows and fetch the cursor page:\n%s", m.View())
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyCtrlN})
	if !strings.Contains(m.status, "no more") {
		t.Fatalf("the comments walk's end should say so, status = %q", m.status)
	}
}

// A server-reported zero-comment article: Enter on the body is a
// no-op with the no-comments line, and NOT a wasted root_comment
// dial — the pseudo floor's verbatim NumChildren=0 is the fast path.
func TestZhihuZeroCommentArticleEntersNowhere(t *testing.T) {
	srv, p := articlePlane(t)
	// Reshape the body door's payload before the open: comment_count 0.
	srv.Config.Handler = withArticleComments(srv.Config.Handler, 0)
	m := openArticle(t, srv)

	before := p.count()
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.reader.layer != layerBody {
		t.Fatalf("Enter on a zero-comment article must not open a layer, got %v", m.reader.layer)
	}
	if !strings.Contains(m.status, "no comments") {
		t.Fatalf("status should say why, got %q", m.status)
	}
	if n := p.count(); n != before {
		t.Fatalf("the zero-count fast path must not dial, hits %d → %d", before, n)
	}
}

// The failure translations on open: 404 is the friendly gone line,
// 401/403 with a cookie is the Ctrl+U guidance — the hot/daily feeds
// untouched either way.
func TestZhihuArticleFailureTranslations(t *testing.T) {
	srv, p := articlePlane(t)

	p.status = 404
	m := openArticle(t, srv)
	if !strings.Contains(m.status, "not found or deleted") {
		t.Fatalf("404 should translate to the gone line, status = %q", m.status)
	}

	p.status = 403
	m = openArticle(t, srv)
	if !strings.Contains(m.status, "Ctrl+U") {
		t.Fatalf("403 should guide to Ctrl+U, status = %q", m.status)
	}
}

// The offline half: after one successful open (comments walked in),
// a re-open with the plane dead renders the body and the comment
// layer from cache, zero requests, the dead refresh silent.
func TestZhihuArticleReopenRendersCacheOffline(t *testing.T) {
	srv, _ := articlePlane(t)
	m := openArticle(t, srv)
	m, cmd := keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter}) // → comments (fetch + walk)
	m = pump(t, m, cmd)
	if m.reader.layer != layerReplies || !strings.Contains(m.View(), "文根一内容") {
		t.Fatalf("setup: comment layer should hold the walked tree:\n%s", m.View())
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc}) // comments → body
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEsc}) // body → list
	srv.Close()                                 // the plane goes dark

	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.reader == nil || m.reader.layer != layerBody {
		t.Fatal("the re-open should land on the body layer")
	}
	view := m.View()
	for _, want := range []string{"专栏文章", "作者甲", "文章正文"} {
		if !strings.Contains(view, want) {
			t.Errorf("offline re-open missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(m.status, "fetch") || strings.Contains(m.status, "HTTP") {
		t.Errorf("a cached open's failed refresh must stay silent, status = %q", m.status)
	}

	// The comment layer replays its cache too.
	m, cmd = keyCmd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pump(t, m, cmd)
	if m.reader.layer != layerReplies || !strings.Contains(m.View(), "文根一内容") {
		t.Fatalf("comments should replay offline:\n%s", m.View())
	}
}

// withArticleComments rewrites the body door's payload to the given
// comment_count (the zero-comment fast-path fixture) by wrapping the
// plane's handler.
func withArticleComments(next http.Handler, n int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/articles/2001" && r.Header.Get("Cookie") != "" {
			fmt.Fprint(w, `{"title":"专栏文章","author":{"name":"作者甲"},
			 "content":"<p>文章正文</p>","comment_count":`+fmt.Sprint(n)+`,"voteup_count":1}`)
			return
		}
		next.ServeHTTP(w, r)
	})
}
