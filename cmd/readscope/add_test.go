package main

// add_test.go — the `readscope add` CLI (ticket 02): copy one EPUB
// into the configured library directory, refuse same-name conflicts
// naming the existing path (zero writes), and report what the parse
// layer thinks of the copy (title, warnings, or the typed refusal).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/epub/fixture"
)

// addBookEnv points the CLI at a temp config whose book_dir is a
// temp library directory, English output.
func addBookEnv(t *testing.T) string {
	t.Helper()
	t.Setenv("LC_ALL", "en_US.UTF-8")
	cfgDir := t.TempDir()
	t.Setenv("READSCOPE_CONFIG", cfgDir)
	t.Setenv("READSCOPE_STATE", filepath.Join(t.TempDir(), "state"))
	books := filepath.Join(t.TempDir(), "books")
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"),
		[]byte("language = \"en\"\nbook_dir = \""+books+"\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return books
}

// runAddOut runs one add capturing its stdout and exit code together.
func runAddOut(t *testing.T, args []string) (string, int) {
	t.Helper()
	var out string
	var code int
	out = captureStdout(t, func() { code = runAdd(args) })
	return out, code
}

func TestRunAddCopiesAndScanSeesIt(t *testing.T) {
	books := addBookEnv(t)
	src := fixture.Write(t, fixture.ValidEPUB3())

	out, code := runAddOut(t, []string{src})
	if code != 0 {
		t.Fatalf("runAdd = %d, want 0", code)
	}
	dst := filepath.Join(books, filepath.Base(src))
	if fi, err := os.Stat(dst); err != nil || fi.Size() == 0 {
		t.Fatalf("copy missing: %v", err)
	}
	if !strings.Contains(out, "合成书 · EPUB3") {
		t.Fatalf("report should carry the parsed title, got %q", out)
	}
}

func TestRunAddRefusesDuplicateZeroWrite(t *testing.T) {
	books := addBookEnv(t)
	first := fixture.Write(t, fixture.ValidEPUB3())
	if _, code := runAddOut(t, []string{first}); code != 0 {
		t.Fatalf("first add = %d", code)
	}
	dst := filepath.Join(books, filepath.Base(first))
	before, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}

	// A different file that happens to carry the same name; the
	// refusal happens before any parse, so its bytes are arbitrary.
	second := filepath.Join(t.TempDir(), filepath.Base(first))
	if err := os.WriteFile(second, []byte("different book entirely"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runAddOut(t, []string{second})
	if code != 1 {
		t.Fatalf("duplicate add = %d, want 1", code)
	}
	after, _ := os.ReadFile(dst)
	if string(before) != string(after) {
		t.Fatal("refused add must not touch the existing file")
	}
	if !strings.Contains(out, "already has a file with this name") || !strings.Contains(out, dst) {
		t.Fatalf("refusal should name the existing path, got %q", out)
	}
}

func TestRunAddUsageAndMissing(t *testing.T) {
	addBookEnv(t)
	if code := runAdd(nil); code != 2 {
		t.Fatalf("no args = %d, want 2", code)
	}
	if code := runAdd([]string{"/no/such/book.epub"}); code != 1 {
		t.Fatalf("missing file = %d, want 1", code)
	}
	if code := runAdd([]string{"a.epub", "b.epub"}); code != 2 {
		t.Fatalf("two args = %d, want 2", code)
	}
}

func TestRunAddReportsBrokenCopies(t *testing.T) {
	books := addBookEnv(t)
	src := fixture.Write(t, fixture.DRMEncrypted())
	out, code := runAddOut(t, []string{src})
	if code != 0 {
		t.Fatalf("a refused book still copies (delete-unshelves handles it), code = %d", code)
	}
	if _, err := os.Stat(filepath.Join(books, filepath.Base(src))); err != nil {
		t.Fatalf("copy missing: %v", err)
	}
	if !strings.Contains(out, "DRM") {
		t.Fatalf("report should explain the refusal, got %q", out)
	}
}

// TestRunAddComposedZH: the copy report is a composed product —
// asserted in Chinese too (CODING_STANDARDS: key parity cannot catch
// a bad composition, only a missing key).
func TestRunAddComposedZH(t *testing.T) {
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	cfgDir := t.TempDir()
	t.Setenv("READSCOPE_CONFIG", cfgDir)
	t.Setenv("READSCOPE_STATE", filepath.Join(t.TempDir(), "state"))
	books := filepath.Join(t.TempDir(), "books")
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"),
		[]byte("language = \"zh-CN\"\nbook_dir = \""+books+"\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	src := fixture.Write(t, fixture.ValidEPUB3())
	out, code := runAddOut(t, []string{src})
	if code != 0 {
		t.Fatalf("runAdd = %d, want 0", code)
	}
	for _, want := range []string{"已拷入：", "《合成书 · EPUB3》", "下次扫描即上架"} {
		if !strings.Contains(out, want) {
			t.Errorf("zh report missing %q: %q", want, out)
		}
	}
}
