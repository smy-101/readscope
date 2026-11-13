package heybox

// circle.go — the circle feeds (圈流, ticket 02): every subscribed
// circle from sources.toml's [heybox] table is one feed, exactly the
// RSS-subscription shape one level up. The list's declaration order
// is the sidebar order (the hand-edit promise: add, delete, reorder
// in TOML, restart, done); the CLI (`readscope heybox add`) is the
// no-hunt-for-topic_id way in, search.go. One card arriving in both
// the recommend feed and a circle re-homes between them — the same
// single-row-per-id roaming HN's Front/New already practice; paging
// depth and read state key off (source, feed), so circles never
// share depth with each other or the recommend feed.

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/smy-101/readscope/internal/core"
)

// topicPath is the circle feed's endpoint (P15/08 of the smoke
// matrix): topic_id + the offset family, signed variant A like every
// other list leg.
const topicPath = "/bbs/app/topic/feeds"

// CircleFeedPrefix is the circle feed keys' spine: "circle/<topic_id>".
// The topic_id is the circle's identity on the host — stable across
// renames, deduping subscriptions, and readable straight out of a
// sidebar key when something needs naming.
const CircleFeedPrefix = "circle/"

// CircleFeedKey builds one circle's feed key from its topic_id.
func CircleFeedKey(topicID int64) string {
	return CircleFeedPrefix + strconv.FormatInt(topicID, 10)
}

// ValidateCircles checks the config's circle table and returns the
// circles ready to load — the shared gate for source construction
// (F5, Ctrl+N, the add CLI) and doctor's census: a hand edit that
// breaks the table fails loudly here, before any dial (the rss
// url-is-required call, same discipline). A circle needs a name
// (the sidebar label) and a positive topic_id (the feed's identity),
// and two entries naming one topic would collapse onto one feed
// key's paging depth, so the duplicate refuses instead. No
// [heybox] table is a valid state: zero circles, no error.
func ValidateCircles(srcs *core.Sources) ([]core.HeyboxCircle, error) {
	circles := srcs.HeyboxCircles()
	named := map[int64]string{}
	for i, c := range circles {
		if strings.TrimSpace(c.Name) == "" {
			return nil, fmt.Errorf("heybox circle #%d: name is required", i+1)
		}
		if c.TopicID <= 0 {
			return nil, fmt.Errorf("heybox circle %q: topic_id must be positive, got %d", c.Name, c.TopicID)
		}
		if prev, dup := named[c.TopicID]; dup {
			return nil, fmt.Errorf("heybox circle %q: topic_id %d already subscribed as %q", c.Name, c.TopicID, prev)
		}
		named[c.TopicID] = c.Name
	}
	return circles, nil
}

// loadCircles grows the feed list with one feed per configured
// circle, in declaration order, after ValidateCircles' gate.
func (s *Source) loadCircles(srcs *core.Sources) error {
	circles, err := ValidateCircles(srcs)
	if err != nil {
		return err
	}
	for _, c := range circles {
		key := CircleFeedKey(c.TopicID)
		s.feeds = append(s.feeds, core.Feed{
			Key:   key,
			Label: c.Name,
			URL:   s.tr.base + topicPath,
			// ListSortTime (the zero value): a circle is chronological
			// by the card clock, like the recommend feed.
		})
		s.topics[key] = c.TopicID
	}
	return nil
}

// fetchCirclePage pulls one circle's page — the smoke's measured
// request verbatim (S8/P15): topic_id, the offset family, and the
// empty lastval (the endpoint takes an offset here; the cursor it
// serves back stays unused). limit 10 is the measured ask; the host
// serves its own ~20-card page either way.
func (s *Source) fetchCirclePage(ctx context.Context, f core.Feed, page int) ([]core.Item, error) {
	return s.fetchCards(ctx, f, "topic-feed", topicPath, url.Values{
		"topic_id": {strconv.FormatInt(s.topics[f.Key], 10)},
		"offset":   {strconv.Itoa(page)},
		"limit":    {"10"},
		"lastval":  {""},
		"dw":       {"720"},
	})
}
