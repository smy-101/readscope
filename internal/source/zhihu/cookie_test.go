package zhihu

// Ticket 01 tests: cookie file loading — raw Cookie-header text, pasted
// from a browser, possibly wrapped across lines. Presence of the two
// session keys is detected without ever printing values.

import (
	"os"
	"path/filepath"
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
