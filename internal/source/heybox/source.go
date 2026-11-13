package heybox

// source.go — the Source registration and the list feeds: the
// anonymous recommendation feed (ticket 01) and, one per
// subscription, the circle feeds (ticket 02, circle.go) — both
// offset-paged feeds of post cards (GLOSSARY: 帖子卡). Every card
// carries the hex link_id in its share_url — the detail layer's key
// (ticket 03) and this row's identity; ad cards have no linkid and
// drop here. The heat snapshot folds the award count and comment
// count (the web endpoints serve `up` as a constant 0 — the smoke's
// fixtures pin `link_award_num` as the live signal). The two-layer
// detail (ticket 03) lands on this same skeleton.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
)

// ID is the source id in sources.toml's enabled list.
const ID = "heybox"

// RecommendFeedKey is the recommendation feed's stable key (GLOSSARY:
// 推荐流) — anonymous, offset-paged.
const RecommendFeedKey = "recommend"

// recommendLabelKey is the feed's i18n key; the UI resolves the label.
const recommendLabelKey = "heybox.recommend"

// recommendPath is the recommendation feed's endpoint (P14 of the
// smoke matrix). pull=0 and dw=800 ride verbatim from the measured
// request — dw is the web client's layout width, pull its feed-mode
// flag; both survived every green leg and stay until a real-network
// smoke says otherwise.
const recommendPath = "/bbs/app/feeds"

func init() { source.Register(ID, New) }

// Source is the heybox implementation of source.Source and its
// offset-paged extension (PagedSource — the recommend feed walks
// /bbs/app/feeds by offset, the circle feeds of ticket 02 walk
// /bbs/app/topic/feeds the same way). The detail extension arrives
// with ticket 03; callers probe with type assertions, so its absence
// is a non-feature.
type Source struct {
	tr *Transport
	// feeds is the sidebar's truth: the recommend feed first, then
	// one feed per subscribed circle in sources.toml's declaration
	// order (the TOML order IS the sidebar order — the hand-edit
	// promise). Built once at construction; Feeds hands out copies.
	feeds []core.Feed
	// topics indexes each circle feed key to its topic_id — the
	// FetchPage dispatch's authority (a key not here and not the
	// recommend feed is not ours).
	topics map[string]int64
}

// The optional extensions heybox carries, pinned at compile time.
var _ source.PagedSource = (*Source)(nil)

// New builds the heybox source. The subscription config carries the
// circle list ([heybox] table, ticket 02): every circle validates
// here — a hand edit that breaks it fails loudly before any dial.
// READSCOPE_HEYBOX_BASE overrides the API root — the same override
// pattern zhihu honors, so tests point one fake server at the
// transport and a proxy debug run can too. The device fingerprint
// loads-or-mints here: a broken device file is a loud error, never a
// silent re-mint (every refresh would otherwise greet the host as a
// brand-new device).
func New(srcs *core.Sources) (source.Source, error) {
	base := ProdBase
	if v := os.Getenv("READSCOPE_HEYBOX_BASE"); v != "" {
		base = v
	}
	// READSCOPE_HEYBOX_MINGAP (milliseconds) rescales the politeness
	// delay — tests run flat out, a real-net smoke can slow down. The
	// production default is the smoke's 1.5s floor.
	minGap := defaultMinGap
	if v := os.Getenv("READSCOPE_HEYBOX_MINGAP"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms >= 0 {
			minGap = time.Duration(ms) * time.Millisecond
		}
	}
	var device string
	if v := os.Getenv("READSCOPE_HEYBOX_DEVICE"); v != "" {
		device = v // probe/debug override; bypasses the file
	} else {
		path, err := DevicePath()
		if err != nil {
			return nil, fmt.Errorf("heybox: %w", err)
		}
		device, err = LoadOrCreateDeviceID(path)
		if err != nil {
			return nil, err
		}
	}
	s := newSource(NewTransport(base, device, WithMinGap(minGap)))
	if err := s.loadCircles(srcs); err != nil {
		return nil, err
	}
	return s, nil
}

// newSource seeds one source with its fixed recommend feed — the
// constructor both New and the test builder funnel through, so the
// feed list is never empty by accident.
func newSource(tr *Transport) *Source {
	s := &Source{tr: tr, topics: map[string]int64{}}
	s.feeds = []core.Feed{{
		Key:      RecommendFeedKey,
		LabelKey: recommendLabelKey,
		URL:      tr.base + recommendPath,
		// The algorithmic stream orders by the card's own clock
		// (modify/create, newest first) — ListSortTime, the zero
		// value, the same call the zhihu follow feed makes.
	}}
	return s
}

// newSourceAt is the test constructor: a transport pinned to one base
// with explicit options (fake server, zero delay). Tests grow circles
// on top via loadCircles or the field itself.
func newSourceAt(base string, opts ...TransportOpt) *Source {
	return newSource(NewTransport(base, "0123456789abcdef0123456789abcdef", opts...))
}

// ID implements source.Source.
func (s *Source) ID() string { return ID }

// Feeds implements source.Source: the recommendation feed first,
// then one feed per subscribed circle in sources.toml's declaration
// order (sidebar order). With no circles subscribed the recommend
// feed stands alone — the spec's "无圈时源仍可用".
func (s *Source) Feeds() []core.Feed {
	out := make([]core.Feed, len(s.feeds))
	copy(out, s.feeds)
	return out
}

// Fetch implements source.Source: the given feed's first page —
// FetchPage's page 0 under the plain-Source contract (a plain Fetch
// is what the doctor-style single pulls ride).
func (s *Source) Fetch(ctx context.Context, f core.Feed) ([]core.Item, error) {
	return s.FetchPage(ctx, f, 0)
}

// FetchPage implements source.PagedSource: one page of the recommend
// feed or a circle feed, offset == page (P15's measured paging —
// offset 1 served page two with zero overlap, measured on the topic
// endpoint the circles ride). An empty page is the feed's end signal,
// per the interface contract.
func (s *Source) FetchPage(ctx context.Context, f core.Feed, page int) ([]core.Item, error) {
	if f.Key == RecommendFeedKey {
		return s.fetchCards(ctx, f, "recommend-feed", recommendPath, url.Values{
			"offset": {strconv.Itoa(page)},
			"pull":   {"0"},
			"dw":     {"800"},
		})
	}
	if _, ok := s.topics[f.Key]; ok {
		return s.fetchCirclePage(ctx, f, page)
	}
	return nil, fmt.Errorf("fetch %s: unknown heybox feed", f.Key)
}

// fetchCards runs one list page through the shared pipeline — dial,
// peel the envelope, map cards to rows, drop ads and the page's own
// repeats. Both list endpoints (/bbs/app/feeds P14, /bbs/app/
// topic/feeds P15/08) serve the same link array, which is why the
// recommend feed and the circle feeds differ in nothing but their
// parameters (circle.go).
func (s *Source) fetchCards(ctx context.Context, f core.Feed, name, path string, extra url.Values) ([]core.Item, error) {
	res := s.tr.Get(ctx, name, path, extra)
	if res.Err != nil {
		return nil, fmt.Errorf("fetch %s: %w", f.Key, res.Err)
	}
	var pageResult struct {
		Links []linkCard `json:"links"`
	}
	if err := json.Unmarshal(res.Result, &pageResult); err != nil {
		return nil, fmt.Errorf("parse %s: not heybox JSON: %w", f.Key, err)
	}
	now := time.Now().UTC()
	items, seen := make([]core.Item, 0, len(pageResult.Links)), map[string]bool{}
	for _, card := range pageResult.Links {
		it, ok := cardItem(card, f.Key, now)
		if !ok || seen[it.ID] {
			continue // ad card / degenerate card, or the page's own repeat
		}
		seen[it.ID] = true
		items = append(items, it)
	}
	return items, nil
}

// linkCard is one list entry as the web endpoints serve it — the
// recommend feed (P14) and the topic feeds (P15/08, field-compatible)
// share the shape. Only the fields a post card renders are declared;
// everything else the host slides in stays raw and ignored.
type linkCard struct {
	LinkID       json.Number `json:"linkid"`         // decimal id; ABSENT on ad cards
	ShareURL     string      `json:"share_url"`      // carries the hex link_id — the detail key
	Title        string      `json:"title"`          // may be empty on text posts
	Description  string      `json:"description"`    // the text post's body / summary
	LinkAwardNum json.Number `json:"link_award_num"` // heat: award (赞) count — `up` serves 0 on web
	CommentNum   json.Number `json:"comment_num"`    // heat: comment count
	CreateAt     int64       `json:"create_at"`
	ModifyAt     int64       `json:"modify_at"`
	User         struct {
		Username string `json:"username"`
	} `json:"user"`
}

// itemIDPrefix is the row id shape's single source: hb:link/<hexid>.
const itemIDPrefix = "hb:link/"

// ItemID builds the deterministic row id "hb:link/<hexid>" — the hex
// id every list card carries in its share_url (decimal↔hex is not
// locally computable; the smoke's finding 2), so the row identity and
// the detail-layer key are the same string however the card arrived.
func ItemID(hexID string) string { return itemIDPrefix + hexID }

// cardItem maps one list card to its row (GLOSSARY: 帖子卡) under
// the feed it arrived in — the row carries the circle's feed key so
// the store's ownership check passes and the sidebar counts land on
// the right circle. Ad cards (no linkid — content_type 23's banners)
// and cards without a hex id in share_url are not rows, not errors:
// no hex id means no detail key and no stable identity, and half a
// row is worse than none. Text posts ride the description as title
// excerpt and full body snapshot; the heat snapshot folds award +
// comment counts; the card clock (modify wins — an edited post
// re-surfaces) orders the feed.
func cardItem(c linkCard, feedKey string, now time.Time) (core.Item, bool) {
	hexID := hexIDFromShareURL(c.ShareURL)
	if hexID == "" {
		return core.Item{}, false
	}
	title := Degrade(c.Title)
	body := Degrade(c.Description)
	if strings.TrimSpace(title) == "" {
		title = excerpt(body)
	}
	if title == "" {
		return core.Item{}, false // no title, no text: nothing renderable
	}
	it := core.Item{
		ID:          ItemID(hexID),
		Source:      ID,
		Feed:        feedKey,
		Title:       title,
		URL:         c.ShareURL,
		Author:      c.User.Username,
		Body:        body,
		FetchedAt:   now,
		Points:      numPtr(c.LinkAwardNum),
		NumComments: numPtr(c.CommentNum),
	}
	if ts := cardClock(c); ts != 0 {
		t := time.Unix(ts, 0).UTC()
		it.PublishedAt = &t
	}
	return it, true
}

// cardClock picks a card's own clock: modify_at when the post was
// edited after creation, else create_at — the ordering a
// chronological feed wants (the zhihu cardClock call, same name for
// the same job).
func cardClock(c linkCard) int64 {
	if c.ModifyAt != 0 {
		return c.ModifyAt
	}
	return c.CreateAt
}

// hexIDFromShareURL extracts the hex link_id from a card's share_url
// query — the detail layer's key and the row's identity. Malformed
// URLs and ids that are not lowercase hex answer "" (not a row).
func hexIDFromShareURL(shareURL string) string {
	u, err := url.Parse(shareURL)
	if err != nil {
		return ""
	}
	if id := u.Query().Get("link_id"); isLowerHex(id) {
		return id
	}
	return ""
}

// numPtr converts a JSON number field to *int, nil when absent — the
// heat columns keep "missing" distinct from zero the hn way, though
// the web endpoints always serve both fields (a zero award is a real
// zero; the UI's meta decides what to show).
func numPtr(n json.Number) *int {
	if n == "" {
		return nil
	}
	v, err := strconv.Atoi(n.String())
	if err != nil {
		return nil
	}
	return &v
}

// cubeRE matches the `[cube_…]` emoji markers heybox text carries
// (`[cube_滑稽]`, `[cube_笑c]`).
var cubeRE = regexp.MustCompile(`\[cube_([^\]]*)\]`)

// Degrade applies the text degradation rules the spec fixes for heybox
// content: `[cube_X]` emoji markers become `[X]`. (Image/video/article
// degradations are detail-layer concerns — ticket 03 extends this
// family.) Titles and descriptions both pass through it: a title that
// renders `[笑c]` is honest about what it lost.
func Degrade(text string) string {
	return cubeRE.ReplaceAllString(text, "[$1]")
}

// excerpt squeezes a text post's body into a one-line row title:
// whitespace collapsed, first titleExcerptRunes runes kept, ellipsis
// when cut. Empty text answers "" (the caller's not-a-row verdict).
const titleExcerptRunes = 40

func excerpt(text string) string {
	single := strings.Join(strings.Fields(text), " ")
	if single == "" {
		return ""
	}
	if utf8.RuneCountInString(single) <= titleExcerptRunes {
		return single
	}
	runes := []rune(single)
	return string(runes[:titleExcerptRunes]) + "…"
}
