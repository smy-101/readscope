// Package zhihu implements the zhihu source on its cookie-gated JSON
// APIs (M4): the hot list and the daily digest anonymously, the
// follow feed and question details behind a pasted browser cookie —
// 免签 (GLOSSARY: 免签) direct requests, no resident browser (ADR
// #14). Ticket 01 landed the data-plane tracer (`readscope zhihu
// probe`); ticket 02 the Source registration and the hot feed;
// ticket 04 the shared transport (transport.go — one request matrix
// and one set of session-loss semantics for the whole package) plus
// the cookie login ceremony (`readscope zhihu login`, Ctrl+U). The
// daily feed, follow feed and question details follow in tickets
// 03/05/06.
//
// The request shape mirrors FeedScope's 2026-11 measured matrix: a
// desktop Chrome UA on every call, x-requested-with: fetch, a
// per-endpoint Referer, and x-zse-93 (a static client-version
// declaration) only where the endpoint demands it.
package zhihu

// zhihu.go — shared constants.

// DesktopUA is the browser identity every zhihu request wears. Zhihu's
// JSON APIs risk-control non-browser fingerprints, so this package
// deliberately does NOT use source.UserAgent (see the exception noted
// there; rss/hn hosts don't care, zhihu's might). Single point of
// maintenance — the stop-loss ladder's first rung swaps this string.
const DesktopUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// XZse93 is the value the x-zse-93 client-version declaration
// carries on the v4/questions meta endpoint (missing → HTTP 403 with
// error code 10003). A static constant the page sends on every API
// request (FeedScope 2026-11); when zhihu bumps it, this is the one
// line to change — and until then, a detail-family 403 reads as
// session-expired at the transport (indistinguishable client-side;
// the probe's report tells 10003 apart).
const XZse93 = "101_3_3.0"

// Hosts bundles zhihu's three API roots: the app-domain twin of the
// hot list, the daily digest service, and the www gateway everything
// else rides on. READSCOPE_ZHIHU_BASE overrides all three in the cmd
// wiring (tests point them at one httptest server; debugging a proxy
// can too).
type Hosts struct {
	API   string // https://api.zhihu.com
	Daily string // https://news-at.zhihu.com
	Web   string // https://www.zhihu.com
}

// ProdHosts are the real-world roots.
var ProdHosts = Hosts{
	API:   "https://api.zhihu.com",
	Daily: "https://news-at.zhihu.com",
	Web:   "https://www.zhihu.com",
}
