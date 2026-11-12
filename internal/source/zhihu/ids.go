package zhihu

import (
	"math/big"
	"regexp"
)

// qidRE matches the question id segment in both URL spellings zhihu
// serves: www's /question/<id> and the api host's internal
// /questions/<id> (the hot-list target.url carries the latter).
var qidRE = regexp.MustCompile(`/questions?/([0-9]+)`)

// QIDFromURL extracts the question id from a zhihu URL — the ONE
// precise source of question ids. Hot-list target.id arrives as an
// unquoted integer beyond 2^53; any float64 decoder (JS's r.json,
// Go's default unmarshal) mangles its low digits, and the mangled id
// 404s on open. The URL is a string end-to-end and exact.
func QIDFromURL(u string) string {
	if m := qidRE.FindStringSubmatch(u); m != nil {
		return m[1]
	}
	return ""
}

// jsMaxInt is 2^53, the exact-integer ceiling of IEEE 754 doubles.
var jsMaxInt = big.NewInt(1 << 53)

// OverJSMaxInt reports whether a decimal id string exceeds 2^53 — the
// flag the probe prints next to each url-extracted qid, so the
// precision contract stays visible in every sample run. big.Int,
// not ParseInt: real hot-list qids are 19 digits, past even int64.
func OverJSMaxInt(id string) bool {
	n, ok := new(big.Int).SetString(id, 10)
	if !ok || n.Sign() < 0 {
		return false
	}
	return n.Cmp(jsMaxInt) > 0
}
