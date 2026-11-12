package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source/zhihu"
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

// TestZhihuCookieStates pins the doctor's cookie reporting: loaded
// session → census note; existing-but-broken (loose perms, no z_c0)
// → failure naming the fix. The missing-file pass lives in
// TestRunAllPass.
func TestZhihuCookieStates(t *testing.T) {
	cookieDir := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		t.Setenv("READSCOPE_CONFIG", dir)
		t.Setenv("READSCOPE_STATE", filepath.Join(t.TempDir(), "state"))
		return dir
	}
	newOut := func(t *testing.T) (string, bool) {
		t.Helper()
		cfg := core.Defaults()
		var buf bytes.Buffer
		ok := Run(&buf, cfg, "test")
		return buf.String(), ok
	}

	t.Run("loaded session reports census", func(t *testing.T) {
		dir := cookieDir(t)
		if _, err := zhihu.SaveCookie(filepath.Join(dir, "zhihu.cookies"),
			"z_c0=2+1.0x/secret; d_c0=2+1.0x/dev"); err != nil {
			t.Fatalf("SaveCookie: %v", err)
		}
		out, ok := newOut(t)
		if !ok {
			t.Fatalf("a healthy cookie must not fail doctor:\n%s", out)
		}
		if !strings.Contains(out, "2 keys · z_c0 ✓") {
			t.Errorf("census note missing:\n%s", out)
		}
	})

	t.Run("loose perms fail", func(t *testing.T) {
		dir := cookieDir(t)
		os.WriteFile(filepath.Join(dir, "zhihu.cookies"), []byte("z_c0=abc"), 0o644)
		out, ok := newOut(t)
		if ok {
			t.Fatalf("a 0644 cookie file must fail doctor:\n%s", out)
		}
		if !strings.Contains(out, "0600") {
			t.Errorf("perm failure should name the wanted mode:\n%s", out)
		}
	})

	t.Run("missing z_c0 fails", func(t *testing.T) {
		dir := cookieDir(t)
		os.WriteFile(filepath.Join(dir, "zhihu.cookies"), []byte("d_c0=only"), 0o600)
		out, ok := newOut(t)
		if ok {
			t.Fatalf("a z_c0-less cookie must fail doctor:\n%s", out)
		}
		if !strings.Contains(out, "z_c0 missing") {
			t.Errorf("failure should name z_c0:\n%s", out)
		}
	})
}

func TestRunAllPass(t *testing.T) {
	out, ok := runDoctor(t)
	if !ok {
		t.Fatalf("doctor reported failure:\n%s", out)
	}
	for _, label := range []string{
		"config directory", "state directory", "open database",
		"database schema", "language packs", "themes", "zhihu cookie",
	} {
		if !strings.Contains(out, label) {
			t.Errorf("report missing check %q:\n%s", label, out)
		}
	}
	if !strings.Contains(out, "7 checks: 7 ok, 0 failed") {
		t.Errorf("summary line wrong:\n%s", out)
	}
	// No cookie file is the anonymous mode: a pass with a note, not a
	// failure — hot/daily work without a session.
	if !strings.Contains(out, "not configured") {
		t.Errorf("anonymous cookie state should carry its note:\n%s", out)
	}
}

func TestCheckFeedsClassifies(t *testing.T) {
	t.Setenv("READSCOPE_CONFIG", filepath.Join(t.TempDir(), "config"))
	t.Setenv("READSCOPE_STATE", filepath.Join(t.TempDir(), "state"))
	cfg := core.Defaults()
	srcs := &core.Sources{Enabled: []string{"rss"}, RSS: core.RSSConfig{Feeds: map[string]core.RSSFeed{
		"fast": {Label: "Fast Feed", URL: "http://fast.example/feed"},
		"slow": {Label: "Slow Feed", URL: "http://slow.example/feed"},
		"down": {Label: "Down Feed", URL: "http://down.example/feed"},
	}}}
	probe := func(ctx context.Context, url string) (time.Duration, error) {
		switch url {
		case "http://slow.example/feed":
			return 3500 * time.Millisecond, nil
		case "http://down.example/feed":
			return 0, errors.New("connection refused")
		}
		return 120 * time.Millisecond, nil
	}
	var buf bytes.Buffer
	if ok := CheckFeeds(&buf, cfg, srcs, probe); ok {
		t.Fatalf("a down feed should fail the check:\n%s", buf.String())
	}
	for _, want := range []string{
		"feed reachability",
		"✓ Fast Feed (120ms)",
		"! Slow Feed (3.5s) · slow",
		"✗ Down Feed: connection refused",
		"3 feeds: 1 ok, 1 slow, 1 failed",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("feed report missing %q:\n%s", want, buf.String())
		}
	}
}

func writeSources(t *testing.T, body string) {
	t.Helper()
	cfgDir := t.TempDir()
	t.Setenv("READSCOPE_CONFIG", cfgDir)
	t.Setenv("READSCOPE_STATE", filepath.Join(t.TempDir(), "state"))
	if err := os.WriteFile(filepath.Join(cfgDir, "sources.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunFeedFailureFailsDoctor(t *testing.T) {
	writeSources(t, "enabled = [\"rss\"]\n[rss.feeds]\ndown = { label = \"Down Feed\", url = \"http://down.example/feed\" }\n")
	var buf bytes.Buffer
	ok := run(&buf, core.Defaults(), "test", func(ctx context.Context, url string) (time.Duration, error) {
		return 0, errors.New("connection refused")
	})
	if ok {
		t.Fatalf("feed failure should fail doctor:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "✗ Down Feed: connection refused") {
		t.Errorf("doctor report should name the failed feed:\n%s", buf.String())
	}
}

func TestRunFeedSuccessPassesDoctor(t *testing.T) {
	writeSources(t, "enabled = [\"rss\"]\n[rss.feeds]\nup = { label = \"Up Feed\", url = \"http://up.example/feed\" }\n")
	var buf bytes.Buffer
	ok := run(&buf, core.Defaults(), "test", func(ctx context.Context, url string) (time.Duration, error) {
		return 50 * time.Millisecond, nil
	})
	if !ok {
		t.Fatalf("reachable feed should pass doctor:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "✓ Up Feed") {
		t.Errorf("doctor report should list the feed:\n%s", buf.String())
	}
}

func TestHTTPProbeClassifiesStatus(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<rss/>")
	}))
	defer ok.Close()
	if _, err := httpProbe(context.Background(), ok.URL); err != nil {
		t.Fatalf("200 should be reachable: %v", err)
	}

	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer missing.Close()
	if _, err := httpProbe(context.Background(), missing.URL); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("404 should be a probe error naming the status, got %v", err)
	}
}

func TestRunBrokenSourcesTomlFailsDoctor(t *testing.T) {
	writeSources(t, "enabled = [\"rss\"\n") // malformed TOML
	var buf bytes.Buffer
	if ok := run(&buf, core.Defaults(), "test", nil); ok {
		t.Fatalf("broken sources.toml should fail doctor:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "sources.toml") {
		t.Errorf("report should name sources.toml:\n%s", buf.String())
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
