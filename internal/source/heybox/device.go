package heybox

// device.go — the install-time device fingerprint: one random 32-hex
// id, created on first use and persisted in the config directory,
// reused by every request from then on (spec user story 24: anonymous
// access carries ONLY this fingerprint — no account identity ever
// rides along). The web endpoints personalize the anonymous recommend
// feed by device, which is exactly why the id must survive restarts
// or the feed would re-cold-start every launch.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/smy-101/readscope/internal/core"
)

// DevicePath is the device-id file's single spelling — <config
// dir>/heybox.device — shared by the source constructor, tests and
// (later) doctor, so they can never disagree about where the
// fingerprint lives. The zhihu.cookies pattern, one file per
// per-install fact.
func DevicePath() (string, error) {
	dir, err := core.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "heybox.device"), nil
}

// LoadOrCreateDeviceID is the fingerprint's whole lifecycle: read the
// file if it exists, otherwise mint 16 random bytes as 32 hex chars
// and persist them (mkdir -p first, 0600 via temp file + rename so a
// umask-looser write can never land). The return is the id itself;
// errors name the file, and a caller that cannot get one refuses
// loudly rather than silently re-minting a new identity per request
// (which would burn the feed's personalization and look like a fresh
// device on every refresh).
func LoadOrCreateDeviceID(path string) (string, error) {
	if raw, err := os.ReadFile(path); err == nil {
		if id := parseDeviceID(string(raw)); id != "" {
			return id, nil
		}
		return "", fmt.Errorf("heybox device id: %s: not a 32-hex device id — delete the file to re-mint", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("heybox device id: %w", err)
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("heybox device id: mint: %w", err)
	}
	id := hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("heybox device id: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(id+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("heybox device id: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", fmt.Errorf("heybox device id: %w", err)
	}
	return id, nil
}

// isLowerHex reports whether s is a non-empty string of [0-9a-f] —
// the shape both the device fingerprint and the hex link_id share.
// One spelling (the 01 review's transfer: device.go and source.go
// had twin rune loops).
func isLowerHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// parseDeviceID accepts a 32-hex-char line with optional surrounding
// whitespace; anything else is "" (the caller decides what that
// means — a re-mint for a blank new file, an error for a corrupt
// one).
func parseDeviceID(raw string) string {
	id := strings.TrimSpace(raw)
	if len(id) != 32 || !isLowerHex(id) {
		return ""
	}
	return id
}
