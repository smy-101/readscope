package zhihu

// Ticket 04 tests: the shared transport — the request matrix worn per
// endpoint (Referer, x-zse-93, cookie), the gated preflight
// (no cookie → ErrLoginRequired, no dial), and the session-loss
// semantics: detail family 401/403 → ErrSessionExpired (Ctrl+U
// guidance upstream), list family 403 → raw error, anonymous doors
// unaffected by any of it. The wrong-version leg pins the 01 review
// transfer: a bumped version constant is indistinguishable from a
// stale cookie client-side, so it lands as session-expired too —
// the probe report is where 10003 gets told apart.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// transportAt lands a Transport against the fake zhihu front.
func transportAt(t *testing.T, fake *fakeZhihu, cookie string) (*Transport, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return NewTransport(Hosts{API: srv.URL, Daily: srv.URL, Web: srv.URL}, cookie, nil), srv
}

func TestTransportWearsTheMatrixPerEndpoint(t *testing.T) {
	fake := &fakeZhihu{}
	tr, srv := transportAt(t, fake, cookieText)
	ctx := context.Background()

	meta := epQuestionMeta
	meta.Referer += fakeQID
	feeds := epAnswersFeeds
	feeds.Referer += fakeQID

	legs := []struct {
		ep   Endpoint
		url  string
		path string // recorded hit to assert against
	}{
		{epHotList, srv.URL + "/topstory/hot-list", "/topstory/hot-list"},
		{epHotWWW, srv.URL + "/api/v3/feed/topstory/hot-lists/total", "/api/v3/feed/"},
		{epDailyLatest, srv.URL + "/api/4/news/latest", "/api/4/news/latest"},
		{epDailyStory, srv.URL + "/api/4/story/" + fakeStory, "/api/4/story/"},
		{epMoments, srv.URL + "/api/v3/moments", "/api/v3/moments"},
		{meta, fmt.Sprintf("%s/api/v4/questions/%s", srv.URL, fakeQID), "/api/v4/questions/"},
		{feeds, fmt.Sprintf("%s/api/v4/questions/%s/feeds", srv.URL, fakeQID), "/feeds"},
		{epCommentsRoot, srv.URL + "/api/v4/comment_v5/answers/" + fakeAID + "/root_comment", "/api/v4/comment_v5/"},
	}
	for _, leg := range legs {
		if res := tr.Get(ctx, leg.ep, leg.url); res.Err != nil {
			t.Fatalf("%s: %v", leg.ep.Name, res.Err)
		}
		var hit *request
		if strings.HasPrefix(leg.path, "/feeds") {
			// the feeds door shares its path prefix with question-meta;
			// tell the hits apart by suffix (hitsWhere)
			hs := hitsWhere(fake, "/feeds")
			if len(hs) == 0 {
				t.Fatalf("%s: never hit %s", leg.ep.Name, leg.path)
			}
			hit = hs[len(hs)-1]
		} else {
			hit = fake.hit(leg.path)
			if hit == nil {
				t.Fatalf("%s: never hit %s", leg.ep.Name, leg.path)
			}
		}
		if hit.ua != DesktopUA || hit.xrw != "fetch" || hit.acceptLang == "" {
			t.Errorf("%s: common headers broken: %+v", leg.ep.Name, hit)
		}
		if hit.referer != leg.ep.Referer {
			t.Errorf("%s: Referer = %q, want %q", leg.ep.Name, hit.referer, leg.ep.Referer)
		}
		if want := leg.ep.ZSE93; hit.zse93 != want {
			t.Errorf("%s: x-zse-93 sent = %v, want %v (value %q)",
				leg.ep.Name, hit.zse93, want, hit.zse93Val)
		}
		if hit.zse93 && hit.zse93Val != XZse93 {
			t.Errorf("%s: x-zse-93 = %q, want the XZse93 constant %q", leg.ep.Name, hit.zse93Val, XZse93)
		}
		if want := leg.ep.Cookie; hit.cookie != want {
			t.Errorf("%s: cookie sent = %v, want %v", leg.ep.Name, hit.cookie, want)
		}
	}
	// The daily legs ride no cookie even with one loaded — they are
	// the anonymous doors (pinned above by Cookie=false; this is the
	// belt to that braces: a hit proves the assertion ran).
	if hit := fake.hit("/api/4/news/latest"); hit == nil || hit.cookie {
		t.Errorf("daily-latest must stay anonymous: %+v", hit)
	}
}

func TestTransportGatedPreflightRefusesWithoutCookie(t *testing.T) {
	fake := &fakeZhihu{}
	tr, srv := transportAt(t, fake, "")

	for _, ep := range []Endpoint{epHotWWW, epMoments, epQuestionMeta, epAnswersFeeds, epCommentsRoot} {
		res := tr.Get(context.Background(), ep, srv.URL+"/anything")
		if !errors.Is(res.Err, ErrLoginRequired) {
			t.Errorf("%s: err = %v, want ErrLoginRequired", ep.Name, res.Err)
		}
		if res.Status != 0 || res.Body != nil {
			t.Errorf("%s: gated preflight must answer before any exchange, got status=%d body=%dB", ep.Name, res.Status, len(res.Body))
		}
	}
	if n := len(fake.hits); n != 0 {
		t.Errorf("gated calls dialed %d times, want 0 — the refusal is client-side", n)
	}

	// Non-gated doors keep working anonymously — 热榜/日报不受影响.
	if res := tr.Get(context.Background(), epHotList, srv.URL+"/topstory/hot-list"); res.Err != nil {
		t.Errorf("hot-list without cookie: %v", res.Err)
	}
	if res := tr.Get(context.Background(), epDailyLatest, srv.URL+"/api/4/news/latest"); res.Err != nil {
		t.Errorf("daily-latest without cookie: %v", res.Err)
	}
}

func TestTransportDetailFamilySessionExpired(t *testing.T) {
	// FeedScope-measured shape: z_c0 dead server-side → v3 lists still
	// answer, every v4/comment_v5 call 403s. Both 401 and 403 on the
	// detail family read as ErrSessionExpired (login guidance
	// upstream); the cookie is never judged deletable from here.
	for _, status := range []int{401, 403} {
		fake := &fakeZhihu{staleStatus: status}
		tr, srv := transportAt(t, fake, cookieText)

		meta := epQuestionMeta
		meta.Referer += fakeQID
		res := tr.Get(context.Background(), meta, fmt.Sprintf("%s/api/v4/questions/%s", srv.URL, fakeQID))
		if !errors.Is(res.Err, ErrSessionExpired) {
			t.Errorf("HTTP %d: err = %v, want ErrSessionExpired", status, res.Err)
		}
		if res.Status != status {
			t.Errorf("HTTP %d: status = %d, want it carried on the response", status, res.Status)
		}
		// The error document rides along — the probe's samples (and
		// its 10003 tell-apart) depend on it.
		if !strings.Contains(string(res.Body), `"code"`) {
			t.Errorf("HTTP %d: error document missing from response body", status)
		}

		res = tr.Get(context.Background(), epCommentsRoot,
			srv.URL+"/api/v4/comment_v5/answers/"+fakeAID+"/root_comment")
		if !errors.Is(res.Err, ErrSessionExpired) {
			t.Errorf("comment_v5 HTTP %d: err = %v, want ErrSessionExpired", status, res.Err)
		}
	}
}

func TestTransportWrongVersionLandsAsSessionExpired(t *testing.T) {
	// 01 review transfer: zhihu bumps the version constant → our
	// XZse93 is now the WRONG version → 403 code 10003. Client-side
	// that is indistinguishable from a stale cookie, so the transport
	// reads it as session-expired (Ctrl+U guidance); diagnosing the
	// real cause (re-paste the constant) belongs to the probe report.
	fake := &fakeZhihu{wrongVersion: "999_9_9.9"}
	tr, srv := transportAt(t, fake, cookieText)

	meta := epQuestionMeta
	meta.Referer += fakeQID
	res := tr.Get(context.Background(), meta, fmt.Sprintf("%s/api/v4/questions/%s", srv.URL, fakeQID))
	if !errors.Is(res.Err, ErrSessionExpired) {
		t.Fatalf("wrong version: err = %v, want ErrSessionExpired", res.Err)
	}
	if res.Status != 403 {
		t.Fatalf("wrong version: status = %d, want 403", res.Status)
	}
}

func TestTransportListFamily403StaysRawError(t *testing.T) {
	// v3 403 is risk control, not login state: raw error naming the
	// endpoint and status — never session-expired, never a cookie
	// verdict (the spec: 清了等于强制重登).
	fake := &fakeZhihu{blockMoments: true}
	tr, srv := transportAt(t, fake, cookieText)

	res := tr.Get(context.Background(), epMoments, srv.URL+"/api/v3/moments")
	if res.Err == nil {
		t.Fatal("blocked moments: want an error")
	}
	if errors.Is(res.Err, ErrSessionExpired) || errors.Is(res.Err, ErrLoginRequired) {
		t.Fatalf("v3 403 must not claim session state: %v", res.Err)
	}
	if !strings.Contains(res.Err.Error(), "moments") || !strings.Contains(res.Err.Error(), "403") {
		t.Errorf("raw error should name endpoint and status: %v", res.Err)
	}
}

func TestTransportPlainStatusAndBodies(t *testing.T) {
	// The anonymous doors' non-200s are plain errors (no session to
	// read); 200s hand the body over untouched — the fetch contract
	// both callers (probe samples, source parse) ride.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/boom") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, hotJSON)
	}))
	t.Cleanup(srv.Close)
	tr := NewTransport(Hosts{API: srv.URL}, "", nil)

	res := tr.Get(context.Background(), epHotList, srv.URL+"/boom")
	if res.Err == nil || !strings.Contains(res.Err.Error(), "500") || errors.Is(res.Err, ErrSessionExpired) {
		t.Fatalf("anon 500 should be a plain error: %v", res.Err)
	}
	res = tr.Get(context.Background(), epHotList, srv.URL+"/topstory/hot-list")
	if res.Err != nil || !strings.Contains(string(res.Body), "detail_text") {
		t.Fatalf("200 body should ride raw: err=%v body=%.32s", res.Err, res.Body)
	}
}

func TestNewLoadsCookieFromFilePerConstruction(t *testing.T) {
	// 保存即用: every refresh builds a fresh source, so the cookie
	// file on disk IS the live session — re-login (Ctrl+U / CLI) lands
	// on the next F5 without a restart. Pinned here at the seam that
	// makes it true: New() reads the file at construction.
	cfgDir := t.TempDir()
	t.Setenv("READSCOPE_CONFIG", cfgDir)
	srv := httptest.NewServer(&fakeZhihu{})
	t.Cleanup(srv.Close)
	t.Setenv("READSCOPE_ZHIHU_BASE", srv.URL)

	// No file: the source is anonymous — the hot door works, no
	// Cookie header rides.
	src, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s := src.(*Source)
	if s.tr.cookie != "" {
		t.Fatal("no cookie file → anonymous transport")
	}

	// Valid file (z_c0 aboard): the transport carries it.
	if _, err := SaveCookie(mustCookiePath(t), "z_c0=2+1.0x/newsecret; d_c0=2+1.0x/dev"); err != nil {
		t.Fatalf("SaveCookie: %v", err)
	}
	s2, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s2.(*Source).tr.cookie != "z_c0=2+1.0x/newsecret; d_c0=2+1.0x/dev" {
		t.Errorf("cookie not loaded per construction: %q", s2.(*Source).tr.cookie)
	}

	// z_c0-less file: 判活 refuses — anonymous, not a broken session.
	if err := writeFile0600(mustCookiePath(t), "d_c0=only"); err != nil {
		t.Fatal(err)
	}
	s3, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s3.(*Source).tr.cookie != "" {
		t.Errorf("z_c0-less file must degrade to anonymous, got %q", s3.(*Source).tr.cookie)
	}
}

func mustCookiePath(t *testing.T) string {
	t.Helper()
	path, err := CookiePath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFile0600(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
