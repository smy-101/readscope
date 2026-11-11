// Package hn implements the Hacker News source on hn.algolia.com's
// public search index: four fixed feeds (Front / New / Ask HN /
// Show HN), each one HTTP GET away, snapshotted into the store like
// any other source. It registers itself under the id "hn" from init —
// the same self-registration contract as rss, which is what keeps
// "adding a source = adding a package" true.
package hn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
)

// ID is the source id in sources.toml's enabled list.
const ID = "hn"

// ProdBase is the Algolia HN Search API root. READSCOPE_HN_BASE
// overrides it — tests point it at a fake server, and debugging a
// proxy setup can too.
const ProdBase = "https://hn.algolia.com/api/v1"

// maxBodyBytes caps one response at 10 MiB, the same budget rss and
// `render` allow.
const maxBodyBytes = 10 << 20

// fetchTimeout bounds one feed's HTTP round trip; parsing is local.
const fetchTimeout = 30 * time.Second

// feedDef is one of HN's four fixed feeds: the Algolia endpoint
// (search ranks by popularity, search_by_date by recency) plus the
// tags filter selecting the story set, and the list ordering the feed
// declares. Slice order is sidebar order.
type feedDef struct {
	key      string
	labelKey string // i18n key; the UI resolves the display label
	endpoint string
	tags     string
	sort     core.ListSort
}

// feedDefs mirrors hn.algolia.com's own tabs. New keeps a story tags
// filter so comments never leak into the story list; ask_hn/show_hn
// ride the popularity endpoint like the site's category pages. The
// three popularity feeds declare ListSortPoints — their Algolia return
// order is points order — while New stays chronological.
var feedDefs = []feedDef{
	{"front", "hn.front", "search", "front_page", core.ListSortPoints},
	{"new", "hn.new", "search_by_date", "story", core.ListSortTime},
	{"ask", "hn.ask", "search", "ask_hn", core.ListSortPoints},
	{"show", "hn.show", "search", "show_hn", core.ListSortPoints},
}

func init() { source.Register(ID, New) }

// Source is the Hacker News implementation of source.Source.
type Source struct {
	base   string // API root; ProdBase in production
	client *http.Client
}

// New builds the HN source. HN has no per-user subscriptions — the
// four feeds are fixed — so the subscription config goes unused.
func New(*core.Sources) (source.Source, error) {
	base := ProdBase
	if v := os.Getenv("READSCOPE_HN_BASE"); v != "" {
		base = v
	}
	return newSource(base), nil
}

func newSource(base string) *Source {
	return &Source{base: base, client: &http.Client{Timeout: fetchTimeout}}
}

// ID implements source.Source.
func (s *Source) ID() string { return ID }

// Feeds implements source.Source: the four fixed feeds in sidebar
// order, each carrying its fetch URL (labels resolve through i18n).
func (s *Source) Feeds() []core.Feed {
	out := make([]core.Feed, len(feedDefs))
	for i, d := range feedDefs {
		out[i] = core.Feed{Key: d.key, LabelKey: d.labelKey, URL: s.url(d), ListSort: d.sort}
	}
	return out
}

func (s *Source) url(d feedDef) string { return s.base + "/" + d.endpoint + "?tags=" + d.tags }

// Fetch implements source.Source: one page of the feed from the
// Algolia index, mapped to snapshot items. Algolia has no validator
// semantics, so HN always fetches unconditionally — no
// ConditionalSource extension, by design.
func (s *Source) Fetch(ctx context.Context, f core.Feed) ([]core.Item, error) {
	var def *feedDef
	for i := range feedDefs {
		if feedDefs[i].key == f.Key {
			def = &feedDefs[i]
			break
		}
	}
	if def == nil {
		return nil, fmt.Errorf("fetch %s: unknown hn feed", f.Key)
	}
	raw, err := s.get(ctx, s.url(*def), "fetch "+f.Key)
	if err != nil {
		return nil, err
	}
	var res searchResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("parse %s: not Algolia JSON: %w", f.Key, err)
	}
	now := time.Now().UTC()
	items := make([]core.Item, 0, len(res.Hits))
	for _, h := range res.Hits {
		if h.ObjectID == "" {
			continue // no id → no deterministic row; skip, never crash
		}
		items = append(items, h.item(def.key, now))
	}
	return items, nil
}

// searchResponse is the slice of Algolia's search payload readscope
// consumes: just the hits (paging fields grow in with Ctrl+N, M3
// ticket 05).
type searchResponse struct {
	Hits []hit `json:"hits"`
}

// hit is one Algolia story record. Every field but objectID is
// optional in the wild: text posts carry story_text instead of url,
// and degenerate records miss author or dates entirely.
type hit struct {
	ObjectID    string `json:"objectID"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Author      string `json:"author"`
	CreatedAtI  int64  `json:"created_at_i"`
	StoryText   string `json:"story_text"`
	Points      *int   `json:"points"`       // absent → nil, distinct from a real 0
	NumComments *int   `json:"num_comments"` // absent → nil
}

// item maps one hit to its snapshot row. The body holds the one-line
// article link for link posts and the story's own HTML for text
// posts, which then flows through the M1 render pipeline like any
// RSS body.
func (h hit) item(feedKey string, now time.Time) core.Item {
	it := core.Item{
		ID:        ItemID(h.ObjectID),
		Source:    ID,
		Feed:      feedKey,
		Title:     h.Title,
		URL:       h.URL,
		Author:    h.Author,
		FetchedAt: now,
	}
	if h.CreatedAtI != 0 {
		t := time.Unix(h.CreatedAtI, 0).UTC()
		it.PublishedAt = &t
	}
	if h.URL != "" {
		it.Body = h.URL
	} else {
		it.Body = h.StoryText
	}
	it.Points, it.NumComments = h.Points, h.NumComments
	return it
}

// ItemID builds the deterministic row id "hn:algolia:<storyID>" —
// global per story, not per feed (the shape the initial schema
// comment fixed; a story roaming between Front and New re-homes its
// single row).
func ItemID(storyID string) string { return itemIDPrefix + storyID }

// itemIDPrefix is the single source of the id shape: ItemID builds
// ids with it, Detail reverses it to recover the story id.
const itemIDPrefix = ID + ":algolia:"

// get performs the shared HTTP skeleton of every Algolia call — one
// GET carrying the reader's identity, a hard body cap, and errors
// that name the operation (op, e.g. "fetch front" / "detail 1001") —
// and returns the raw JSON body. Parsing stays with the caller so
// the fetch-vs-parse phase distinction in error messages survives.
func (s *Source) get(ctx context.Context, url, op string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	req.Header.Set("User-Agent", source.UserAgent)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %s", op, resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return raw, nil
}
