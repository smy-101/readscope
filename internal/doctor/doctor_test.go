package doctor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/core"
)

func runDoctor(t *testing.T) (string, bool) {
	t.Helper()
	t.Setenv("READSCOPE_STATE", filepath.Join(t.TempDir(), "state"))
	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "config"))
	cfg := core.Defaults()
	var buf bytes.Buffer
	ok := Run(&buf, cfg, "test")
	return buf.String(), ok
}

func TestRunAllPass(t *testing.T) {
	out, ok := runDoctor(t)
	if !ok {
		t.Fatalf("doctor reported failure:\n%s", out)
	}
	for _, label := range []string{
		"config directory", "state directory", "open database",
		"database schema", "language packs", "themes",
	} {
		if !strings.Contains(out, label) {
			t.Errorf("report missing check %q:\n%s", label, out)
		}
	}
	if !strings.Contains(out, "6 checks: 6 ok, 0 failed") {
		t.Errorf("summary line wrong:\n%s", out)
	}
}

func TestRunDetectsBrokenDB(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("READSCOPE_STATE", dir)
	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "config"))
	// Garbage where SQLite expects a database file.
	if err := os.WriteFile(filepath.Join(dir, "readscope.db"), []byte("this is not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := core.Defaults()
	var buf bytes.Buffer
	ok := Run(&buf, cfg, "test")
	if ok {
		t.Fatalf("doctor passed against a corrupt db:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "✗") {
		t.Errorf("expected a failure marker:\n%s", buf.String())
	}
}
