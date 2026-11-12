package zhihu

import (
	"os"
	"strings"
)

// CookieInfo summarizes a loaded cookie header without ever carrying
// its values — probe output and doctor reports print this, never the
// header itself.
type CookieInfo struct {
	KeyCount int  // k=v pairs seen
	HasZC0   bool // z_c0: the login-session key
	HasDC0   bool // d_c0: the device key
}

// LoadCookie reads the cookie file — raw Cookie-header text pasted
// from a browser, possibly wrapped across lines by the terminal — and
// returns it normalized into one valid header value. A missing file
// is the caller's decision (the probe walks the anonymous endpoints),
// so the os error passes through unchanged.
func LoadCookie(path string) (string, CookieInfo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", CookieInfo{}, err
	}
	return ParseCookie(string(raw)), SummarizeCookie(string(raw)), nil
}

// ParseCookie normalizes a pasted Cookie header: each line trimmed,
// stray trailing semicolons dropped, empties dropped, joined with
// "; ". A single-line paste round-trips verbatim; a wrapped one
// (each fragment ending in ";") becomes one valid header again.
func ParseCookie(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		l = strings.TrimSuffix(l, ";")
		if l = strings.TrimSpace(l); l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, "; ")
}

// SummarizeCookie censuses the header: pair count plus presence of
// the two keys zhihu's APIs actually read. Key detection matches
// "z_c0=" as a pair start so a decoy like "xz_c0=" doesn't count.
func SummarizeCookie(header string) CookieInfo {
	info := CookieInfo{}
	for _, part := range strings.Split(header, ";") {
		kv := strings.TrimSpace(part)
		if kv == "" {
			continue
		}
		info.KeyCount++
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		switch kv[:eq] {
		case "z_c0":
			info.HasZC0 = true
		case "d_c0":
			info.HasDC0 = true
		}
	}
	return info
}
