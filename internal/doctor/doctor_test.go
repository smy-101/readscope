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
	"github.com/smy-101/readscope/internal/epub/fixture"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/source/zhihu"
	"github.com/smy-101/readscope/internal/store"
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
		"heybox source", "EPUB library",
	} {
		if !strings.Contains(out, label) {
			t.Errorf("report missing check %q:\n%s", label, out)
		}
	}
	if !strings.Contains(out, "9 checks: 9 ok, 0 failed") {
		t.Errorf("summary line wrong:\n%s", out)
	}
	// No cookie file is the anonymous mode: a pass with a note, not a
	// failure — hot/daily work without a session.
	if !strings.Contains(out, "not configured") {
		t.Errorf("anonymous cookie state should carry its note:\n%s", out)
	}
	// A fresh install carries no circles and no cache: the heybox
	// line's honest note, a pass never a failure. The library line
	// says the same thing its own way: no books yet, still a pass.
	if !strings.Contains(out, "0 circles · never fetched") {
		t.Errorf("fresh heybox state should carry its note:\n%s", out)
	}
	if !strings.Contains(out, "no books yet") {
		t.Errorf("fresh library state should carry its note:\n%s", out)
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

// TestHeyboxLineReportsCirclesAndLastFetch: a healthy subscription
// with cached content renders the census note — circle count, cached
// item count, and the age of the last fetch that moved content
// (doctor never dials the host; the cache is the witness). Both
// language packs are asserted as COMPOSITIONS: the note splices the
// time.* relative-age strings in, and a pattern like zh's
// "%d 分钟前" colliding with a trailing 前 in the pattern itself is
// exactly the garble this test exists to catch — key-presence parity
// alone never would.
func TestHeyboxLineReportsCirclesAndLastFetch(t *testing.T) {
	for _, c := range []struct{ lang, want string }{
		{"en", "1 circles · 3 cached items · fetched 5m"},
		{"zh-CN", "1 圈 · 3 条快照 · 最近拉取 5 分钟前"},
	} {
		t.Run(c.lang, func(t *testing.T) {
			writeSources(t, "enabled = [\"heybox\"]\n\n[[heybox.circles]]\nname = \"黑神话：悟空\"\ntopic_id = 568547\n")
			path, err := store.DefaultPath()
			if err != nil {
				t.Fatal(err)
			}
			st, err := store.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			if err := st.Migrate(); err != nil {
				t.Fatal(err)
			}
			fetched := time.Now().UTC().Add(-5 * time.Minute)
			items := []core.Item{
				{ID: "hb:link/aa", Source: "heybox", Feed: "circle/568547", Title: "帖一", URL: "https://x", FetchedAt: fetched},
				{ID: "hb:link/bb", Source: "heybox", Feed: "circle/568547", Title: "帖二", URL: "https://x", FetchedAt: fetched},
				{ID: "hb:link/cc", Source: "heybox", Feed: "recommend", Title: "帖三", URL: "https://x", FetchedAt: fetched},
			}
			if _, _, err := st.SaveFeedItems(context.Background(), "heybox", "circle/568547", items[:2]); err != nil {
				t.Fatal(err)
			}
			if _, _, err := st.SaveFeedItems(context.Background(), "heybox", "recommend", items[2:]); err != nil {
				t.Fatal(err)
			}

			cfg := core.Defaults()
			cfg.Language = c.lang
			var buf bytes.Buffer
			if ok := run(&buf, cfg, "test", nil); !ok {
				t.Fatalf("a healthy heybox state must pass doctor:\n%s", buf.String())
			}
			if !strings.Contains(buf.String(), c.want) {
				t.Errorf("heybox note wrong:\n%s", buf.String())
			}
			if strings.Contains(buf.String(), "前 前") {
				t.Errorf("double suffix garble in the note:\n%s", buf.String())
			}
		})
	}
}

// TestHeyboxBrokenCircleTableFails: the one thing a hand edit can
// break — a circle row without a usable topic_id — fails the check
// naming the entry instead of passing silently.
func TestHeyboxBrokenCircleTableFails(t *testing.T) {
	writeSources(t, "enabled = [\"heybox\"]\n\n[[heybox.circles]]\nname = \"坏圈\"\ntopic_id = 0\n")
	var buf bytes.Buffer
	if ok := run(&buf, core.Defaults(), "test", nil); ok {
		t.Fatalf("a broken circle table must fail doctor:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "topic_id must be positive") {
		t.Errorf("failure should name the broken entry:\n%s", buf.String())
	}
}

// --- M6 ticket 02: the library check ---------------------------------

// doctorBookEnv points a library-check run at a temp state dir and a
// temp library directory (English output).
func doctorBookEnv(t *testing.T) (*core.Config, string) {
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

// TestLibraryCheckEmpty: doctor rides the same library.Scan seam the
// bookshelf does; an empty (or missing) library directory is the
// fresh-install note, never a failure.
func TestLibraryCheckEmpty(t *testing.T) {
	cfg, _ := doctorBookEnv(t)
	cat, _ := i18n.Load("en")
	c := libraryCheck(cat, cfg)
	if c.err != nil {
		t.Fatalf("empty library must pass: %v", c.err)
	}
	if !strings.Contains(c.note, "no books yet") {
		t.Fatalf("note = %q, want the fresh-install note", c.note)
	}
}

// TestLibraryCheckCensus: the note counts shelved books, hard
// failures and warned books from the scan it just ran.
func TestLibraryCheckCensus(t *testing.T) {
	cfg, books := doctorBookEnv(t)
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
