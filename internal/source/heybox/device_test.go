package heybox

// device_test.go — the install-time fingerprint's lifecycle: mint
// once, persist, reuse forever (spec user story 24 — the anonymous
// identity must not churn, or the feed's device-keyed personalization
// resets on every launch).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateDeviceIDMintsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heybox.device")
	id1, err := LoadOrCreateDeviceID(path)
	if err != nil {
		t.Fatalf("first LoadOrCreateDeviceID: %v", err)
	}
	if len(id1) != 32 || strings.ToLower(id1) != id1 {
		t.Fatalf("minted id %q is not 32 lowercase hex chars", id1)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("device file not written: %v", err)
	}
	if strings.TrimSpace(string(raw)) != id1 {
		t.Errorf("file holds %q, want the minted id %q", raw, id1)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %v, want 0600", perm)
	}

	// The restart contract: a second load (fresh process, same file)
	// answers the same id — never a re-mint.
	id2, err := LoadOrCreateDeviceID(path)
	if err != nil {
		t.Fatalf("second LoadOrCreateDeviceID: %v", err)
	}
	if id2 != id1 {
		t.Errorf("second load = %q, want the persisted %q", id2, id1)
	}
}

func TestLoadOrCreateDeviceIDCorruptFileRefuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heybox.device")
	if err := os.WriteFile(path, []byte("not-hex-at-all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateDeviceID(path); err == nil {
		t.Fatal("a corrupt device file must refuse, not silently re-mint")
	}
}

func TestDevicePathUnderConfigDir(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", t.TempDir())
	path, err := DevicePath()
	if err != nil {
		t.Fatalf("DevicePath: %v", err)
	}
	if !strings.HasSuffix(path, "heybox.device") {
		t.Errorf("path = %q, want the heybox.device spelling", path)
	}
}
