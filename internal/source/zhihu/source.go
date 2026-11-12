package zhihu

// source.go — the Source registration and the three fixed feeds: the
// hot board (ticket 02) mapped to question-card snapshots with
// url-extracted ids (GLOSSARY: 问题卡) and the folded heat snapshot
// (GLOSSARY: 热度快照); the follow feed (ticket 05), the cursor
// variant of source.PagedSource — heterogeneous moments cards
// filtered down to the same question-card rows, walked forward by
// zhihu's opaque paging.next cursor; and the daily digest (ticket
// 03), an anonymous day snapshot whose full texts arrive lazily:
// the zhihu half of source.BodySource, the story body fetched on
// first open through CleanHTML into the body snapshot. Since ticket
// 04 every fetch rides the shared transport (transport.go) — the
// request matrix and the session-loss semantics live there,
// single-point. The detail layers arrive with ticket 06.

import (
	"context"
	"encoding/json"
	"fmt"
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

// FollowFeedKey is the follow feed's stable key (GLOSSARY: 关注流) —
// cookie-gated, cursor-paged.
const FollowFeedKey = "follow"

// RecommendFeedKey is the recommend feed's stable key (GLOSSARY:
// 推荐流) — the home page's algorithmic feed, cookie-gated,
// cursor-paged, same door family as the follow feed (ticket 08).
const RecommendFeedKey = "recommend"

// DailyFeedKey is the daily digest's stable key (GLOSSARY: 知乎日报)
// — anonymous, a day's snapshot, lazy bodies.
const DailyFeedKey = "daily"

// hotLabelKey is the hot feed's i18n key; the UI resolves the label.
const hotLabelKey = "zhihu.hot"

// followLabelKey is the follow feed's i18n key.
const followLabelKey = "zhihu.follow"

// recommendLabelKey is the recommend feed's i18n key.
const recommendLabelKey = "zhihu.recommend"

// dailyLabelKey is the daily feed's i18n key.
const dailyLabelKey = "zhihu.daily"

func init() { source.Register(ID, New) }

// Source is the zhihu implementation of source.Source and of the
// cursor PagedSource variant (ticket 05). The remaining optional
// extension — DetailSource for the question's three layers — arrives
// with ticket 06, and callers probe with type assertions, so its
// absence is a non-feature, not an error (Ctrl+N on the hot board
// says "no more items", Enter reads the empty body).
type Source struct {
	hosts Hosts      // the three API roots; ProdHosts in production
	tr    *Transport // every request rides the shared matrix (transport.go)
}

// The optional extensions zhihu carries, pinned at compile time.
var (
	_ source.CursorPagedSource = (*Source)(nil)
	_ source.BodySource        = (*Source)(nil)
)

// New builds the zhihu source. The subscription config goes unused
// today (the hot feed is fixed); READSCOPE_ZHIHU_BASE overrides every
// host root with one base — the same override `zhihu probe` honors,
// so tests point one fake server at all three doors and a proxy debug
// run can too.
//
// 保存即用 (the Ctrl+U contract): the cookie loads per construction —
// every refresh builds a fresh source (refresh → source.New), so a
// re-login takes effect on the next F5 without a restart. A missing
// file is the anonymous mode, not an error; a broken or z_c0-less
// file degrades to anonymous too, so gated calls answer
// ErrLoginRequired (pointing at Ctrl+U) instead of a confusing 403 —
// doctor names the file's real state.
func New(*core.Sources) (source.Source, error) {
	hosts := ProdHosts
	if v := os.Getenv("READSCOPE_ZHIHU_BASE"); v != "" {
		hosts = Hosts{API: v, Daily: v, Web: v, Zhuanlan: v}
	}
	cookie := ""
	if path, err := CookiePath(); err == nil {
		if header, info, err := LoadCookie(path); err == nil && info.HasZC0 {
			cookie = header
		}
	}
	return newSource(hosts, cookie), nil
}

func newSource(hosts Hosts, cookie string) *Source {
	return &Source{hosts: hosts, tr: NewTransport(hosts, cookie, nil)}
}

// ID implements source.Source.
func (s *Source) ID() string { return ID }

// hotURL is the anonymous hot door. No limit parameter: the board
// serves its fixed cap (~30 rows) whatever you ask for.
func (s *Source) hotURL() string { return s.hosts.API + "/topstory/hot-list" }

// momentsURL is the follow feed's first page. Later pages ride the
// paging.next cursor verbatim (FetchCursorPage) — the server, not the
// client, spells its own page addresses.
func (s *Source) momentsURL() string {
	return s.hosts.Web + "/api/v3/moments?limit=10&desktop=true"
}

// recommendURL is the recommend feed's first page: the home page's
// own XHR door, no seed needed (probe 2026-11-12: the server hands
// session_token inside paging.next, the entry takes ?desktop=true
// alone). Later pages ride paging.next verbatim, same as moments.
func (s *Source) recommendURL() string {
	return s.hosts.Web + "/api/v3/feed/topstory/recommend?desktop=true"
}

// dailyLatestURL is the daily digest's anonymous list door: one
// GET, the current issue whole (no paging — a day is the full
// snapshot).
func (s *Source) dailyLatestURL() string { return s.hosts.Daily + "/api/4/news/latest" }

// storyURL is one story's API door (the body behind FetchBody).
func (s *Source) storyURL(storyID string) string {
	return s.hosts.Daily + "/api/4/story/" + storyID
}

// Feeds implements source.Source: the hot board, the recommend
// feed, the follow feed and the daily digest in sidebar order (spec
// revision 2: 热榜 → 推荐 → 关注 → 日报).
func (s *Source) Feeds() []core.Feed {
	return []core.Feed{
		{
			Key:      HotFeedKey,
			LabelKey: hotLabelKey,
			URL:      s.hotURL(),
			// The board's own return order is heat order, so the feed
			// declares ListSortPoints like hn's popularity feeds and the
			// UI ranks rows by the heat snapshot.
			ListSort: core.ListSortPoints,
		},
		{
			Key:      RecommendFeedKey,
			LabelKey: recommendLabelKey,
			URL:      s.recommendURL(),
			// The algorithmic stream orders by the card's own clock
			// (created/updated, newest first) like the follow feed —
			// ListSortTime, the zero value.
		},
		{
			Key:      FollowFeedKey,
			LabelKey: followLabelKey,
			URL:      s.momentsURL(),
			// The moments stream is chronological: ListSortTime (the
			// zero value), newest first by the card's own clock.
		},
		{
			Key:      DailyFeedKey,
			LabelKey: dailyLabelKey,
			URL:      s.dailyLatestURL(),
			// A day's digest is one snapshot: chronological order (the
			// zero value), no pages behind it.
		},
	}
}

// ItemID builds the deterministic row id "zh:question/<qid>". Every
// zhihu list that renders question cards mints this same id (hot and
// the follow feed), so one question is one row however it arrives —
// the store's cross-feed re-homing moves it.
func ItemID(qid string) string { return itemIDPrefix + qid }

// itemIDPrefix is the single source of the id shape.
const itemIDPrefix = "zh:question/"

// articleIDPrefix is the article row id's single source (ticket 09):
// an article never folds into a question — it is its own row and its
// own two-layer detail (GLOSSARY: 问题卡 vs 文章卡).
const articleIDPrefix = "zh:article/"

// ArticleItemID builds the deterministic row id "zh:article/<id>".
// Both cursor feeds (recommend and follow) mint this same id, so one
// article is one row however it arrives.
func ArticleItemID(id string) string { return articleIDPrefix + id }

// IsArticleItem reports whether an item id is an article row's — the
// UI's dispatch key for the two-layer detail (body → comments, no
// floors layer in between).
func IsArticleItem(itemID string) bool {
	_, ok := articleIDOf(itemID)
	return ok
}

// articleIDOf extracts the article id from a "zh:article/<id>" item
// id — DetailOnDemand's dispatch key (the third id shape after
// zh:question and zh:daily).
func articleIDOf(itemID string) (string, bool) {
	return strings.CutPrefix(itemID, articleIDPrefix)
}

// articlePage renders the column's human-openable page — Ctrl+O and
// Ctrl+Y's target for an article row (and its comment layer: a bare
// comment id addresses no article page on its own).
func articlePage(articleID string) string { return "https://zhuanlan.zhihu.com/p/" + articleID }

// dailyIDPrefix is the daily story id shape's single source; daily
// ids share no namespace with question ids — a story is never a
// question row (GLOSSARY: 问题卡 vs 知乎日报).
const dailyIDPrefix = "zh:daily/"

// DailyItemID builds the deterministic row id "zh:daily/<storyID>".
func DailyItemID(storyID string) string { return dailyIDPrefix + storyID }

// dailyStoryPage renders the human-openable story page — Ctrl+O and
// Ctrl+Y's target. The API door the fetch rides is not a page.
func dailyStoryPage(storyID string) string { return "https://daily.zhihu.com/story/" + storyID }

// questionURL renders the canonical www question page — the
// Ctrl+O/Ctrl+Y target. target.url's internal spelling
// (api…/questions/<id>) is not a page a human can open.
func questionURL(qid string) string { return "https://www.zhihu.com/question/" + qid }

// Fetch implements source.Source: dispatches one fixed feed. Skips
// are never errors, fetch and parse errors name the feed. The follow
// feed is NOT here — its pages carry the cursor the caller must
// persist, which Fetch's items-only shape cannot (see the refusal
// below); the refresh layer routes it to FetchCursorPage first.
func (s *Source) Fetch(ctx context.Context, f core.Feed) ([]core.Item, error) {
	switch f.Key {
	case HotFeedKey:
		return s.fetchHot(ctx, f)
	case DailyFeedKey:
		return s.fetchDaily(ctx, f)
	case FollowFeedKey, RecommendFeedKey:
		// A loud refusal, not a half-fetch: the cursor feeds' pages
		// carry the cursor the caller must persist, which Fetch's
		// items-only shape cannot — the refresh layer routes them to
		// FetchCursorPage before probing Fetch, so landing here is a
		// wiring bug, and it should say so.
		return nil, fmt.Errorf("fetch %s: cursor-paged feed — walk it via FetchCursorPage", f.Key)
	default:
		return nil, fmt.Errorf("fetch %s: unknown zhihu feed", f.Key)
	}
}

// fetchHot pulls the whole board in one anonymous GET. Only question
// cards become rows — non-question cards (video/pin/…) skip, as do
// rows without an extractable qid or a title; the heat column folds
// into the points snapshot.
func (s *Source) fetchHot(ctx context.Context, f core.Feed) ([]core.Item, error) {
	res := s.tr.Get(ctx, epHotList, s.hotURL())
	if res.Err != nil {
		return nil, fmt.Errorf("fetch %s: %w", f.Key, res.Err)
	}
	var list hotList // the shape both hot doors serve (probe.go)
	if err := json.Unmarshal(res.Body, &list); err != nil {
		return nil, fmt.Errorf("parse %s: not zhihu JSON: %w", f.Key, err)
	}
	now := time.Now().UTC()
	items := make([]core.Item, 0, len(list.Data))
	for _, row := range list.Data {
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

// fetchDaily pulls the current issue in one anonymous GET. The story
// list carries no body and no per-story clock — the full text is a
// first-open fetch (FetchBody below), and the row's meta is the
// issue's own hint string ("作者 · 2 分钟阅读"), served atomic and
// stored verbatim in the meta column. Rows without an id or a title
// skip like the board's degenerate cards.
func (s *Source) fetchDaily(ctx context.Context, f core.Feed) ([]core.Item, error) {
	res := s.tr.Get(ctx, epDailyLatest, s.dailyLatestURL())
	if res.Err != nil {
		return nil, fmt.Errorf("fetch %s: %w", f.Key, res.Err)
	}
	var list dailyLatest // the shape the probe walks too (probe.go)
	if err := json.Unmarshal(res.Body, &list); err != nil {
		return nil, fmt.Errorf("parse %s: not zhihu JSON: %w", f.Key, err)
	}
	now := time.Now().UTC()
	items := make([]core.Item, 0, len(list.Stories))
	for _, st := range list.Stories {
		sid := st.ID.String()
		if sid == "" || sid == "0" || st.Title == "" {
			continue // nothing renderable; skip, never crash
		}
		items = append(items, core.Item{
			ID:     DailyItemID(sid),
			Source: ID,
			Feed:   DailyFeedKey,
			Title:  st.Title,
			URL:    dailyStoryPage(sid),
			Meta:   st.Hint,
			// The digest declares no per-story timestamps: PublishedAt
			// stays nil and FetchedAt is the snapshot clock — a fresh
			// issue tops the list over older cached ones.
			FetchedAt: now,
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

// --- the follow feed (ticket 05) -----------------------------------------

// CursorPaged implements source.CursorPagedSource: the follow and
// recommend feeds page by cursor — the board is a fixed window and
// the daily digest a day's snapshot, neither has a page two.
func (s *Source) CursorPaged(f core.Feed) bool {
	return f.Key == FollowFeedKey || f.Key == RecommendFeedKey
}

// FetchCursorPage implements source.CursorPagedSource: one moments or
// recommend page — the first when cursor is "", else the page the
// server addressed with that cursor (paging.next rides verbatim: the
// server spells its own page addresses). Only question cards become
// rows (questionCard folds answer cards into their question and
// drops everything else — the article card included until ticket 09);
// the same question twice on one page keeps the first card. The next
// walk position is the server's paging.next, "" meaning no further
// page — which, not an empty filtered page, is the walk's end (see
// the interface contract).
func (s *Source) FetchCursorPage(ctx context.Context, f core.Feed, cursor string) ([]core.Item, string, error) {
	var url string
	switch f.Key {
	case FollowFeedKey:
		url = s.momentsURL()
	case RecommendFeedKey:
		url = s.recommendURL()
	default:
		return nil, "", fmt.Errorf("fetch %s: not a zhihu cursor feed", f.Key)
	}
	if cursor != "" {
		url = cursor
	}
	ep := epMoments
	if f.Key == RecommendFeedKey {
		ep = epRecommend
	}
	res := s.tr.Get(ctx, ep, url)
	if res.Err != nil {
		return nil, "", fmt.Errorf("fetch %s: %w", f.Key, res.Err)
	}
	var page moments
	if err := json.Unmarshal(res.Body, &page); err != nil {
		return nil, "", fmt.Errorf("parse %s: not zhihu JSON: %w", f.Key, err)
	}
	now := time.Now().UTC()
	items, seen := make([]core.Item, 0, len(page.Data)), map[string]bool{}
	for _, card := range page.Data {
		it, ok := feedCard(card, f.Key, now)
		if !ok || seen[it.ID] {
			continue // 同题去重: the page's first card owns the row
		}
		seen[it.ID] = true
		items = append(items, it)
	}
	return items, page.Paging.Next, nil
}

// feedCard maps one moments-family card (moments and recommend
// serve field-compatible targets, probe 2026-11-12) to its row
// (GLOSSARY: 问题卡 / 文章卡), stamped with the feed it arrived on.
// Answer cards fold into their question — the answerer's name rides
// the author column — question cards pass through, and since ticket
// 09 article cards pass through as their own zh:article rows (the
// mapping both cursor feeds share). Every other card
// (pin/video/advert/…) is not a row and not an error. Cards without
// a usable id or title skip the same way the hot board's degenerate
// rows do.
func feedCard(c momentsCard, feedKey string, now time.Time) (core.Item, bool) {
	var id, title, author string
	isArticle := c.Target.Type == "article"
	switch c.Target.Type {
	case "answer":
		// The card's own id is the answer's; the row is the question it
		// answers (json.Number keeps the big id digits exact).
		id, title, author = c.Target.Question.ID.String(), c.Target.Question.Title, c.Target.Author.Name
	case "question":
		id, title, author = c.Target.ID.String(), c.Target.Title, c.Target.Author.Name
	case "article":
		// The article's own row: the column page is the Ctrl+O/Ctrl+Y
		// target (the API door is not a page). json.Number again — the
		// probe measured article ids past 2^53.
		id, title, author = c.Target.ID.String(), c.Target.Title, c.Target.Author.Name
	default:
		return core.Item{}, false
	}
	if id == "" || id == "0" || title == "" {
		return core.Item{}, false // nothing renderable; skip, never crash
	}
	rowID, url := ItemID(id), questionURL(id) // questions share one namespace across feeds
	if isArticle {
		rowID, url = ArticleItemID(id), articlePage(id)
	}
	it := core.Item{
		ID:     rowID,
		Source: ID,
		Feed:   feedKey,
		Title:  title,
		URL:    url,
		Author: author,
		// A follow stream is chronological and pages land at different
		// moments: the card's own clock — not the fetch's — orders the
		// list, or deeper (older) pages would sort above fresh ones.
		FetchedAt: now,
	}
	if ts := cardTime(c.Target); ts != 0 {
		t := time.Unix(ts, 0).UTC()
		it.PublishedAt = &t
	}
	return it, true
}

// cardTime picks a moments-family card's clock: the question/answer
// cards spell it updated_time/created_time, the article card
// created/updated (probe 2026-11-12 — no _time suffix on articles).
// updated wins when present (an edited old answer re-surfaces in the
// stream), created is the fallback; zero means the card carried no
// clock at all and the fetch's clock orders the row.
func cardTime(t momentsTarget) int64 {
	if t.UpdatedTime != 0 {
		return t.UpdatedTime
	}
	if t.CreatedTime != 0 {
		return t.CreatedTime
	}
	if t.Updated != 0 {
		return t.Updated
	}
	return t.Created
}

// --- the daily digest's lazy body (ticket 03) -----------------------------

// BodyOnDemand implements source.BodySource: daily stories alone —
// their full text is a first-open fetch, while question cards are
// detail-layer items (ticket 06) whose bodies never ride this path.
func (s *Source) BodyOnDemand(it core.Item) bool {
	return strings.HasPrefix(it.ID, dailyIDPrefix)
}

// FetchBody implements source.BodySource: one anonymous GET to the
// story door, the body HTML preprocessed through CleanHTML for the
// render pipeline. The caller persists the return into the body
// snapshot; re-opens never dial again (二次打开渲染缓存零请求).
// An empty body errors like the probe's BLOCKED verdict — a well
// 200 that serves nothing is a shape moved, not a story.
func (s *Source) FetchBody(ctx context.Context, it core.Item) (string, error) {
	storyID, ok := strings.CutPrefix(it.ID, dailyIDPrefix)
	if !ok {
		return "", fmt.Errorf("fetch body: %q is not a daily story id", it.ID)
	}
	res := s.tr.Get(ctx, epDailyStory, s.storyURL(storyID))
	if res.Err != nil {
		return "", fmt.Errorf("fetch body %s: %w", it.ID, res.Err)
	}
	var story dailyStory // the shape the probe walks too (probe.go)
	if err := json.Unmarshal(res.Body, &story); err != nil {
		return "", fmt.Errorf("parse story %s: not zhihu JSON: %w", storyID, err)
	}
	if strings.TrimSpace(story.Body) == "" {
		return "", fmt.Errorf("parse story %s: empty body — 站点可能改版?", storyID)
	}
	return CleanHTML(story.Body), nil
}
