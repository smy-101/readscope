// Package heybox implements the heybox (小黑盒) source on its
// anonymous read-only web APIs (M5): the recommendation feed first
// (ticket 01), circle feeds and the two-layer post detail in tickets
// 02/03. Zero login, zero writes — the three permanent red lines of
// ADR #16 (no write operations, no native-signature reversing, no
// captcha bypassing); risk-control refusals surface as a "try later"
// hint and never touch the cache.
//
// The request shape is the smoke-verified variant A contract
// (.scratch/m5-heybox/smoke.md, 2026-11-13): desktop Chrome UA, the
// web Referer, the 999.0.3 parameter set, and the hkey/_time/nonce
// query signature that signs the canonical path only. The transport
// (transport.go) owns all of it single-point.
package heybox

// heybox.go — shared constants.

// ProdBase is the API root every heybox request rides; the web client
// the contract was measured from talks to this host only.
const ProdBase = "https://api.xiaoheihe.cn"

// DesktopUA is the browser identity every heybox request wears. Like
// zhihu, this package deliberately does NOT use source.UserAgent: the
// web endpoints are risk-controlled and a non-browser fingerprint is
// one of the things they gate on (the smoke ran desktop Chrome UA
// end to end, zero refusals). Single point of maintenance — the
// version switch's neighbor on the stop-loss ladder.
const DesktopUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// WebReferer is the site Referer the web client sends on every API
// call; the smoke carried it on all legs.
const WebReferer = "https://www.xiaoheihe.cn/"
