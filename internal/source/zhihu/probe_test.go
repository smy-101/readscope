package zhihu

// Ticket 01 tests: the probe walk against a fake zhihu fronted by
// httptest — every endpoint fixture'd, every header contract asserted
// (UA, x-requested-with, per-endpoint Referer, x-zse-93 semantics,
// cookie presence), the qid/aid/story chain, sample saving, and the
// GREEN/BLOCKED/SKIPPED verdicts. All offline; the real-network run
// is the ticket's manual step.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

const (
	fakeQID    = "18984420158198241699" // >2^53
	fakeAID    = "1912345678901234567"
	fakeStory  = "9792937"
	cookieText = "z_c0=2+1.0x/secret; d_c0=2+1.0x/dev"
)

const (
	hotJSON = `{"data":[{"detail_text":"2045 万热度","target":{"id":18984420158198241699,` +
		`"type":"question","title":"为什么终端阅读器又火了？",` +
		`"url":"https://api.zhihu.com/questions/18984420158198241699"}}]}`

	dailyLatestJSON = `{"date":"20261129","stories":[{"id":9792937,` +
		`"title":"今天的日报","hint":"作者 · 2 分钟阅读"}]}`

	dailyStoryJSON = `{"title":"今天的日报","body":"<p>全文正文，离线快照用。</p>"}`

	// moments mixes four card kinds: only answer/question cards should
	// survive; article and pin are censused as dropped.
	momentsJSON = `{"data":[
	 {"target":{"type":"answer","id":1912345678901234567,"author":{"name":"李四"},
	  "question":{"id":18984420158198241699,"title":"关注的问题"}}},
	 {"target":{"type":"question","id":100001,"title":"关注的新提问","author":{"name":"王五"}}},
	 {"target":{"type":"article","id":200002,"title":"被丢弃的文章"}},
	 {"target":{"type":"pin","id":300003}}
	],"paging":{"next":"https://www.zhihu.com/api/v3/moments?cursor=abc","is_end":false}}`

	// recommend (ticket 08) mixes the algorithmic feed's card kinds:
	// answer/question kept as question cards, article censused apart
	// (ticket 09 will keep it), pin/video dropped.
	recommendJSON = `{"data":[
	 {"target":{"type":"answer","id":1912345678901234568,"author":{"name":"赵六"},
	  "question":{"id":18984420158198241698,"title":"推荐的问题"}}},
	 {"target":{"type":"article","id":200009,"title":"推荐的文章"}},
	 {"target":{"type":"pin","id":300010}},
	 {"target":{"type":"video","id":400011}}
	],"paging":{"next":"https://www.zhihu.com/api/v3/feed/topstory/recommend?cursor=xyz","is_end":false}}`

	questionMetaJSON = `{"title":"为什么终端阅读器又火了？","author":{"name":"张三"},
	 "answer_count":213,"visit_count":12000000,"detail":"<p>问题描述</p>"}`

	v4ErrJSON = `{"error":{"code":10003,"message":"请升级客户端版本"}}`

	feedsJSON = `{"data":[{"target":{"id":1912345678901234567,"type":"answer",
	 "author":{"name":"李四"},"voteup_count":12345,"comment_count":541,
	 "content":"<p>回答全文</p>","created_time":1700000000}}],
	 "paging":{"next":"https://www.zhihu.com/api/v4/questions/x/feeds?cursor=n","is_end":false}}`

	commentsJSON = `{"data":[{"id":9876543210123,"author":{"name":"王五"},"like_count":231,
	 "content":"根评论内容","child_comment_count":14,
	 "child_comments":[{"id":1},{"id":2}]}],
	 "paging":{"next":"","is_end":true}}`
)

// request is one recorded probe hit, for header-contract asserts.
type request struct {
	path       string
	query      string // raw query string — the cursor pages' address
	ua         string
	xrw        string // x-requested-with
	zse93      bool
	zse93Val   string // the version value when present (wrong-version legs)
	cookie     bool
	referer    string
	accept     string
	acceptLang string
}

// fakeZhihu implements the endpoint + header matrix the probe walks.
// Semantics mirror the 2026-11 FeedScope matrix:
//   - v4/questions  : cookie required; x-zse-93 required (403 code 10003 without)
//   - v4/…/feeds    : cookie required; x-zse-93 FORBIDDEN on the cursorless entry (403 with)
//   - v3 moments    : cookie required (403 without)
//   - hot/daily     : anonymous ok
type fakeZhihu struct {
	t                   *testing.T
	mu                  sync.Mutex
	hits                []request
	blockMoments        bool   // simulate risk control (403 even with cookie)
	blockRecommend      bool   // simulate risk control on the recommend door (403 even with cookie)
	emptyHot            bool   // serve {"data":[]} — redesign tripwire fixture
	hotOverride         string // serve this body for /topstory/hot-list (source-test fixtures)
	momentsOverride     string // serve this body for /api/v3/moments (follow-feed fixtures)
	recommendOverride   string // serve this body for /api/v3/feed/topstory/recommend (ticket 08 fixtures)
	dailyLatestOverride string // serve this body for /api/4/news/latest (ticket 03 fixtures)
	dailyStoryOverride  string // serve this body for /api/4/story/* (ticket 03 fixtures)
	staleStatus         int    // ≠0: v4/comment_v5 answer this status despite a cookie (stale z_c0)
	wrongVersion        string // ≠"": question-meta demands exactly this x-zse-93 value (zhihu bumped it)

	// ticket 06 fixtures (detail.go): the question family's overrides,
	// a 404 arm, and the child-comment walk's per-root page sequences.
	questionMetaOverride string
	feedsOverride        string
	rootOverride         string
	goneStatus           int                 // ≠0: v4/questions + comment_v5 answer this (404 fixture)
	childPages           map[string][]string // root id → page bodies in walk order
	childClicks          map[string]int
	extraPages           map[string]string // "path?query" (exact) or path → body, for cursor-addressed pages (paging.next targets)
}

// offsetParamRe picks the offset parameter's value out of a raw
// query: digits-only values hit the real API's zombie quirk,
// composite cursors ("387770585_11616175634_0") ride extraPages.
var offsetParamRe = regexp.MustCompile(`(?:^|&)offset=([^&]*)`)

// schemeHost rebuilds the request's scheme://host for fixture
// paging.next addresses.
func schemeHost(r *http.Request) string {
	if r.TLS != nil {
		return "https://" + r.Host
	}
	return "http://" + r.Host
}

func (f *fakeZhihu) record(r *http.Request) request {
	req := request{
		path:       r.URL.Path,
		query:      r.URL.RawQuery,
		ua:         r.Header.Get("User-Agent"),
		xrw:        r.Header.Get("x-requested-with"),
		zse93:      r.Header.Get("x-zse-93") != "",
		zse93Val:   r.Header.Get("x-zse-93"),
		cookie:     r.Header.Get("Cookie") != "",
		referer:    r.Header.Get("Referer"),
		accept:     r.Header.Get("Accept"),
		acceptLang: r.Header.Get("Accept-Language"),
	}
	f.mu.Lock()
	f.hits = append(f.hits, req)
	f.mu.Unlock()
	return req
}

func (f *fakeZhihu) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := f.record(r)
	write := func(status int, body string) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}
	if body, ok := f.extraPages[req.path+"?"+req.query]; ok && req.query != "" {
		write(200, body) // a cursor-addressed page: paging.next rides verbatim
		return
	}
	if body, ok := f.extraPages[req.path]; ok {
		write(200, body) // a cursor-addressed page (queryless spelling): paging.next rides verbatim
		return
	}
	switch {
	case req.path == "/topstory/hot-list":
		if f.hotOverride != "" {
			write(200, f.hotOverride)
			return
		}
		if f.emptyHot {
			write(200, `{"data":[]}`)
			return
		}
		write(200, hotJSON)
	case req.path == "/api/v3/feed/topstory/hot-lists/total":
		write(200, hotJSON)
	case req.path == "/api/4/news/latest":
		if f.dailyLatestOverride != "" {
			write(200, f.dailyLatestOverride)
			return
		}
		write(200, dailyLatestJSON)
	case strings.HasPrefix(req.path, "/api/4/story/"):
		if f.dailyStoryOverride != "" {
			write(200, f.dailyStoryOverride)
			return
		}
		write(200, dailyStoryJSON)
	case req.path == "/api/v3/moments":
		if !req.cookie {
			write(403, v4ErrJSON) // anonymous: gate
			return
		}
		if f.blockMoments {
			write(403, v4ErrJSON) // pure risk control
			return
		}
		if f.momentsOverride != "" {
			write(200, f.momentsOverride)
			return
		}
		write(200, momentsJSON)
	case req.path == "/api/v3/feed/topstory/recommend":
		if !req.cookie {
			write(403, v4ErrJSON) // anonymous: the www v3 gateway's session door
			return
		}
		if f.blockRecommend {
			write(403, v4ErrJSON) // pure risk control
			return
		}
		if f.recommendOverride != "" {
			write(200, f.recommendOverride)
			return
		}
		write(200, recommendJSON)
	case strings.HasPrefix(req.path, "/api/v4/comment_v5/"):
		if !req.cookie || f.staleStatus != 0 {
			write(f.staleStatusOr(401), v4ErrJSON)
			return
		}
		if f.goneStatus != 0 {
			write(f.goneStatus, v4ErrJSON) // the answer behind the comments is gone
			return
		}
		if strings.Contains(req.path, "/child_comment") {
			// one root's completion walk: the page sequence for the root
			// id in the path, advancing per click; past the end serves the
			// is_end page again (a runaway walk double-clicks and still
			// terminates).
			root := f.childRootID(req.path)
			f.mu.Lock()
			pages := f.childPages[root]
			click := f.childClicks[root]
			f.childClicks[root] = click + 1
			f.mu.Unlock()
			if len(pages) == 0 {
				write(404, v4ErrJSON)
				return
			}
			if click >= len(pages) {
				click = len(pages) - 1
			}
			write(200, pages[click])
			return
		}
		if strings.Contains(req.path, "/root_comment") {
			// Real-network contract (2026-12 smoke measurement): the
			// first page rides NO offset parameter — a literal numeric
			// offset (offset=0 included) answers HTTP 200 with a zombie
			// empty page whose own next re-points at offset=0, on every
			// answer, uniformly. Cursor pages arrive verbatim from
			// paging.next (composite offset strings) and are served by
			// the extraPages arm above.
			if m := offsetParamRe.FindStringSubmatch(req.query); m != nil && m[1] != "" {
				write(200, `{"data":[],"paging":{"next":"`+schemeHost(r)+req.path+`?limit=10&offset=`+m[1]+`&order_by=score","is_end":false,"totals":0}}`)
				return
			}
		}
		if f.rootOverride != "" {
			write(200, f.rootOverride)
			return
		}
		write(200, commentsJSON)
	case strings.HasPrefix(req.path, "/api/v4/questions/"):
		feeds := strings.HasSuffix(req.path, "/feeds")
		if !req.cookie || f.staleStatus != 0 {
			write(f.staleStatusOr(401), v4ErrJSON)
			return
		}
		if f.goneStatus != 0 {
			write(f.goneStatus, v4ErrJSON) // the question (or answer) is gone
			return
		}
		if f.wrongVersion != "" && !feeds && req.zse93Val != f.wrongVersion {
			write(403, v4ErrJSON) // zhihu bumped the constant: ours is now the wrong version
			return
		}
		if feeds && req.zse93 {
			write(403, v4ErrJSON) // cursorless feeds entry rejects the version header
			return
		}
		if !feeds && !req.zse93 {
			write(403, v4ErrJSON) // question meta demands it (code 10003 gate)
			return
		}
		if feeds {
			if f.feedsOverride != "" {
				write(200, f.feedsOverride)
				return
			}
			write(200, feedsJSON)
		} else {
			if f.questionMetaOverride != "" {
				write(200, f.questionMetaOverride)
				return
			}
			write(200, questionMetaJSON)
		}
	default:
		write(404, "{}")
	}
}

// childRootID digs the root comment id out of a child_comment path
// (/api/v4/comment_v5/comment/<id>/child_comment) — the fixture key.
func (f *fakeZhihu) childRootID(path string) string {
	rest := strings.TrimPrefix(path, "/api/v4/comment_v5/comment/")
	if i := strings.Index(rest, "/"); i >= 0 {
		return rest[:i]
	}
	return rest
}

func (f *fakeZhihu) hit(pathPrefix string) *request {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.hits {
		if strings.HasPrefix(f.hits[i].path, pathPrefix) {
			return &f.hits[i]
		}
	}
	return nil
}

// staleStatusOr returns the stale-session status when armed, else the
// given default.
func (f *fakeZhihu) staleStatusOr(def int) int {
	if f.staleStatus != 0 {
		return f.staleStatus
	}
	return def
}

// hitCount counts recorded hits with the given path prefix (the
// gated-preflight tests assert the transport never dialed).
func (f *fakeZhihu) hitCount(pathPrefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for i := range f.hits {
		if strings.HasPrefix(f.hits[i].path, pathPrefix) {
			n++
		}
	}
	return n
}

// childHitCount counts one root's completion-walk clicks (the
// six-page bound's assert).
func (f *fakeZhihu) childHitCount(rootID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.childClicks[rootID]
}

// hitExact finds the one hit with exactly this path (the question
// meta and feeds doors share a prefix; only the path tells them
// apart).
func (f *fakeZhihu) hitExact(path string) *request {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.hits {
		if f.hits[i].path == path {
			return &f.hits[i]
		}
	}
	return nil
}

func newTestProbe(t *testing.T, fake *fakeZhihu, cookie string) (*Probe, *strings.Builder, string) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	out := filepath.Join(t.TempDir(), "samples")
	var buf strings.Builder
	p := &Probe{
		Hosts:        Hosts{API: srv.URL, Daily: srv.URL, Web: srv.URL},
		CookieHeader: cookie,
		OutDir:       out,
		Client:       srv.Client(),
	}
	return p, &buf, out
}

func verdicts(rs []EndpointResult) map[string]string {
	m := make(map[string]string, len(rs))
	for _, r := range rs {
		m[r.Name] = r.Verdict
	}
	return m
}

func TestProbeFullWalkWithCookie(t *testing.T) {
	fake := &fakeZhihu{t: t}
	p, buf, out := newTestProbe(t, fake, cookieText)

	rs := p.Run(context.Background(), buf)
	v := verdicts(rs)

	for name, want := range map[string]string{
		"hot-list": "GREEN", "hot-www-door": "GREEN",
		"daily-latest": "GREEN", "daily-story": "GREEN",
		"moments": "GREEN", "recommend": "GREEN", "question-meta": "GREEN",
		"answers-feeds": "GREEN", "comments-root": "GREEN",
		"matrix-questions-nozse": "MATCH", "matrix-feeds-withzse": "MATCH",
	} {
		if v[name] != want {
			t.Errorf("%s verdict = %q, want %q (note: %s)", name, v[name], want, noteOf(rs, name))
		}
	}
	if Blocked(rs) {
		t.Error("Blocked() = true on an all-green walk")
	}

	// Header contract on the shared request shape.
	if h := fake.hit("/topstory/hot-list"); h == nil || !strings.Contains(h.ua, "Chrome/") || h.xrw != "fetch" {
		t.Errorf("hot-list headers: ua/xrw contract broken: %+v", h)
	}
	if h := fake.hit("/api/v3/moments"); h == nil || h.referer != "https://www.zhihu.com/follow" || !h.cookie {
		t.Errorf("moments headers: referer/cookie contract broken: %+v", h)
	}
	if h := fake.hitExact("/api/v3/feed/topstory/recommend"); h == nil || h.referer != "https://www.zhihu.com/" || !h.cookie {
		t.Errorf("recommend headers: referer(home page)/cookie contract broken: %+v", h)
	}
	if h := fake.hit("/api/v4/questions/"); h == nil || !h.zse93 || h.referer != "https://www.zhihu.com/question/"+fakeQID {
		t.Errorf("question-meta headers: zse93 required / referer contract broken: %+v", h)
	}
	// Both /feeds hits, told apart by the version header: the main
	// walk carries no x-zse-93, the matrix self-check does.
	feedsHits := hitsWhere(fake, "/feeds")
	if len(feedsHits) != 2 {
		t.Fatalf("feeds hits = %d, want 2 (main walk + matrix)", len(feedsHits))
	}
	var mainFeeds, matrixFeeds *request
	for i := range feedsHits {
		if feedsHits[i].zse93 {
			matrixFeeds = feedsHits[i]
		} else {
			mainFeeds = feedsHits[i]
		}
	}
	if mainFeeds == nil || !mainFeeds.cookie {
		t.Errorf("answers-feeds main walk must carry cookie without x-zse-93: %+v", mainFeeds)
	}
	if matrixFeeds == nil {
		t.Error("matrix feeds hit (with x-zse-93) missing")
	}
	if h := fake.hit("/api/4/news/latest"); h == nil || h.referer != "https://daily.zhihu.com/" {
		t.Errorf("daily-latest referer contract broken: %+v", h)
	}

	// The chain: qid from the hot list, aid from the feeds page, story
	// id from the daily list.
	if h := fake.hit("/api/v4/questions/"); h == nil || !strings.Contains(h.path, "/questions/"+fakeQID) {
		t.Errorf("question-meta path missing url-extracted qid: %+v", h)
	}
	if h := fake.hit("/api/v4/comment_v5/"); h == nil || !strings.Contains(h.path, "/answers/"+fakeAID) {
		t.Errorf("comments-root path missing chained aid: %+v", h)
	}

	// Samples on disk for every walked endpoint.
	for _, name := range []string{"hot-list", "hot-www-door", "daily-latest", "daily-story",
		"moments", "recommend", "question-meta", "answers-feeds", "comments-root",
		"matrix-questions-nozse", "matrix-feeds-withzse"} {
		if _, err := os.Stat(filepath.Join(out, name+".json")); err != nil {
			t.Errorf("sample %s.json missing: %v", name, err)
		}
	}

	// The report says what the tracer needs to say.
	s := buf.String()
	for _, want := range []string{"GREEN", ">2^53", "2045 万热度", "answer=1 question=1",
		"dropped=2", "article=1", "child_gap=12"} {
		if !strings.Contains(s, want) {
			t.Errorf("report missing %q\n---\n%s", want, s)
		}
	}
}

func TestProbeNoCookieSkipsLoginEndpoints(t *testing.T) {
	fake := &fakeZhihu{t: t}
	p, buf, _ := newTestProbe(t, fake, "")

	rs := p.Run(context.Background(), buf)
	v := verdicts(rs)
	for name := range map[string]struct{}{
		"hot-www-door": {}, "moments": {}, "recommend": {}, "question-meta": {}, "answers-feeds": {}, "comments-root": {},
	} {
		if v[name] != "SKIPPED" {
			t.Errorf("%s = %q, want SKIPPED (note: %s)", name, v[name], noteOf(rs, name))
		}
	}
	if !strings.Contains(buf.String(), "SKIPPED-NO-COOKIE") {
		t.Errorf("report should name the skip reason:\n%s", buf)
	}
	if Blocked(rs) {
		t.Error("a no-cookie walk is not a blocked data plane")
	}
}

func TestProbeBlockedMoments(t *testing.T) {
	fake := &fakeZhihu{t: t, blockMoments: true}
	p, buf, _ := newTestProbe(t, fake, cookieText)

	rs := p.Run(context.Background(), buf)
	v := verdicts(rs)
	if v["moments"] != "BLOCKED" {
		t.Fatalf("moments = %q, want BLOCKED (note: %s)", v["moments"], noteOf(rs, "moments"))
	}
	if !Blocked(rs) {
		t.Error("Blocked() = false with a 403 moments")
	}
	if !strings.Contains(buf.String(), "HTTP 403") {
		t.Errorf("report should carry the observed status:\n%s", buf)
	}
}

func TestProbeRedesignTripwire(t *testing.T) {
	// The spec's 「0 条解析即报站点可能改版」: a well-formed but empty
	// list must carry the alarm in its note, not a silent GREEN.
	fake := &fakeZhihu{t: t, emptyHot: true}
	p, buf, _ := newTestProbe(t, fake, "")

	rs := p.Run(context.Background(), buf)
	if v := verdicts(rs)["hot-list"]; v != "GREEN" {
		t.Fatalf("hot-list verdict = %q, want GREEN (endpoint reachable)", v)
	}
	if !strings.Contains(buf.String(), "站点可能改版") {
		t.Errorf("0-row hot list should trip the redesign alarm:\n%s", buf)
	}
}

func noteOf(rs []EndpointResult, name string) string {
	for _, r := range rs {
		if r.Name == name {
			return r.Note
		}
	}
	return ""
}

func hitsWhere(f *fakeZhihu, suffix string) []*request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*request
	for i := range f.hits {
		if strings.HasSuffix(f.hits[i].path, suffix) {
			out = append(out, &f.hits[i])
		}
	}
	return out
}
