package zhihu

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/smy-101/readscope/internal/core"
)

// CookieInfo summarizes a loaded cookie header without ever carrying
// its values — probe output and doctor reports print this, never the
// header itself.
type CookieInfo struct {
	KeyCount int  // k=v pairs seen
	HasZC0   bool // z_c0: the login-session key
	HasDC0   bool // d_c0: the device key
}

// ErrNoZC0 is the login ceremony's one refusal: a paste
// with no z_c0 pair cannot authenticate anything (z_c0 IS the
// session), so neither the CLI login nor the Ctrl+U dialog writes it.
// The wrapped message names the next step; no value ever rides along.
var ErrNoZC0 = errors.New("no z_c0 in the pasted header — copy the full Cookie request header from DevTools")

// CookiePath is the cookie file's single spelling — <config
// dir>/zhihu.cookies — shared by the CLI login, the TUI's Ctrl+U
// dialog, the source constructor and doctor, so they can never
// disagree about where the session lives.
func CookiePath() (string, error) {
	dir, err := core.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "zhihu.cookies"), nil
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

// SaveCookie is the login ceremony's write half: validate the pasted
// text (must parse into pairs carrying z_c0), then persist it as the
// normalized header with mode 0600 — via temp file + rename, so the
// session never exists at a umask-looser mode and a hand-written
// 0644 file cannot survive a re-save. Refusal (no z_c0) writes
// nothing; filesystem errors pass through for the caller to name.
func SaveCookie(path, raw string) (CookieInfo, error) {
	header := ParseCookie(raw)
	info := SummarizeCookie(header)
	if strings.TrimSpace(header) == "" {
		return CookieInfo{}, fmt.Errorf("empty paste: %w", ErrNoZC0)
	}
	if !info.HasZC0 {
		return info, fmt.Errorf("%d pairs parsed, none is z_c0: %w", info.KeyCount, ErrNoZC0)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return info, err
	}
	tmp, err := os.CreateTemp(dir, ".zhihu.cookies-*")
	if err != nil {
		return info, err
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(header); err != nil {
		tmp.Close()
		os.Remove(name)
		return info, err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(name)
		return info, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return info, err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return info, err
	}
	return info, nil
}

// ParseCookie normalizes a pasted Cookie header: each line trimmed,
// stray trailing semicolons dropped (all of them — a wrapped paste
// can leave several), empties dropped, joined with "; ". A
// single-line paste round-trips verbatim; a wrapped one (each
// fragment ending in ";") becomes one valid header again.
func ParseCookie(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimRight(strings.TrimSpace(l), " \t;"); l != "" {
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
