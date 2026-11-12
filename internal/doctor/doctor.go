// Package doctor is readscope's environment self-check: the same checks
// FeedScope's doctor runs (config/state locations, database, language
// packs, themes, terminal), phrased so a broken environment is diagnosable
// from one command.
package doctor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"golang.org/x/term"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/i18n"
	"github.com/smy-101/readscope/internal/refresh"
	"github.com/smy-101/readscope/internal/source/rss"
	"github.com/smy-101/readscope/internal/source/zhihu"
	"github.com/smy-101/readscope/internal/store"
	"github.com/smy-101/readscope/internal/theme"
)

type check struct {
	label string
	err   error
	note  string // success detail, e.g. the cookie census / anonymous note
}

// Probe measures one feed's reachability: it fetches url and reports
// how long that took. Doctor classifies from those two facts alone,
// so tests inject probes and never touch the network.
type Probe func(ctx context.Context, url string) (time.Duration, error)

// slowFeedAfter is the elapsed time past which a reachable feed is
// reported slow (warning, not failure).
const slowFeedAfter = 3 * time.Second

// Run executes all checks, prints a report to w, and reports whether
// everything passed (the CLI turns false into exit code 1). A failed
// feed probe — or a sources.toml that won't parse — fails the whole
// run; slow feeds warn but pass.
func Run(w io.Writer, cfg *core.Config, version string) bool {
	return run(w, cfg, version, httpProbe)
}

func run(w io.Writer, cfg *core.Config, version string, probe Probe) bool {
	cat := loadCatalog(cfg)
	th, _ := theme.Load(cfg.Theme)

	fmt.Fprintln(w, th.Accent.Render(cat.Get("doctor.title")))
	fmt.Fprintln(w, th.Dim.Render(fmt.Sprintf("%s: %s", cat.Get("doctor.version"), version)))
	fmt.Fprintln(w)

	checks := []check{
		named(cat, "doctor.config_dir", func() error { _, err := core.Path(); return err }),
		named(cat, "doctor.state_dir", func() error { _, err := store.DefaultPath(); return err }),
		named(cat, "doctor.db_open", func() error { return checkDB("db_open") }),
		named(cat, "doctor.db_migrate", func() error { return checkDB("db_migrate") }),
		named(cat, "doctor.i18n_load", checkI18n),
		named(cat, "doctor.theme_load", checkThemes),
		zhihuCookieCheck(cat),
	}
	okCount := 0
	for _, c := range checks {
		if c.err != nil {
			fmt.Fprintf(w, "%s %s: %v\n", th.Error.Render("✗"), c.label, c.err)
			continue
		}
		okCount++
		if c.note != "" {
			fmt.Fprintf(w, "%s %s: %s\n", th.Success.Render("✓"), c.label, c.note)
		} else {
			fmt.Fprintf(w, "%s %s\n", th.Success.Render("✓"), c.label)
		}
	}

	// Terminal size: informational, never a failure (piped output is fine).
	fmt.Fprintln(w)
	if fd := int(os.Stdout.Fd()); term.IsTerminal(fd) {
		width, height, _ := term.GetSize(fd)
		fmt.Fprintf(w, "%s %s: %d×%d\n", th.Success.Render("✓"), cat.Get("doctor.terminal"), width, height)
	} else {
		fmt.Fprintf(w, "%s %s\n", th.Dim.Render("-"), cat.Get("doctor.terminal"))
	}
	fmt.Fprintln(w, th.Dim.Render(cat.Get("doctor.hint_env")))

	fmt.Fprintln(w)
	fmt.Fprintln(w, th.StatusAccent.Render(cat.Get("doctor.summary", len(checks), okCount, len(checks)-okCount)))

	staticOK := okCount == len(checks)
	fmt.Fprintln(w)
	srcs, err := core.LoadSources()
	if err != nil {
		fmt.Fprintln(w, th.Accent.Render(cat.Get("doctor.feeds")))
		fmt.Fprintf(w, "%s %v\n", th.Error.Render("✗"), err)
		return false
	}
	return staticOK && CheckFeeds(w, cfg, srcs, probe)
}

// httpProbe is the production probe behind doctor: one plain GET
// (same User-Agent the refresher sends) with the caller's deadline.
// Any HTTP answer that isn't an error status means reachable; the
// elapsed time lets doctor flag slow feeds.
func httpProbe(ctx context.Context, url string) (time.Duration, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", rss.UserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return time.Since(start), fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return time.Since(start), nil
}

// CheckFeeds probes every configured RSS subscription in feed-key
// order and reports reachability: ✓ ok (with elapsed time), ! slow
// (warning — reachable but past slowFeedAfter), ✗ failed (probe error
// or HTTP ≥ 400). Slow never fails; a failed feed does. With no feeds
// configured it prints the no-feeds hint and passes.
func CheckFeeds(w io.Writer, cfg *core.Config, srcs *core.Sources, probe Probe) bool {
	cat := loadCatalog(cfg)
	th, _ := theme.Load(cfg.Theme)

	feeds := srcs.RSS.Feeds
	if len(feeds) == 0 {
		fmt.Fprintln(w, th.Dim.Render(refresh.NoFeedsHint(cat)))
		return true
	}

	keys := make([]string, 0, len(feeds))
	for k := range feeds {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fmt.Fprintln(w)
	fmt.Fprintln(w, th.Accent.Render(cat.Get("doctor.feeds")))

	ok, slow, failed := 0, 0, 0
	for _, k := range keys {
		f := feeds[k]
		label := f.Label
		if label == "" {
			label = k
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		elapsed, err := probe(ctx, f.URL)
		cancel()
		switch {
		case err != nil:
			failed++
			fmt.Fprintf(w, "%s %s\n", th.Error.Render("✗"), cat.Get("doctor.feed_fail", label, err))
		case elapsed > slowFeedAfter:
			slow++
			fmt.Fprintf(w, "%s %s\n", th.Warning.Render("!"), cat.Get("doctor.feed_slow", label, fmtDur(elapsed)))
		default:
			ok++
			fmt.Fprintf(w, "%s %s\n", th.Success.Render("✓"), cat.Get("doctor.feed_ok", label, fmtDur(elapsed)))
		}
	}
	fmt.Fprintln(w, th.StatusAccent.Render(cat.Get("doctor.feeds_summary", len(feeds), ok, slow, failed)))
	return failed == 0
}

// fmtDur rounds an elapsed time for display: whole milliseconds under
// a second, centiseconds above ("120ms", "3.5s").
func fmtDur(d time.Duration) string {
	if d >= time.Second {
		return d.Round(10 * time.Millisecond).String()
	}
	return d.Round(time.Millisecond).String()
}

func loadCatalog(cfg *core.Config) *i18n.Catalog {
	cat, err := i18n.Load(cfg.Language)
	if err != nil {
		// Language packs are embedded; failing here means the binary is
		// broken. Fall back to English rather than crashing doctor.
		cat, _ = i18n.Load(i18n.Default)
	}
	return cat
}

func named(cat *i18n.Catalog, key string, fn func() error) check {
	return check{label: cat.Get(key), err: fn()}
}

func checkDB(mode string) error {
	path, err := store.DefaultPath()
	if err != nil {
		return err
	}
	s, err := store.Open(path)
	if err != nil {
		return err
	}
	defer s.Close()
	if mode == "db_migrate" {
		return s.Migrate()
	}
	return s.Ping()
}

func checkI18n() error {
	infos, err := i18n.Available()
	if err != nil {
		return err
	}
	for _, info := range infos {
		if _, err := i18n.Load(info.Code); err != nil {
			return err
		}
	}
	return nil
}

func checkThemes() error {
	for _, name := range theme.Available() {
		if _, err := theme.Load(name); err != nil {
			return err
		}
	}
	return nil
}

// zhihuCookieCheck reports the zhihu cookie file's state. 未配置 is
// the anonymous mode — a PASS with a note (hot/daily work without a
// session); a loaded session reports its census; a file that exists
// but can't serve (looser perms, no z_c0, unreadable) fails the run —
// someone tried to log in and it's broken. Values never print.
func zhihuCookieCheck(cat *i18n.Catalog) check {
	label := cat.Get("doctor.cookie")
	path, err := zhihu.CookiePath()
	if err != nil {
		return check{label: label, err: err}
	}
	fi, err := os.Stat(path)
	if os.IsNotExist(err) {
		return check{label: label, note: cat.Get("doctor.cookie_none")}
	}
	if err != nil {
		return check{label: label, err: err}
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		return check{label: label, err: fmt.Errorf("%s", cat.Get("doctor.cookie_perms", fmt.Sprintf("%#o", perm)))}
	}
	_, info, err := zhihu.LoadCookie(path)
	if err != nil {
		return check{label: label, err: err}
	}
	if !info.HasZC0 {
		return check{label: label, err: fmt.Errorf("%s", cat.Get("doctor.cookie_no_zc0"))}
	}
	return check{label: label, note: cat.Get("doctor.cookie_ok", info.KeyCount)}
}
