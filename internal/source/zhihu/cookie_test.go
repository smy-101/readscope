package zhihu

// Ticket 01 tests: cookie file loading — raw Cookie-header text, pasted
// from a browser, possibly wrapped across lines. Presence of the two
// session keys is detected without ever printing values.
// Ticket 04 adds the save side: validation (z_c0 or refuse), 0600
// atomic persistence, and the shared path spelling.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCookie(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zhihu.cookies")

	t.Run("missing file", func(t *testing.T) {
		_, _, err := LoadCookie(path)
		if !os.IsNotExist(err) {
			t.Errorf("err = %v, want IsNotExist", err)
		}
	})

	t.Run("single line", func(t *testing.T) {
		os.WriteFile(path, []byte("z_c0=2+1.0x/secret; d_c0=2+1.0x/dev; xhihu=yes"), 0o600)
		header, info, err := LoadCookie(path)
		if err != nil {
			t.Fatalf("LoadCookie: %v", err)
		}
		if header != "z_c0=2+1.0x/secret; d_c0=2+1.0x/dev; xhihu=yes" {
			t.Errorf("header = %q", header)
		}
		if !info.HasZC0 || !info.HasDC0 {
			t.Errorf("HasZC0/HasDC0 = %v/%v, want true/true", info.HasZC0, info.HasDC0)
		}
		if info.KeyCount != 3 {
			t.Errorf("KeyCount = %d, want 3", info.KeyCount)
		}
	})

	t.Run("wrapped paste joins with semicolon", func(t *testing.T) {
		os.WriteFile(path, []byte("z_c0=abc;\n d_c0=def;\nk3=v3\n"), 0o600)
		header, info, err := LoadCookie(path)
		if err != nil {
			t.Fatalf("LoadCookie: %v", err)
		}
		if header != "z_c0=abc; d_c0=def; k3=v3" {
			t.Errorf("header = %q, want lines joined by \"; \"", header)
		}
		if info.KeyCount != 3 {
			t.Errorf("KeyCount = %d, want 3", info.KeyCount)
		}
	})

	t.Run("no z_c0", func(t *testing.T) {
		os.WriteFile(path, []byte("d_c0=def"), 0o600)
		_, info, err := LoadCookie(path)
		if err != nil {
			t.Fatalf("LoadCookie: %v", err)
		}
		if info.HasZC0 {
			t.Error("HasZC0 = true, want false")
		}
	})
}

func TestParseCookieGarbageNeverPanics(t *testing.T) {
	// The login ceremony accepts free-form pastes; garbage must
	// normalize to something censusable, never crash — refusal is the
	// save step's job (ErrNoZC0 check below), not the parser's.
	for raw, want := range map[string]string{
		"hello world":   "hello world", // not k=v: kept, censused, refused at save
		";;;":           "",
		"  ":            "",
		"":              "",
		"z_c0=abc;;;":   "z_c0=abc",
		"xz_c0=decoy=1": "xz_c0=decoy=1", // decoy key: parses, must not count as z_c0
	} {
		if got := ParseCookie(raw); got != want {
			t.Errorf("ParseCookie(%q) = %q, want %q", raw, got, want)
		}
	}
	if info := SummarizeCookie(ParseCookie("xz_c0=decoy=1")); info.HasZC0 {
		t.Error("xz_c0= must not census as z_c0 (pair-start match only)")
	}
}

func TestCookiePathHonorsConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("READSCOPE_CONFIG", dir)
	path, err := CookiePath()
	if err != nil {
		t.Fatalf("CookiePath: %v", err)
	}
	if want := filepath.Join(dir, "zhihu.cookies"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestSaveCookieValidWrites0600AndNormalized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zhihu.cookies")

	info, err := SaveCookie(path, "z_c0=2+1.0x/secret;\n d_c0=2+1.0x/dev;\nk3=v3\n")
	if err != nil {
		t.Fatalf("SaveCookie: %v", err)
	}
	if info.KeyCount != 3 || !info.HasZC0 || !info.HasDC0 {
		t.Errorf("info = %+v, want 3 keys with z_c0/d_c0", info)
	}

	// 0600 exactly — the file carries a live session.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 0600", perm)
	}
	// Content is the normalized single header, and LoadCookie
	// round-trips it (the constructor's load path).
	header, _, err := LoadCookie(path)
	if err != nil {
		t.Fatalf("LoadCookie: %v", err)
	}
	if header != "z_c0=2+1.0x/secret; d_c0=2+1.0x/dev; k3=v3" {
		t.Errorf("stored header = %q", header)
	}
}

func TestSaveCookieCreatesConfigDir(t *testing.T) {
	// First run: ~/.config/readscope may not exist yet (t.TempDir
	// stands in); the save must create it, not fail.
	path := filepath.Join(t.TempDir(), "config", "zhihu.cookies")
	if _, err := SaveCookie(path, "z_c0=abc"); err != nil {
		t.Fatalf("SaveCookie: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cookie file missing: %v", err)
	}
}

func TestSaveCookieRefusesGarbage(t *testing.T) {
	dir := t.TempDir()
	for _, raw := range []string{"", "\n\n", "hello world", "d_c0=dev; k=v"} {
		path := filepath.Join(dir, "zhihu.cookies")
		_, err := SaveCookie(path, raw)
		if err == nil || !errors.Is(err, ErrNoZC0) {
			t.Errorf("SaveCookie(%q) err = %v, want ErrNoZC0", raw, err)
		}
		// Refusal means refusal: no file, no half-written session.
		if _, statErr := os.Stat(path); statErr == nil {
			t.Errorf("SaveCookie(%q) must not leave a file behind", raw)
		}
	}
	if !strings.Contains(fmt.Sprint(ErrNoZC0), "z_c0") {
		t.Error("the sentinel must name z_c0 — the error is the user's next step")
	}
}

func TestSaveCookieReplacesWiderPerms(t *testing.T) {
	// A pre-existing 0644 cookie (hand-written once, say) must not
	// survive a re-save: temp-file + rename lands the new file 0600.
	path := filepath.Join(t.TempDir(), "zhihu.cookies")
	os.WriteFile(path, []byte("z_c0=old"), 0o644)
	if _, err := SaveCookie(path, "z_c0=new; d_c0=dev"); err != nil {
		t.Fatalf("SaveCookie: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode after re-save = %o, want 0600", perm)
	}
	header, _, _ := LoadCookie(path)
	if header != "z_c0=new; d_c0=dev" {
		t.Errorf("stored header = %q, want the re-pasted one", header)
	}
}
