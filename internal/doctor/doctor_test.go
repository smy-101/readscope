package doctor

// doctor_test.go — the library check (M6 ticket 02): doctor rides
// the same library.Scan seam the bookshelf does and reports the
// live census — shelved books, hard failures, warned books — or the
// honest fresh-install note for an empty library.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/epub/fixture"
	"github.com/smy-101/readscope/internal/i18n"
)

func doctorEnv(t *testing.T) (*core.Config, string) {
	t.Helper()
	t.Setenv("READSCOPE_STATE", filepath.Join(t.TempDir(), "state"))
	books := t.TempDir()
	cfg := core.Defaults()
	cfg.BookDir = books
	cfg.Language = "en"
	return cfg, books
}

func shelveFixture(t *testing.T, dir, name string, files []fixture.File) {
	t.Helper()
	src := fixture.Write(t, files)
	if err := os.Rename(src, filepath.Join(dir, name)); err != nil {
		t.Fatalf("shelve %s: %v", name, err)
	}
}

func TestLibraryCheckEmpty(t *testing.T) {
	cfg, _ := doctorEnv(t)
	cat, _ := i18n.Load("en")
	c := libraryCheck(cat, cfg)
	if c.err != nil {
		t.Fatalf("empty library must pass: %v", c.err)
	}
	if !strings.Contains(c.note, "no books yet") {
		t.Fatalf("note = %q, want the fresh-install note", c.note)
	}
}

func TestLibraryCheckCensus(t *testing.T) {
	cfg, books := doctorEnv(t)
	shelveFixture(t, books, "good.epub", fixture.ValidEPUB3())
	shelveFixture(t, books, "dirty.epub", fixture.MissingNCX()) // 1 warning
	shelveFixture(t, books, "drm.epub", fixture.DRMEncrypted()) // hard fail
	cat, _ := i18n.Load("en")

	c := libraryCheck(cat, cfg)
	if c.err != nil {
		t.Fatalf("census must pass: %v", c.err)
	}
	for _, want := range []string{"2 books", "1 broken", "1 with warnings"} {
		if !strings.Contains(c.note, want) {
			t.Errorf("note %q missing %q", c.note, want)
		}
	}
}
