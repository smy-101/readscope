package zhihu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProbeArgs overrides the ids the walk would otherwise chain from
// earlier responses — for re-running one deep endpoint against a
// known qid/aid/story without re-walking the list layer.
type ProbeArgs struct {
	QID     string // question id for the v4 family
	StoryID string // daily story id
	Aid     string // answer id for comments-root
}

// EndpointResult is one walked endpoint's outcome. Verdict is GREEN
// (2xx and parsed), BLOCKED (transport error, 4xx/5xx, or unparseable
// body), SKIPPED (no cookie / no upstream id), or for the two header
// self-checks MATCH/MISMATCH (observed vs the matrix's expectation).
type EndpointResult struct {
	Name    string
	Status  string // "HTTP 200", "ERR", "SKIPPED"
	Verdict string
	Note    string
}

// Verdict strings, named for the ticket's stop-loss vocabulary.
const (
	verdictGreen    = "GREEN"
	verdictBlocked  = "BLOCKED"
	verdictSkipped  = "SKIPPED"
	verdictMatch    = "MATCH"
	verdictMismatch = "MISMATCH"
)

// Probe is the M4 data-plane tracer: walk the spec's endpoint contract
// (hot list, daily list + story, follow feed, question meta, answer
// feeds, comment root), print one line per endpoint, save every raw
// body under OutDir, and self-check the x-zse-93 header matrix.
// It never prints cookie values. Since ticket 04 the walk rides the
// shared transport (transport.go) — the same matrix the source
// fetches wear, one header block for the whole package.
type Probe struct {
	Hosts        Hosts
	CookieHeader string // "" = walk the anonymous endpoints only
	OutDir       string // "" = don't save samples
	Client       *http.Client
	Args         ProbeArgs
}

const (
	metaInclude = "detail%2Cexcerpt%2Canswer_count%2Cfollower_count%2Cvisit_count%2Cauthor%2Ccreated"
	// Feeds include is the page request's own string (FeedScope
	// 2026-11, "勿手改"): the cursorless entry accepts exactly this
	// lean set — the full page include without a cursor 403s.
	feedsInclude = "data%5B%2A%5D.is_normal%2Ccomment_count%2Ccan_comment%2Ccontent%2C" +
		"voteup_count%2Ccomment_permission%2Ccreated_time%2Cupdated_time%2Cquestion%2C" +
		"excerpt%3Bdata%5B%2A%5D.author.follower_count%2Cvip_info"
)

// Blocked reports whether any walked endpoint came back BLOCKED —
// SKIPPED (no cookie) is a walk outcome, not a blocked data plane.
func Blocked(rs []EndpointResult) bool {
	for _, r := range rs {
		if r.Verdict == verdictBlocked {
			return true
		}
	}
	return false
}

// save writes one raw sample (best effort — a read-only dir must not
// kill the walk; the report line notes where samples were heading).
// Only real exchanges land on disk: a transport-level ERR has no body
// worth keeping, but an HTTP error document does (zhihu's error JSON
// carries the code — 10003 and friends).
func (p *Probe) save(name string, f Response) {
	if p.OutDir == "" || f.Status == 0 {
		return
	}
	if err := os.MkdirAll(p.OutDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(p.OutDir, name+".json"), f.Body, 0o644)
}

// outcome folds one transport response into a walk result: ERR (no
// answer at all), HTTP n + BLOCKED (an answer that said no — the note
// says which reading: session-expired endpoints name the Ctrl+U
// path, everything else reports raw with the error code), or the
// parse's verdict on the 200 body. parse returns the note and whether
// the shape held (0-row parses stay GREEN with the redesign alarm in
// the note — reachability is real).
func (p *Probe) outcome(name string, f Response, parse func(body []byte) (note string, ok bool)) EndpointResult {
	p.save(name, f)
	switch {
	case f.Status == 0:
		return EndpointResult{name, "ERR", verdictBlocked, f.Err.Error()}
	case f.Err != nil:
		note := "blocked" + errCode(f.Body)
		if errors.Is(f.Err, ErrSessionExpired) {
			note = "blocked: session expired (401/403 on a cookie'd call) — re-login (Ctrl+U)" + errCode(f.Body)
		}
		return EndpointResult{name, fmt.Sprintf("HTTP %d", f.Status), verdictBlocked, note}
	}
	note, ok := parse(f.Body)
	if !ok {
		return EndpointResult{name, "HTTP 200", verdictBlocked, note}
	}
	return EndpointResult{name, "HTTP 200", verdictGreen, note}
}

// errCode digs zhihu's error document {"error":{"code":…}} out of a
// non-200 body, for the report line (10003 = the version gate).
func errCode(body []byte) string {
	var e struct {
		Error struct {
			Code json.Number `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Code != "" {
		return " code=" + string(e.Error.Code)
	}
	return ""
}

// ---- response shapes (the fields the report summarizes) ----

// hotList is the shape BOTH hot doors serve: rows of target (type,
// title, url carrying the question id) plus detail_text (the heat
// column — the raw input of the 热度快照, GLOSSARY: 热度快照). IDs
// decode via json.Number to stay exact; production ids come from
// target.url (see QIDFromURL).
type hotList struct {
	Data []struct {
		DetailText string `json:"detail_text"`
		Target     struct {
			ID    json.Number `json:"id"`
			Type  string      `json:"type"`
			Title string      `json:"title"`
			URL   string      `json:"url"`
		} `json:"target"`
	} `json:"data"`
}

type dailyLatest struct {
	Date    string `json:"date"`
	Stories []struct {
		ID    json.Number `json:"id"`
		Title string      `json:"title"`
		Hint  string      `json:"hint"`
	} `json:"stories"`
}

type dailyStory struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// momentsCard is one heterogeneous card of the moments-family feeds
// (follow and recommend serve field-compatible cards, probe
// 2026-11-12): only answer and question cards ever reach a list
// as question cards (GLOSSARY: 问题卡); everything else the probe
// censuses as dropped. updated_time/created_time are the card's own
// clock — the follow and recommend lists order by them, not by
// fetch time.
type momentsCard struct {
	Target momentsTarget `json:"target"`
}

// momentsTarget is the card's payload half (both cursor feeds —
// follow and recommend — deserialize this same shape). Ids are
// json.Number — question ids run past 2^53, and any float64 path
// (the default unmarshal) mangles the low digits into an id that
// 404s on open; the number's exact string is the only trustworthy
// spelling. The dual clock (updated/created) is cardTime's input:
// the cursor-feed lists order by the card's own clock, not the
// fetch's.
type momentsTarget struct {
	Type        string      `json:"type"`
	ID          json.Number `json:"id"`
	Title       string      `json:"title"`
	Author      struct{ Name string }
	UpdatedTime int64 `json:"updated_time"`
	CreatedTime int64 `json:"created_time"`
	Question    struct {
		ID    json.Number `json:"id"`
		Title string      `json:"title"`
	} `json:"question"`
}

type moments struct {
	Data   []momentsCard `json:"data"`
	Paging struct {
		Next  string `json:"next"`
		IsEnd bool   `json:"is_end"`
	} `json:"paging"`
}

type questionMeta struct {
	Title       string                `json:"title"`
	Author      struct{ Name string } `json:"author"`
	AnswerCount int64                 `json:"answer_count"`
	VisitCount  int64                 `json:"visit_count"`
	Detail      string                `json:"detail"`
}

// feedsPage is one page of a question's answers: target carries the
// floor-head triple (GLOSSARY: 楼层头 — author, voteup_count,
// comment_count) plus the full-content HTML; paging.next is the cursor.
type feedsPage struct {
	Data []struct {
		Target struct {
			ID           json.Number           `json:"id"`
			Type         string                `json:"type"`
			Author       struct{ Name string } `json:"author"`
			VoteupCount  int64                 `json:"voteup_count"`
			CommentCount int64                 `json:"comment_count"`
			Content      string                `json:"content"`
			CreatedTime  int64                 `json:"created_time"`
		} `json:"target"`
	} `json:"data"`
	Paging struct {
		Next  string `json:"next"`
		IsEnd bool   `json:"is_end"`
	} `json:"paging"`
}

// The comment_v5 shapes (commentsPage, commentNode) live in
// detail.go — the walk's summary and ticket 06's comment layers read
// the same payload shape.

// Run walks the contract, streaming one line per endpoint to w and
// returning every result (caller turns BLOCKED into exit 1).
func (p *Probe) Run(ctx context.Context, w io.Writer) []EndpointResult {
	var rs []EndpointResult
	res := func(r EndpointResult) {
		rs = append(rs, r)
		fmt.Fprintf(w, "%-18s %-9s %-8s %s\n", r.Name, r.Status, r.Verdict, r.Note)
	}

	h := p.Hosts
	tr := NewTransport(h, p.CookieHeader, p.Client)
	fmt.Fprintln(w, "zhihu probe — M4 data plane tracer (ticket 01)")
	fmt.Fprintf(w, "hosts: api=%s daily=%s web=%s\n", h.API, h.Daily, h.Web)
	if p.CookieHeader != "" {
		info := SummarizeCookie(p.CookieHeader)
		fmt.Fprintf(w, "cookie: loaded keys=%d z_c0=%v d_c0=%v\n", info.KeyCount, info.HasZC0, info.HasDC0)
	} else {
		fmt.Fprintln(w, "cookie: <none> — login endpoints will report SKIPPED-NO-COOKIE")
	}

	// login gates one endpoint on the cookie: without it the endpoint
	// reports SKIPPED-NO-COOKIE (the anonymous walk is an outcome, not
	// an error). The same gate lives in the transport's Endpoint rows
	// (Gated); the probe keeps its own pre-check because SKIPPED is a
	// walk verdict, not an error.
	login := func(name string) bool {
		if p.CookieHeader == "" {
			res(EndpointResult{name, "SKIPPED", verdictSkipped, "SKIPPED-NO-COOKIE"})
			return false
		}
		return true
	}

	// 1-2. Hot list through both doors. The app-domain twin is the
	// spec's anonymous door (qid chains from here). The www v3
	// gateway FeedScope rides turned out to be session-gated in the
	// wild (HTTP 401 code=101 anonymous, measured on the first real
	// run) — so it walks as a login endpoint and with a cookie merely
	// backs up the api door's data.
	var qid string
	{
		r := p.outcome("hot-list", tr.Get(ctx, epHotList, h.API+"/topstory/hot-list?limit=10"),
			func(b []byte) (string, bool) {
				var hot hotList
				if err := json.Unmarshal(b, &hot); err != nil {
					return "unparseable: " + err.Error(), false
				}
				note := fmt.Sprintf("rows=%d", len(hot.Data)) + redesignNote(len(hot.Data))
				if len(hot.Data) > 0 {
					q := QIDFromURL(hot.Data[0].Target.URL)
					note += fmt.Sprintf(" qid1=%s heat[0]=%q", flag53(q), hot.Data[0].DetailText)
					if q != "" {
						qid = q
					}
				}
				return note, true
			})
		res(r)
	}
	if login("hot-www-door") {
		r := p.outcome("hot-www-door",
			tr.Get(ctx, epHotWWW, h.Web+"/api/v3/feed/topstory/hot-lists/total?limit=50"),
			func(b []byte) (string, bool) {
				var hot hotList
				if err := json.Unmarshal(b, &hot); err != nil {
					return "unparseable: " + err.Error(), false
				}
				note := fmt.Sprintf("rows=%d", len(hot.Data)) + redesignNote(len(hot.Data))
				if len(hot.Data) > 0 {
					note += fmt.Sprintf(" qid1=%s", flag53(QIDFromURL(hot.Data[0].Target.URL)))
				}
				return note, true
			})
		res(r)
	}
	if p.Args.QID != "" {
		qid = p.Args.QID
	}

	// 3. Daily list (anonymous); story id chains from here.
	var storyID string
	{
		r := p.outcome("daily-latest", tr.Get(ctx, epDailyLatest, h.Daily+"/api/4/news/latest"),
			func(b []byte) (string, bool) {
				var latest dailyLatest
				if err := json.Unmarshal(b, &latest); err != nil {
					return "unparseable: " + err.Error(), false
				}
				note := fmt.Sprintf("stories=%d", len(latest.Stories)) + redesignNote(len(latest.Stories))
				if len(latest.Stories) > 0 {
					storyID = string(latest.Stories[0].ID)
					note += fmt.Sprintf(" first=%q hint=%q", latest.Stories[0].Title, latest.Stories[0].Hint)
				}
				return note, true
			})
		res(r)
	}
	if p.Args.StoryID != "" {
		storyID = p.Args.StoryID
	}

	// 4. Daily story body (the offline snapshot's source).
	if storyID == "" {
		res(EndpointResult{"daily-story", "SKIPPED", verdictSkipped, "SKIPPED-UPSTREAM(daily-latest)"})
	} else {
		r := p.outcome("daily-story", tr.Get(ctx, epDailyStory, h.Daily+"/api/4/story/"+storyID),
			func(b []byte) (string, bool) {
				var story dailyStory
				if err := json.Unmarshal(b, &story); err != nil {
					return "unparseable: " + err.Error(), false
				}
				note := fmt.Sprintf("body=%dB html=%v", len(story.Body), strings.Contains(story.Body, "<"))
				if story.Body == "" {
					return note, false
				}
				return note, true
			})
		res(r)
	}

	// 5. Follow feed: card census — question cards kept, the rest
	// counted by type and dropped.
	if login("moments") {
		r := p.outcome("moments",
			tr.Get(ctx, epMoments, h.Web+"/api/v3/moments?limit=10&desktop=true"),
			func(b []byte) (string, bool) {
				var ms moments
				if err := json.Unmarshal(b, &ms); err != nil {
					return "unparseable: " + err.Error(), false
				}
				keptA, keptQ, dropped := 0, 0, map[string]int{}
				for _, m := range ms.Data {
					switch m.Target.Type {
					case "answer":
						keptA++
					case "question":
						keptQ++
					default:
						dropped[m.Target.Type]++
					}
				}
				note := fmt.Sprintf("cards=%d kept=%d(answer=%d question=%d) dropped=%d%s cursor=%v",
					len(ms.Data), keptA+keptQ, keptA, keptQ, len(ms.Data)-keptA-keptQ, census(dropped), ms.Paging.Next != "") + redesignNote(len(ms.Data))
				return note, true
			})
		res(r)
	}

	// 5b. Recommend feed (ticket 08): the home page's algorithmic
	// feed — same v3 session door as moments, same card census plus
	// the article tally kept apart (ticket 09 will keep those cards;
	// the probe already tells them from the true drops).
	if login("recommend") {
		r := p.outcome("recommend",
			tr.Get(ctx, epRecommend, h.Web+"/api/v3/feed/topstory/recommend?desktop=true"),
			func(b []byte) (string, bool) {
				var ms moments
				if err := json.Unmarshal(b, &ms); err != nil {
					return "unparseable: " + err.Error(), false
				}
				keptA, keptQ, articles, dropped := 0, 0, 0, map[string]int{}
				for _, m := range ms.Data {
					switch m.Target.Type {
					case "answer":
						keptA++
					case "question":
						keptQ++
					case "article":
						articles++ // kept apart: ticket 09's rows, not a drop
					default:
						dropped[m.Target.Type]++
					}
				}
				note := fmt.Sprintf("cards=%d kept=%d(answer=%d question=%d) article=%d dropped=%d%s cursor=%v",
					len(ms.Data), keptA+keptQ, keptA, keptQ, articles, len(ms.Data)-keptA-keptQ-articles, census(dropped), ms.Paging.Next != "") + redesignNote(len(ms.Data))
				return note, true
			})
		res(r)
	}

	// The v4 pair, built once — the walk and the header-matrix legs hit
	// the same two addresses. Referer carries the qid; the matrix rows
	// hold everything else.
	var metaURL, feedsURL string
	referer := ""
	if qid != "" {
		referer = "https://www.zhihu.com/question/" + qid
		metaURL = fmt.Sprintf("%s/api/v4/questions/%s?include=%s", h.Web, qid, metaInclude)
		feedsURL = fmt.Sprintf("%s/api/v4/questions/%s/feeds?include=%s&limit=5&order=default&platform=desktop",
			h.Web, qid, feedsInclude)
	}

	// 6-7. Question meta + answers feeds (the v4 family).
	var aid string
	if qid == "" {
		res(EndpointResult{"question-meta", "SKIPPED", verdictSkipped, "SKIPPED-UPSTREAM(hot-list) — or pass -qid"})
		res(EndpointResult{"answers-feeds", "SKIPPED", verdictSkipped, "SKIPPED-UPSTREAM(hot-list) — or pass -qid"})
	} else {
		metaEp, feedsEp := epQuestionMeta, epAnswersFeeds
		metaEp.Referer, feedsEp.Referer = referer, referer

		if login("question-meta") {
			// x-zse-93 REQUIRED here: missing = 403 code 10003.
			r := p.outcome("question-meta", tr.Get(ctx, metaEp, metaURL),
				func(b []byte) (string, bool) {
					var q questionMeta
					if err := json.Unmarshal(b, &q); err != nil {
						return "unparseable: " + err.Error(), false
					}
					return fmt.Sprintf("title=%q answers=%d author=%s detail=%dB",
						q.Title, q.AnswerCount, q.Author.Name, len(q.Detail)), true
				})
			res(r)
		}

		if login("answers-feeds") {
			// x-zse-93 FORBIDDEN here: the cursorless entry rejects it.
			r := p.outcome("answers-feeds", tr.Get(ctx, feedsEp, feedsURL),
				func(b []byte) (string, bool) {
					var fp feedsPage
					if err := json.Unmarshal(b, &fp); err != nil {
						return "unparseable: " + err.Error(), false
					}
					note := fmt.Sprintf("answers=%d", len(fp.Data)) + redesignNote(len(fp.Data))
					for _, a := range fp.Data {
						if a.Target.Type == "answer" && a.Target.ID != "" {
							if aid == "" {
								aid = string(a.Target.ID)
							}
							note += fmt.Sprintf(" first=%q · 赞 %d · %d 条评论",
								a.Target.Author.Name, a.Target.VoteupCount, a.Target.CommentCount)
							break
						}
					}
					note += fmt.Sprintf(" cursor=%v", fp.Paging.Next != "")
					return note, true
				})
			res(r)
		}
	}
	if p.Args.Aid != "" {
		aid = p.Args.Aid
	}

	// 8. Comment root.
	if aid == "" {
		res(EndpointResult{"comments-root", "SKIPPED", verdictSkipped, "SKIPPED-UPSTREAM(answers-feeds) — or pass -aid"})
	} else if login("comments-root") {
		r := p.outcome("comments-root",
			tr.Get(ctx, epCommentsRoot,
				h.Web+"/api/v4/comment_v5/answers/"+aid+"/root_comment?order_by=score&limit=20&offset="),
			func(b []byte) (string, bool) {
				var cs commentsPage
				if err := json.Unmarshal(b, &cs); err != nil {
					return "unparseable: " + err.Error(), false
				}
				note := fmt.Sprintf("roots=%d", len(cs.Data)) + redesignNote(len(cs.Data))
				if len(cs.Data) > 0 {
					r0 := cs.Data[0]
					note += fmt.Sprintf(" first=%q like=%d child_gap=%d",
						r0.Author.Name, r0.LikeCount, r0.ChildCommentCount-int64(len(r0.ChildComments)))
				}
				return note, true
			})
		res(r)
	}

	// 9. Header-matrix self-check: the two negative expectations that
	// make the per-endpoint x-zse-93 rule real. Informational — a
	// MISMATCH is intel (zhihu moved the gate), not a blocked plane.
	if p.CookieHeader == "" || qid == "" {
		res(EndpointResult{"header-matrix", "SKIPPED", verdictSkipped, "needs cookie + qid"})
	} else {
		matrixLeg := func(base Endpoint, url string, wantStatus int, gateMoved string) {
			name := base.Name
			f := tr.Get(ctx, base, url)
			p.save(name, f)
			switch {
			case f.Status == 0:
				res(EndpointResult{name, "ERR", verdictMismatch, f.Err.Error()})
			case f.Status == wantStatus:
				res(EndpointResult{name, fmt.Sprintf("HTTP %d", f.Status), verdictMatch,
					"gate enforced as documented" + errCode(f.Body)})
			case f.Status == http.StatusOK:
				res(EndpointResult{name, "HTTP 200", verdictMismatch, gateMoved})
			default:
				res(EndpointResult{name, fmt.Sprintf("HTTP %d", f.Status), verdictMismatch, "unexpected status"})
			}
		}
		noZse := epQuestionMeta // questions WITHOUT the version header → 403
		noZse.Name, noZse.ZSE93, noZse.Referer = "matrix-questions-nozse", false, referer
		withZse := epAnswersFeeds // cursorless feeds WITH the version header → 403
		withZse.Name, withZse.ZSE93, withZse.Referer = "matrix-feeds-withzse", true, referer
		matrixLeg(noZse, metaURL, http.StatusForbidden,
			"GATE MOVED: question meta answers without x-zse-93")
		matrixLeg(withZse, feedsURL, http.StatusForbidden,
			"GATE MOVED: feeds entry tolerates x-zse-93 now")
	}

	// Verdict recap + the ladder, so the report closes itself.
	green, blocked, skipped := 0, 0, 0
	for _, r := range rs {
		switch r.Verdict {
		case verdictGreen:
			green++
		case verdictBlocked:
			blocked++
		case verdictSkipped:
			skipped++
		}
	}
	fmt.Fprintf(w, "—\n%d GREEN · %d BLOCKED · %d SKIPPED", green, blocked, skipped)
	if blocked == 0 {
		fmt.Fprintf(w, " — Go direct data plane holds")
	} else {
		fmt.Fprintf(w, "\nladder: ① tweak headers/UA → ② chromedp one-shot login channel → ③ anonymous-only (hot+daily)")
	}
	fmt.Fprintln(w)
	if p.OutDir != "" {
		fmt.Fprintf(w, "samples: %s\n", p.OutDir)
	}
	return rs
}

// flag53 decorates a qid with its precision flag for the report.
func flag53(qid string) string {
	if qid == "" {
		return "<no-qid-in-url>"
	}
	if OverJSMaxInt(qid) {
		return qid + "(>2^53)"
	}
	return qid
}

// redesignNote is the spec's tripwire — 「0 条解析即报站点可能改版」:
// zhihu's lists are never legitimately empty, so a well-formed but
// 0-row parse means the shape moved under us. Reachability is real,
// so the verdict stays GREEN; the note carries the alarm.
func redesignNote(rows int) string {
	if rows == 0 {
		return " — 0 rows: 站点可能改版?"
	}
	return ""
}

// census renders a dropped-card tally like " (article=1,pin=1)".
func census(dropped map[string]int) string {
	if len(dropped) == 0 {
		return ""
	}
	parts := make([]string, 0, len(dropped))
	for k, v := range dropped {
		parts = append(parts, fmt.Sprintf("%s=%d", k, v))
	}
	sort.Strings(parts)
	return " (" + strings.Join(parts, ",") + ")"
}
