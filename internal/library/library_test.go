package library

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/epub"
	"github.com/smy-101/readscope/internal/epub/fixture"
	"github.com/smy-101/readscope/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "lib.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

// shelve places a fixture-built book at dir/relPath, the way a user's
// file manager would (nested subdirectories included).
func shelve(t *testing.T, dir, relPath string, files []fixture.File) string {
	t.Helper()
	src := fixture.Write(t, files)
	dst := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("shelve %s: %v", relPath, err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatalf("shelve %s: %v", relPath, err)
	}
	return dst
}

func TestDirExpandsHome(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cfg := core.Defaults() // BookDir = "~/Books"
	dir, err := Dir(cfg)
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if dir != "/home/tester/Books" {
		t.Fatalf("Dir = %q, want /home/tester/Books", dir)
	}
	cfg.BookDir = "/abs/books"
	if dir, _ := Dir(cfg); dir != "/abs/books" {
		t.Fatalf("absolute BookDir must pass through, got %q", dir)
	}
}

func TestScanShelvesAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	s := newStore(t)
	shelve(t, dir, "good.epub", fixture.ValidEPUB3())

	res, err := Scan(t.Context(), s, dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Scanned != 1 || res.New != 1 || res.Updated != 0 || len(res.Failed) != 0 || res.Warned != 0 {
		t.Fatalf("first scan = %+v", res)
	}
	rows, err := s.BookRows(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("BookRows = %d, %v", len(rows), err)
	}
	if rows[0].Title != "合成书 · EPUB3" || rows[0].Author != "作者甲" || rows[0].Language != "zh-CN" {
		t.Fatalf("metadata lost: %+v", rows[0])
	}
	if rows[0].Path != filepath.Join(dir, "good.epub") {
		t.Fatalf("path = %q", rows[0].Path)
	}

	// Rescan with nothing changed: zero writes (no New, no Updated).
	res, err = Scan(t.Context(), s, dir)
	if err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if res.New != 0 || res.Updated != 0 {
		t.Fatalf("idempotent rescan wrote: %+v", res)
	}

	// A different book at the same path: metadata change updates the row.
	shelve(t, dir, "good.epub", fixture.MinimalEPUB3())
	res, err = Scan(t.Context(), s, dir)
	if err != nil {
		t.Fatalf("rescan after change: %v", err)
	}
	if res.Updated != 1 {
		t.Fatalf("changed metadata must update: %+v", res)
	}
}

func TestScanCountsWarnings(t *testing.T) {
	dir := t.TempDir()
	s := newStore(t)
	shelve(t, dir, "dirty.epub", fixture.MissingNCX()) // TOC generated: 1 warning

	res, err := Scan(t.Context(), s, dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Warned != 1 {
		t.Fatalf("Warned = %d, want 1: %+v", res.Warned, res)
	}
	if rows, _ := s.BookRows(t.Context()); len(rows) != 1 {
		t.Fatal("dirty book must still be shelved")
	}
}

func TestScanHardFailsDoNotStopTheWalk(t *testing.T) {
	dir := t.TempDir()
	s := newStore(t)
	shelve(t, dir, "drm.epub", fixture.DRMEncrypted())
	if err := os.WriteFile(filepath.Join(dir, "garbage.epub"), []byte("not a zip at all"), 0o644); err != nil {
		t.Fatalf("garbage book: %v", err)
	}
	shelve(t, dir, "sub/ok.epub", fixture.MinimalEPUB3())

	res, err := Scan(t.Context(), s, dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(res.Failed) != 2 {
		t.Fatalf("Failed = %d, want 2: %+v", len(res.Failed), res)
	}
	byBase := map[string]error{}
	for _, f := range res.Failed {
		byBase[filepath.Base(f.Path)] = f.Err
	}
	if !errors.Is(byBase["drm.epub"], epub.ErrDRMUnsupported) {
		t.Fatalf("drm.epub err = %v, want ErrDRMUnsupported", byBase["drm.epub"])
	}
	if !errors.Is(byBase["garbage.epub"], epub.ErrInvalidZip) {
		t.Fatalf("garbage.epub err = %v, want ErrInvalidZip", byBase["garbage.epub"])
	}
	if res.New != 1 {
		t.Fatalf("the good book behind the failures must shelve: %+v", res)
	}
}

// A book that shelved fine and later broke on disk keeps its row and
// its reading progress — only disappearance delists (the pinned
// semantics: a temporarily broken file must not CASCADE away reading
// state).
func TestScanBrokenButPresentKeepsRowAndProgress(t *testing.T) {
	dir := t.TempDir()
	s := newStore(t)
	path := shelve(t, dir, "later-broken.epub", fixture.ValidEPUB3())
	res, err := Scan(t.Context(), s, dir)
	if err != nil || res.New != 1 {
		t.Fatalf("scan: %+v %v", res, err)
	}
	rows, _ := s.BookRows(t.Context())
	id := rows[0].ID
	if _, err := s.Exec(t.Context(),
		`INSERT INTO progress (book_id, spine_index, block_offset, pct, updated_at) VALUES (?, 0, 0, 0.5, ?)`,
		id, time.Now().UTC()); err != nil {
		t.Fatalf("seed progress: %v", err)
	}

	if err := os.WriteFile(path, []byte("now garbage"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	res, err = Scan(t.Context(), s, dir)
	if err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if res.Delisted != 0 || len(res.Failed) != 1 {
		t.Fatalf("broken-but-present must not delist: %+v", res)
	}
	rows, err = s.BookRows(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("row must survive a broken file: %d rows, %v", len(rows), err)
	}
	if !rows[0].HasProgress || rows[0].Pct != 0.5 {
		t.Fatalf("progress destroyed: %+v", rows[0])
	}
}

func TestScanDelistsVanished(t *testing.T) {
	dir := t.TempDir()
	s := newStore(t)
	path := shelve(t, dir, "gone.epub", fixture.ValidEPUB3())
	shelve(t, dir, "stays.epub", fixture.MinimalEPUB3())
	if _, err := Scan(t.Context(), s, dir); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}

	res, err := Scan(t.Context(), s, dir)
	if err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if res.Delisted != 1 {
		t.Fatalf("Delisted = %d, want 1: %+v", res.Delisted, res)
	}
	rows, _ := s.BookRows(t.Context())
	if len(rows) != 1 || rows[0].Path != filepath.Join(dir, "stays.epub") {
		t.Fatalf("wrong survivor: %+v", rows)
	}
}

func TestScanMissingDirIsEmpty(t *testing.T) {
	s := newStore(t)
	res, err := Scan(t.Context(), s, filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("missing dir must not error: %v", err)
	}
	if res.Scanned != 0 || len(res.Failed) != 0 {
		t.Fatalf("missing dir = %+v, want empty", res)
	}
}

// The scan is a synchronous fast operation at a thousand-book scale
// (ticket acceptance). The ceiling is generous — it exists to catch
// quadratic blowups (a per-book full-table rescan, say), not to
// benchmark disk speed.
func TestScanThousandBooks(t *testing.T) {
	if testing.Short() {
		t.Skip("thousand-book fixture build")
	}
	dir := t.TempDir()
	s := newStore(t)
	files := fixture.MinimalEPUB3()
	for i := 0; i < 1000; i++ {
		shelve(t, dir, "b"+strconv.Itoa(i)+".epub", files)
	}
	start := time.Now()
	res, err := Scan(t.Context(), s, dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	elapsed := time.Since(start)
	if res.Scanned != 1000 || res.New != 1000 {
		t.Fatalf("scan = %+v", res)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("scan of 1000 books took %v", elapsed)
	}
	t.Logf("1000-book scan: %v", elapsed)
}
