// Package rss implements the RSS/Atom source: a plain HTTP fetch with a
// readscope User-Agent and size cap, parsed by gofeed. It registers
// itself under the id "rss" — sibling sources (hn in M3) follow the
// same pattern, which is what keeps "adding a source = adding a
// package" true.
package rss

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
)

// ID is the source id in sources.toml.
const ID = "rss"

// userAgent identifies readscope to feed hosts. The version suffix is
// not wired in yet (cmd owns Version); worth revisiting if hosts care.
const userAgent = "readscope (+https://github.com/smy-101/readscope)"

// maxFeedBytes caps a feed body at 10 MiB, same budget `render` allows.
const maxFeedBytes = 10 << 20

// fetchTimeout bounds one feed's HTTP round trip; parsing is local.
const fetchTimeout = 30 * time.Second

func init() { source.Register(ID, New) }

// Source is the RSS/Atom implementation of source.Source.
type Source struct {
	feeds  []core.Feed // sorted by key: stable sidebar + refresh output
	client *http.Client
}

// New builds the RSS source from the user's subscriptions. Feed keys
// must carry a URL; the label falls back to the key.
func New(srcs *core.Sources) (source.Source, error) {
	feeds := make([]core.Feed, 0, len(srcs.RSS.Feeds))
	for key, f := range srcs.RSS.Feeds {
		if strings.TrimSpace(f.URL) == "" {
			return nil, fmt.Errorf("rss feed %q: url is required", key)
		}
		label := f.Label
		if label == "" {
			label = key
		}
		feeds = append(feeds, core.Feed{Key: key, Label: label, URL: f.URL})
	}
	sort.Slice(feeds, func(i, j int) bool { return feeds[i].Key < feeds[j].Key })
	return &Source{feeds: feeds, client: &http.Client{Timeout: fetchTimeout}}, nil
}

// ID implements source.Source.
func (s *Source) ID() string { return ID }

// Feeds implements source.Source.
func (s *Source) Feeds() []core.Feed {
	out := make([]core.Feed, len(s.feeds))
	copy(out, s.feeds)
	return out
}

// Fetch implements source.Source: unconditional fetch. Only the
// non-conditional refresh fallback (sources without the extension,
// HN in M3) and tests call it today; everyone persistent goes through
// FetchConditional.
func (s *Source) Fetch(ctx context.Context, f core.Feed) ([]core.Item, error) {
	items, _, _, err := s.FetchConditional(ctx, f, core.FetchState{})
	return items, err
}

// FetchConditional implements source.ConditionalSource: fetch the feed,
// replaying prev's validators as If-None-Match / If-Modified-Since. A
// 304 answer returns changed=false and no items — nothing to parse or
// persist beyond the state itself, whose validators carry over (a 304
// body is empty; if the server sends fresh ones we take them). A 200
// maps items as usual and returns the response's validators — empty
// when the server sent none, which makes the next request
// unconditional: a plain refresh, by design.
func (s *Source) FetchConditional(ctx context.Context, f core.Feed, prev core.FetchState) ([]core.Item, core.FetchState, bool, error) {
	resp, err := s.request(ctx, f.URL, prev)
	if err != nil {
		return nil, prev, false, fmt.Errorf("fetch %s: %w", f.Key, err)
	}
	defer resp.Body.Close()

	next := core.FetchState{
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		LastFetched:  time.Now().UTC(),
	}
	switch {
	case resp.StatusCode == http.StatusNotModified && (prev.ETag != "" || prev.LastModified != ""):
		// A 304 is only meaningful for a request we made conditional. An
		// unconditional request answered 304 is a broken server (RFC 9110
		// §15.4.5): falling through to the error keeps it from silently
		// reading as "snapshot current" for a feed we never fetched.
		if next.ETag == "" {
			next.ETag = prev.ETag
		}
		if next.LastModified == "" {
			next.LastModified = prev.LastModified
		}
		return nil, next, false, nil
	case resp.StatusCode == http.StatusOK:
	default:
		return nil, prev, false, fmt.Errorf("fetch %s: HTTP %s", f.Key, resp.Status)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes))
	if err != nil {
		return nil, prev, false, fmt.Errorf("fetch %s: %w", f.Key, err)
	}
	parsed, err := gofeed.NewParser().ParseString(string(raw))
	if err != nil {
		return nil, prev, false, fmt.Errorf("parse %s: not a valid RSS/Atom feed: %w", f.Key, err)
	}
	now := next.LastFetched
	items := make([]core.Item, 0, len(parsed.Items))
	for _, it := range parsed.Items {
		items = append(items, core.Item{
			ID:          ItemID(f.Key, it),
			Source:      ID,
			Feed:        f.Key,
			Title:       it.Title,
			URL:         it.Link,
			Author:      authorOf(it),
			PublishedAt: publishedOf(it),
			FetchedAt:   now,
			Body:        bodyOf(it),
		})
	}
	return items, next, true, nil
}

// ItemID builds the deterministic row id "rss:<feedkey>:<guid>". Feeds
// that omit GUID fall back to the link, then to a content hash —
// stable across refreshes either way.
func ItemID(feedKey string, it *gofeed.Item) string {
	key := it.GUID
	if key == "" {
		key = it.Link
	}
	if key == "" {
		sum := sha1.Sum([]byte(it.Title + "\x00" + it.Published))
		key = fmt.Sprintf("sha1:%x", sum)
	}
	return ID + ":" + feedKey + ":" + key
}

// bodyOf prefers the full content and falls back to the summary, so the
// offline snapshot is the richest text the feed offers.
func bodyOf(it *gofeed.Item) string {
	if it.Content != "" {
		return it.Content
	}
	return it.Description
}

func authorOf(it *gofeed.Item) string {
	if it.Author != nil {
		return it.Author.Name
	}
	return ""
}

func publishedOf(it *gofeed.Item) *time.Time {
	if it.PublishedParsed != nil {
		return it.PublishedParsed
	}
	return it.UpdatedParsed // Atom feeds often carry only <updated>
}

// request does the HTTP leg ourselves (not gofeed's ParseURL) so the
// User-Agent, timeout, conditional headers and error wrapping are
// ours. prev's validators become If-None-Match / If-Modified-Since;
// empty ones are simply omitted (unconditional request).
func (s *Source) request(ctx context.Context, url string, prev core.FetchState) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if prev.ETag != "" {
		req.Header.Set("If-None-Match", prev.ETag)
	}
	if prev.LastModified != "" {
		req.Header.Set("If-Modified-Since", prev.LastModified)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	// Body is drained+closed by FetchConditional; status handling is
	// its switch, so every code path there sees the same response.
	return resp, nil
}
