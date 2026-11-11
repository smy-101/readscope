package opml

import (
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/i18n"
)

// Added is one feed the import merged into sources.toml: the key it
// landed under (new), and its label/URL.
type Added struct {
	Key   string
	Label string
	URL   string
}

// Outcome is the import verdict, data only — the CLI localizes it via
// Report. Skipped names the feeds whose URL was already subscribed
// (in the config or earlier in the same document); NoTitle names the
// URLs that carried no title and were named after their host.
type Outcome struct {
	Added   []Added
	Skipped []string
	NoTitle []string
}

// Import merges document-ordered OPML feeds into the subscription
// config, mutating srcs: every URL not already present (config or
// earlier in doc) is added under a deterministic key, and "rss" joins
// the enabled list when absent — importing subscriptions implies
// wanting the source visible. Existing entries are never touched.
func Import(srcs *core.Sources, doc []Feed) Outcome {
	oc := Outcome{}
	if srcs.RSS.Feeds == nil {
		srcs.RSS.Feeds = map[string]core.RSSFeed{}
	}
	subscribed := map[string]bool{}
	for _, f := range srcs.RSS.Feeds {
		subscribed[f.URL] = true
	}
	enableRSS(srcs)
	for _, f := range doc {
		if subscribed[f.URL] {
			skip := f.Title
			if skip == "" {
				skip = f.URL // titleless dups still deserve a name in the report
			}
			oc.Skipped = append(oc.Skipped, skip)
			continue
		}
		label := strings.TrimSpace(f.Title)
		if label == "" {
			label = host(f.URL)
			if label == "" {
				label = f.URL // even a junk URL deserves a visible name
			}
			oc.NoTitle = append(oc.NoTitle, f.URL)
		}
		key := uniqueKey(srcs.RSS.Feeds, f.Title, f.URL)
		srcs.RSS.Feeds[key] = core.RSSFeed{Label: label, URL: f.URL}
		subscribed[f.URL] = true
		oc.Added = append(oc.Added, Added{Key: key, Label: label, URL: f.URL})
	}
	return oc
}

// enableRSS appends the rss source id when missing, preserving the
// user's order otherwise.
func enableRSS(srcs *core.Sources) {
	for _, id := range srcs.Enabled {
		if id == "rss" {
			return
		}
	}
	srcs.Enabled = append(srcs.Enabled, "rss")
}

// Report writes the localized import verdict to out: one line per
// added feed (label + the key it landed under), one per skipped
// duplicate, a warning per titleless feed, then a summary line.
// Importing a dirty-but-parseable document is a success — warnings,
// not failures.
func Report(out io.Writer, oc Outcome, cat *i18n.Catalog) {
	for _, a := range oc.Added {
		fmt.Fprintln(out, cat.Get("rss.import_added", a.Label, a.Key))
	}
	for _, s := range oc.Skipped {
		fmt.Fprintln(out, cat.Get("rss.import_skip", s))
	}
	for _, u := range oc.NoTitle {
		fmt.Fprintln(out, cat.Get("rss.import_no_title", u))
	}
	fmt.Fprintln(out, cat.Get("rss.import_summary", len(oc.Added), len(oc.Skipped)))
}

// uniqueKey derives a feed key nobody has taken yet: slug(title) for
// ASCII titles ("Hacker News" → hacker-news), the URL host otherwise
// (CJK titles keep their title as the label, the host as the stable
// key); collisions get -2, -3… suffixes.
func uniqueKey(taken map[string]core.RSSFeed, title, feedURL string) string {
	base := slug(title)
	if base == "" {
		base = host(feedURL)
	}
	if base == "" {
		base = "feed"
	}
	if _, exists := taken[base]; !exists {
		return base
	}
	for n := 2; ; n++ {
		candidate := base + "-" + strconv.Itoa(n)
		if _, exists := taken[candidate]; !exists {
			return candidate
		}
	}
}

// slug lowercases an ASCII title into dash-joined runs of [a-z0-9];
// titles with no ASCII letters (CJK, punctuation-only) yield "".
func slug(title string) string {
	var b strings.Builder
	dash := false // pending separator, emitted before the next run
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case unicode.IsLetter(r) && r < 128, unicode.IsDigit(r) && r < 128:
			b.WriteRune(r)
			dash = true
		case dash:
			b.WriteByte('-')
			dash = false
		}
	}
	return strings.Trim(b.String(), "-")
}

// host returns the feed URL's host (port stripped); a URL that does
// not parse yields "".
func host(feedURL string) string {
	u, err := url.Parse(feedURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.Split(u.Host, ":")[0]
}
