package zhihu

// transport.go — the shared transport every zhihu request rides (ticket
// 04): one place owns the measured request shape (desktop Chrome UA,
// x-requested-with, a real fetch's Accept headers), the per-endpoint
// matrix (Referer, x-zse-93, cookie), and the session-loss
// semantics (which 401/403s mean "re-login" and which mean "blocked").
// The probe walk (probe.go) and the source fetches (source.go) are
// both thin callers of this — no third header block, ever (02 review
// transfer).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// fetchTimeout bounds one HTTP round trip; parsing is local.
const fetchTimeout = 30 * time.Second

// maxBodyBytes caps one response body (the biggest legitimate payload
// is a fat answers-feeds page; everything past this is a runaway).
const maxBodyBytes = 10 << 20

// Session errors — the failure semantics the spec fixes (ticket 04).
// Both wrap into endpoint errors; the UI maps them to login guidance
// instead of a raw HTTP line.
var (
	// ErrLoginRequired: a cookie-gated endpoint was called with no
	// cookie loaded. The fix is the login ceremony (Ctrl+U / `zhihu
	// login`), not a retry.
	ErrLoginRequired = errors.New("zhihu: login required — paste a cookie (Ctrl+U / readscope zhihu login)")

	// ErrSessionExpired: the detail family answered 401/403 with a
	// cookie aboard. A stale z_c0 reads exactly like this server-side;
	// the cookie is NOT cleared (v3 403 vs v4 403 is unjudgeable
	// client-side, and the hot/daily feeds don't care either way).
	ErrSessionExpired = errors.New("zhihu: login session expired — re-login (Ctrl+U / readscope zhihu login)")

	// ErrContentGone: a detail-family endpoint answered 404 — the
	// question (or the answer behind a comments call) was deleted or
	// never existed. The UI translates this into the friendly 不存在
	// 或已删除 line instead of a raw HTTP error.
	ErrContentGone = errors.New("zhihu: question or answer not found (404)")
)

// Family is how an HTTP 401/403 reads on one endpoint — the spec's
// 失效语义. Detail-family auth failures mean the session died; list and
// anonymous doors read them as plain blocks (risk control, gate), so
// their errors stay raw and specific.
type Family int8

const (
	// FamilyAnon: the anonymous doors (hot board's api twin, daily).
	// No session to lose — non-200s are plain errors.
	FamilyAnon Family = iota
	// FamilyList: the www v3 gateway (moments, the hot www door).
	// 403 is risk control, not login state: report raw, never clear
	// the cookie, never claim the session died.
	FamilyList
	// FamilyDetail: v4 + comment_v5 (question meta, answer feeds,
	// comments). 401/403 → ErrSessionExpired: z_c0 expired
	// server-side (FeedScope-measured: v3 lists keep working while
	// every v4 detail call 403s).
	FamilyDetail
)

// Endpoint is one row of the request matrix (ADR #14, ticket 01's
// measured contract): the shared headers plus this endpoint's Referer,
// whether it wears x-zse-93 (XZse93 — required by the
// question-meta gate, forbidden on the cursorless feeds entry), and
// how its auth failures read. The vars below are the single source of
// truth; callers clone-and-flip only the variable parts (the question
// Referer's qid, the matrix self-check's zse93 legs).
type Endpoint struct {
	Name    string // report/sample name, e.g. "hot-list"
	Referer string
	ZSE93   bool // send x-zse-93: XZse93 (required by the meta gate; poison on cursorless feeds)
	Cookie  bool // send the cookie when one is loaded
	Gated   bool // cookie REQUIRED: without one, ErrLoginRequired — no request is sent
	Family  Family
}

// The matrix. Referers are per the 2026-11 measurement; the x-zse-93
// legs follow the gate table (questions demands it, feeds-forbids it —
// both gates measured relaxed in the wild on 2026-11-12, the shape is
// kept anyway as the documented contract).
var (
	epHotList = Endpoint{
		Name: "hot-list", Referer: "https://www.zhihu.com/hot",
		Cookie: true, Family: FamilyAnon, // anonymous door; rides the cookie when present, never needs it
	}
	epHotWWW = Endpoint{
		Name: "hot-www-door", Referer: "https://www.zhihu.com/hot",
		Cookie: true, Gated: true, Family: FamilyList, // session door (anon 401 code=101), backup of the api twin
	}
	epDailyLatest = Endpoint{
		Name: "daily-latest", Referer: "https://daily.zhihu.com/",
		Family: FamilyAnon,
	}
	epDailyStory = Endpoint{
		Name: "daily-story", Referer: "https://daily.zhihu.com/",
		Family: FamilyAnon,
	}
	epMoments = Endpoint{
		Name: "moments", Referer: "https://www.zhihu.com/follow",
		Cookie: true, Gated: true, Family: FamilyList, // v3: 403 = risk control, raw error (follow feed, ticket 05)
	}
	epRecommend = Endpoint{
		Name: "recommend", Referer: "https://www.zhihu.com/",
		Cookie: true, Gated: true, Family: FamilyList, // v3 session door, the home page's own feed (ticket 08); 403 = risk control, raw error
	}
	epQuestionMeta = Endpoint{
		Name: "question-meta", Referer: "https://www.zhihu.com/question/", // + qid at the call site
		ZSE93: true, Cookie: true, Gated: true, Family: FamilyDetail,
	}
	epAnswersFeeds = Endpoint{
		Name: "answers-feeds", Referer: "https://www.zhihu.com/question/", // + qid at the call site
		ZSE93: false, Cookie: true, Gated: true, Family: FamilyDetail, // cursorless entry REJECTS the version header
	}
	epCommentsRoot = Endpoint{
		Name: "comments-root", Referer: "https://www.zhihu.com/",
		Cookie: true, Gated: true, Family: FamilyDetail, // x-zse-93 harmless here; not sent (comment_v5 doesn't gate)
	}
	epChildComments = Endpoint{
		Name: "child-comments", Referer: "https://www.zhihu.com/",
		Cookie: true, Gated: true, Family: FamilyDetail, // the root's child-gap completion walk (ticket 06)
	}
	epArticle = Endpoint{
		Name: "article-meta", Referer: "https://zhuanlan.zhihu.com/p/", // + id at the call site
		Cookie: true, Gated: true, Family: FamilyDetail, // zhuanlan door (probe 09: www /api/v4/articles 403s 10003 cookie or not); anonymous 403, cookie 200, no version header
	}
	epArticleComments = Endpoint{
		Name: "article-comments", Referer: "https://zhuanlan.zhihu.com/p/", // + id at the call site
		Cookie: true, Gated: true, Family: FamilyDetail, // comment_v5 doesn't gate the version header
	}
)

// Transport is one host set + cookie + client behind every zhihu GET.
// Cheap to build (the source constructs one per refresh, which is what
// makes a Ctrl+U re-login effective on the next F5 without a restart);
// safe for concurrent use (one Transport per fetch flow, no mutation).
type Transport struct {
	hosts  Hosts
	cookie string // normalized Cookie header; "" = anonymous
	client *http.Client
}

// NewTransport builds a transport. client nil → default client with
// fetchTimeout (tests inject the fake server's client or nil —
// httptest on 127.0.0.1 needs neither TLS config nor proxy tweaks).
func NewTransport(hosts Hosts, cookie string, client *http.Client) *Transport {
	return &Transport{hosts: hosts, cookie: cookie, client: client}
}

// Response is one raw outcome. Body rides along even on non-2xx — the
// probe saves the error document (zhihu's error JSON carries the code,
// 10003 and friends), and the source names the status.
type Response struct {
	Status int
	Body   []byte
	Err    error
}

// Get performs one matrix GET. Common headers first (UA, Accept,
// Accept-Language, x-requested-with), then the endpoint's own legs
// (Referer, x-zse-93, Cookie), then the family's reading of
// the answer: detail 401/403 → ErrSessionExpired, everything else
// non-200 → a plain error carrying name and status.
func (t *Transport) Get(ctx context.Context, ep Endpoint, url string) Response {
	if ep.Gated && t.cookie == "" {
		return Response{Err: fmt.Errorf("%s: %w", ep.Name, ErrLoginRequired)}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Response{Err: fmt.Errorf("%s: %w", ep.Name, err)}
	}
	h := req.Header
	h.Set("User-Agent", DesktopUA)
	h.Set("Accept", "*/*")
	h.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	h.Set("x-requested-with", "fetch")
	if ep.Referer != "" {
		h.Set("Referer", ep.Referer)
	}
	if ep.ZSE93 {
		h.Set("x-zse-93", XZse93)
	}
	if ep.Cookie && t.cookie != "" {
		h.Set("Cookie", t.cookie)
	}
	client := t.client
	if client == nil {
		client = &http.Client{Timeout: fetchTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Response{Err: fmt.Errorf("%s: %w", ep.Name, err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return Response{Status: resp.StatusCode, Err: fmt.Errorf("%s: %w", ep.Name, err)}
	}
	res := Response{Status: resp.StatusCode, Body: body}
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusNotFound && ep.Family == FamilyDetail:
		// The question (or the answer behind a comments call) is gone:
		// deleted upstream, or the id never existed. A distinct error
		// so the UI can say 不存在或已删除 instead of a raw status.
		res.Err = fmt.Errorf("%s: %w (HTTP 404)", ep.Name, ErrContentGone)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		if ep.Family == FamilyDetail {
			// Stale z_c0, a bumped version gate, or risk control —
			// indistinguishable client-side; the detail family reads
			// them all as session-expired and points at Ctrl+U (the
			// probe's report is where 10003 gets told apart).
			res.Err = fmt.Errorf("%s: %w (HTTP %d)", ep.Name, ErrSessionExpired, resp.StatusCode)
		} else {
			res.Err = fmt.Errorf("%s: HTTP %d", ep.Name, resp.StatusCode)
		}
	default:
		res.Err = fmt.Errorf("%s: HTTP %d", ep.Name, resp.StatusCode)
	}
	return res
}
