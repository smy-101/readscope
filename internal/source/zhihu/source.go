package zhihu

// source.go — the Source registration and the hot feed (ticket 02):
// one anonymous GET, rows mapped to question-card snapshots with
// url-extracted ids (GLOSSARY: 问题卡) and the folded heat snapshot
// (GLOSSARY: 热度快照). The cookie-gated feeds and the detail layers
// arrive with their tickets; until then zhihu is list-only.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
)

// ID is the source id in sources.toml's enabled list.
const ID = "zhihu"

// HotFeedKey is the hot feed's stable key (the sidebar slug and the
// Feed column of every hot row).
const HotFeedKey = "hot"

// hotLabelKey is the hot feed's i18n key; the UI resolves the label.
const hotLabelKey = "zhihu.hot"

// fetchTimeout bounds one feed's HTTP round trip; parsing is local.
const fetchTimeout = 30 * time.Second

func init() { source.Register(ID, New) }

// Source is the zhihu implementation of source.Source: the anonymous
// hot feed today. The optional extensions arrive with their tickets —
// DetailSource for the question's three layers (06), the cursor
// PagedSource variant for the follow feed (05) — and callers probe
// with type assertions, so their absence is a non-feature, not an
// error (Ctrl+N says "no more items", Enter reads the empty body).
type Source struct {
	hosts  Hosts // the three API roots; ProdHosts in production
	client *http.Client
}

// New builds the zhihu source. The subscription config goes unused
// today (the hot feed is fixed); READSCOPE_ZHIHU_BASE overrides every
// host root with one base — the same override `zhihu probe` honors,
// so tests point one fake server at all three doors and a proxy debug
// run can too.
func New(*core.Sources) (source.Source, error) {
	hosts := ProdHosts
	if v := os.Getenv("READSCOPE_ZHIHU_BASE"); v != "" {
		hosts = Hosts{API: v, Daily: v, Web: v}
	}
	return newSource(hosts), nil
}

func newSource(hosts Hosts) *Source {
	return &Source{hosts: hosts, client: &http.Client{Timeout: fetchTimeout}}
}

// ID implements source.Source.
func (s *Source) ID() string { return ID }

// hotURL is the anonymous hot door. No limit parameter: the board
// serves its fixed cap (~30 rows) whatever you ask for.
func (s *Source) hotURL() string { return s.hosts.API + "/topstory/hot-list" }

// Feeds implements source.Source: the hot feed alone in sidebar order
// — 日报 joins in ticket 03 and 关注 in ticket 05, both after the
// board.
func (s *Source) Feeds() []core.Feed {
	return []core.Feed{{
		Key:      HotFeedKey,
		LabelKey: hotLabelKey,
		URL:      s.hotURL(),
		// The board's own return order is heat order, so the feed
		// declares ListSortPoints like hn's popularity feeds and the
		// UI ranks rows by the heat snapshot.
		ListSort: core.ListSortPoints,
	}}
}

// ItemID builds the deterministic row id "zh:question/<qid>". Every
// zhihu list that renders question cards mints this same id (hot
// today, the follow feed in ticket 05), so one question is one row
// however it arrives — the store's cross-feed re-homing moves it.
func ItemID(qid string) string { return itemIDPrefix + qid }

// itemIDPrefix is the single source of the id shape.
const itemIDPrefix = "zh:question/"

// questionURL renders the canonical www question page — the
// Ctrl+O/Ctrl+Y target. target.url's internal spelling
// (api…/questions/<id>) is not a page a human can open.
func questionURL(qid string) string { return "https://www.zhihu.com/question/" + qid }

// Fetch implements source.Source: the whole board in one anonymous
// GET. Only question cards become rows — non-question cards
// (video/pin/…) skip, as do rows without an extractable qid or a
// title; skips are never errors, fetch and parse errors name the
// feed. The heat column folds into the points snapshot.
func (s *Source) Fetch(ctx context.Context, f core.Feed) ([]core.Item, error) {
	if f.Key != HotFeedKey {
		return nil, fmt.Errorf("fetch %s: unknown zhihu feed", f.Key)
	}
	raw, err := s.get(ctx, s.hotURL(), hotReferer, "fetch "+f.Key)
	if err != nil {
		return nil, err
	}
	var res hotList // the shape both hot doors serve (probe.go)
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("parse %s: not zhihu JSON: %w", f.Key, err)
	}
	now := time.Now().UTC()
	items := make([]core.Item, 0, len(res.Data))
	for _, row := range res.Data {
		if row.Target.Type != "question" {
			continue // not a question card: not a row, not an error
		}
		qid := QIDFromURL(row.Target.URL)
		if qid == "" || row.Target.Title == "" {
			continue // no id or no title → nothing renderable; skip, never crash
		}
		items = append(items, core.Item{
			ID:     ItemID(qid),
			Source: ID,
			Feed:   HotFeedKey,
			Title:  row.Target.Title,
			URL:    questionURL(qid),
			// A ranked board carries no timestamps: PublishedAt stays
			// nil (the list's meta column is the heat, not the age)
			// and FetchedAt is the snapshot clock.
			FetchedAt: now,
			Points:    heatPoints(row.DetailText),
		})
	}
	return items, nil
}

// wanHeatRE matches the board's heat column "2045 万热度" — the space
// optional, zhihu has served both spellings.
var wanHeatRE = regexp.MustCompile(`^([0-9]+)\s*万热度$`)

// heatPoints folds detail_text into the points snapshot: "2045 万热度"
// → 20450000. Anything else — a pinned card's empty column, a format
// zhihu invents later — is nil: the row lists without a heat segment
// rather than claiming a wrong number.
//
// The unfold (÷10000) lives in ui.zhihuMeta — the pair is the hn.pts
// shape (folding is the source's semantics, formatting the UI's),
// and each side is pinned against the literal "2045 万热度" ↔
// 20450000 by its own test, so a drifted constant fails one of them.
func heatPoints(detailText string) *int {
	m := wanHeatRE.FindStringSubmatch(strings.TrimSpace(detailText))
	if m == nil {
		return nil
	}
	wan, err := strconv.Atoi(m[1])
	if err != nil {
		return nil
	}
	n := wan * 10000
	return &n
}

// hotReferer is the hot door's Referer, matching the measured request
// shape (the probe sends the same).
const hotReferer = "https://www.zhihu.com/hot"

// get performs the shared HTTP skeleton of the source's fetches — the
// measured fingerprint (desktop UA, x-requested-with, a real fetch's
// Accept headers, the per-endpoint Referer) on one GET with a hard
// body cap, errors naming the operation (op, e.g. "fetch hot"). The
// hot door needs neither cookie nor x-zse-93; ticket 04 grows this
// into the shared transport the cookie-gated endpoints ride.
func (s *Source) get(ctx context.Context, url, referer, op string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	req.Header.Set("User-Agent", DesktopUA)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("x-requested-with", "fetch")
	req.Header.Set("Referer", referer)
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
