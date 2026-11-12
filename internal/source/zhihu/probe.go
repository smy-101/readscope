package zhihu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
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
// body under OutDir, and self-check the x-zse-93 header matrix. It
// never prints cookie values.
type Probe struct {
	Hosts        Hosts
	CookieHeader string // "" = walk the anonymous endpoints only
	OutDir       string // "" = don't save samples
	Client       *http.Client
	Args         ProbeArgs
}

const (
	maxBodyBytes = 10 << 20
	probeTimeout = 30 * time.Second

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

// fetch is one raw outcome; body rides along even on non-200 so the
// sample file keeps the error document (zhihu's error JSON carries
// the code — 10003 and friends).
type fetch struct {
	status int
	body   []byte
	err    error
}

// get performs one probe GET with the shared request shape: desktop
// Chrome UA, x-requested-with: fetch, Accept headers a real fetch
// sends, per-endpoint Referer, optional x-zse-93 and Cookie. The body
// is capped and saved under <OutDir>/<name>.json.
func (p *Probe) get(ctx context.Context, name, url, referer string, zse93, cookie bool) fetch {
	f := fetch{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		f.err = err
		return f
	}
	req.Header.Set("User-Agent", DesktopUA)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("x-requested-with", "fetch")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if zse93 {
		req.Header.Set("x-zse-93", XZse93)
	}
	if cookie && p.CookieHeader != "" {
		req.Header.Set("Cookie", p.CookieHeader)
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: probeTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		f.err = err
		return f
	}
	defer resp.Body.Close()
	f.status = resp.StatusCode
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		f.err = err
		return f
	}
	f.body = body
	p.save(name, f)
	return f
}

// save writes one raw sample (best effort — a read-only dir must not
// kill the walk; the report line notes where samples were heading).
func (p *Probe) save(name string, f fetch) {
	if p.OutDir == "" || f.err != nil {
		return
	}
	if err := os.MkdirAll(p.OutDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(p.OutDir, name+".json"), f.body, 0o644)
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
// column). IDs decode via json.Number to stay exact; production ids
// come from target.url (see QIDFromURL).
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

// moments is the follow feed: heterogeneous cards, of which only
// answer and question cards ever reach a list (question cards);
// everything else the probe censuses as dropped.
type moments struct {
	Data []struct {
		Target struct {
			Type     string                `json:"type"`
			ID       json.Number           `json:"id"`
			Title    string                `json:"title"`
			Author   struct{ Name string } `json:"author"`
			Question struct {
				ID    json.Number `json:"id"`
				Title string      `json:"title"`
			} `json:"question"`
		} `json:"target"`
	} `json:"data"`
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
// floor-head triple (author, voteup_count, comment_count) plus the
// full-content HTML; paging.next is the cursor.
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

// commentsRoot is the first page of an answer's comments: roots carry
// child_comment_count and a 1-2 item hot-children preview; the gap
// between them is what ticket 06's completion walk fills.
type commentsRoot struct {
	Data []struct {
		ID                json.Number           `json:"id"`
		Author            struct{ Name string } `json:"author"`
		LikeCount         int64                 `json:"like_count"`
		Content           string                `json:"content"`
		ChildCommentCount int64                 `json:"child_comment_count"`
		ChildComments     []struct {
			ID json.Number `json:"id"`
		} `json:"child_comments"`
	} `json:"data"`
	Paging struct {
		Next  string `json:"next"`
		IsEnd bool   `json:"is_end"`
	} `json:"paging"`
}

// Run walks the contract, streaming one line per endpoint to w and
// returning every result (caller turns BLOCKED into exit 1).
func (p *Probe) Run(ctx context.Context, w io.Writer) []EndpointResult {
	var rs []EndpointResult
	res := func(r EndpointResult) {
		rs = append(rs, r)
		fmt.Fprintf(w, "%-18s %-9s %-8s %s\n", r.Name, r.Status, r.Verdict, r.Note)
	}

	h := p.Hosts
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
	// an error). Defined once, used by every session-gated endpoint.
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
		f := p.get(ctx, "hot-list", h.API+"/topstory/hot-list?limit=10", "https://www.zhihu.com/hot", false, true)
		switch {
		case f.err != nil:
			res(EndpointResult{"hot-list", "ERR", verdictBlocked, f.err.Error()})
		case f.status != http.StatusOK:
			res(EndpointResult{"hot-list", fmt.Sprintf("HTTP %d", f.status), verdictBlocked,
				fmt.Sprintf("blocked%s", errCode(f.body))})
		default:
			var hot hotList
			verdict, note := verdictBlocked, "unparseable"
			if err := json.Unmarshal(f.body, &hot); err == nil {
				note = fmt.Sprintf("rows=%d", len(hot.Data))
				if len(hot.Data) > 0 {
					q := QIDFromURL(hot.Data[0].Target.URL)
					note += fmt.Sprintf(" qid1=%s heat[0]=%q", flag53(q), hot.Data[0].DetailText)
					if q != "" {
						qid = q
					}
				}
				verdict = verdictGreen
			} else {
				note += ": " + err.Error()
			}
			res(EndpointResult{"hot-list", "HTTP 200", verdict, note})
		}
	}
	if login("hot-www-door") {
		f := p.get(ctx, "hot-www-door", h.Web+"/api/v3/feed/topstory/hot-lists/total?limit=50",
			"https://www.zhihu.com/hot", false, true)
		switch {
		case f.err != nil:
			res(EndpointResult{"hot-www-door", "ERR", verdictBlocked, f.err.Error()})
		case f.status != http.StatusOK:
			res(EndpointResult{"hot-www-door", fmt.Sprintf("HTTP %d", f.status), verdictBlocked,
				fmt.Sprintf("blocked%s", errCode(f.body))})
		default:
			var hot hotList
			verdict, note := verdictBlocked, "unparseable"
			if err := json.Unmarshal(f.body, &hot); err == nil {
				note = fmt.Sprintf("rows=%d", len(hot.Data))
				if len(hot.Data) > 0 {
					note += fmt.Sprintf(" qid1=%s", flag53(QIDFromURL(hot.Data[0].Target.URL)))
				}
				verdict = verdictGreen
			} else {
				note += ": " + err.Error()
			}
			res(EndpointResult{"hot-www-door", "HTTP 200", verdict, note})
		}
	}
	if p.Args.QID != "" {
		qid = p.Args.QID
	}

	// 3. Daily list (anonymous); story id chains from here.
	var storyID string
	{
		f := p.get(ctx, "daily-latest", h.Daily+"/api/4/news/latest", "https://daily.zhihu.com/", false, false)
		if f.err != nil {
			res(EndpointResult{"daily-latest", "ERR", verdictBlocked, f.err.Error()})
		} else if f.status != http.StatusOK {
			res(EndpointResult{"daily-latest", fmt.Sprintf("HTTP %d", f.status), verdictBlocked,
				"blocked" + errCode(f.body)})
		} else {
			var latest dailyLatest
			note := "unparseable"
			verdict := verdictBlocked
			if err := json.Unmarshal(f.body, &latest); err == nil {
				note = fmt.Sprintf("stories=%d", len(latest.Stories))
				if len(latest.Stories) > 0 {
					storyID = string(latest.Stories[0].ID)
					note += fmt.Sprintf(" first=%q hint=%q", latest.Stories[0].Title, latest.Stories[0].Hint)
				}
				verdict = verdictGreen
			} else {
				note += ": " + err.Error()
			}
			res(EndpointResult{"daily-latest", "HTTP 200", verdict, note})
		}
	}
	if p.Args.StoryID != "" {
		storyID = p.Args.StoryID
	}

	// 4. Daily story body (the offline snapshot's source).
	if storyID == "" {
		res(EndpointResult{"daily-story", "SKIPPED", verdictSkipped, "SKIPPED-UPSTREAM(daily-latest)"})
	} else {
		f := p.get(ctx, "daily-story", h.Daily+"/api/4/story/"+storyID, "https://daily.zhihu.com/", false, false)
		switch {
		case f.err != nil:
			res(EndpointResult{"daily-story", "ERR", verdictBlocked, f.err.Error()})
		case f.status != http.StatusOK:
			res(EndpointResult{"daily-story", fmt.Sprintf("HTTP %d", f.status), verdictBlocked,
				"blocked" + errCode(f.body)})
		default:
			var story dailyStory
			verdict := verdictBlocked
			note := "unparseable"
			if err := json.Unmarshal(f.body, &story); err == nil {
				note = fmt.Sprintf("body=%dB html=%v", len(story.Body), strings.Contains(story.Body, "<"))
				if story.Body != "" {
					verdict = verdictGreen
				}
			} else {
				note += ": " + err.Error()
			}
			res(EndpointResult{"daily-story", "HTTP 200", verdict, note})
		}
	}

	// 5. Follow feed: card census — question cards kept, the rest
	// counted by type and dropped.
	{
		if login("moments") {
			f := p.get(ctx, "moments", h.Web+"/api/v3/moments?limit=10&desktop=true",
				"https://www.zhihu.com/follow", false, true)
			switch {
			case f.err != nil:
				res(EndpointResult{"moments", "ERR", verdictBlocked, f.err.Error()})
			case f.status == http.StatusUnauthorized || f.status == http.StatusForbidden:
				res(EndpointResult{"moments", fmt.Sprintf("HTTP %d", f.status), verdictBlocked,
					fmt.Sprintf("blocked (403 = risk control or stale cookie; 401 = stale)%s", errCode(f.body))})
			case f.status != http.StatusOK:
				res(EndpointResult{"moments", fmt.Sprintf("HTTP %d", f.status), verdictBlocked, "blocked" + errCode(f.body)})
			default:
				var ms moments
				verdict := verdictBlocked
				note := "unparseable"
				if err := json.Unmarshal(f.body, &ms); err == nil {
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
					note = fmt.Sprintf("cards=%d kept=%d(answer=%d question=%d) dropped=%d%s cursor=%v",
						len(ms.Data), keptA+keptQ, keptA, keptQ, len(ms.Data)-keptA-keptQ, census(dropped), ms.Paging.Next != "")
					verdict = verdictGreen
				} else {
					note += ": " + err.Error()
				}
				res(EndpointResult{"moments", "HTTP 200", verdict, note})
			}
		}
	}

	// 6-7. Question meta + answers feeds (the v4 family).
	var aid string
	if qid == "" {
		res(EndpointResult{"question-meta", "SKIPPED", verdictSkipped, "SKIPPED-UPSTREAM(hot-list) — or pass -qid"})
		res(EndpointResult{"answers-feeds", "SKIPPED", verdictSkipped, "SKIPPED-UPSTREAM(hot-list) — or pass -qid"})
	} else {
		metaURL := fmt.Sprintf("%s/api/v4/questions/%s?include=%s", h.Web, qid, metaInclude)
		feedsURL := fmt.Sprintf("%s/api/v4/questions/%s/feeds?include=%s&limit=5&order=default&platform=desktop",
			h.Web, qid, feedsInclude)
		referer := "https://www.zhihu.com/question/" + qid

		if login("question-meta") {
			// x-zse-93 REQUIRED here: missing = 403 code 10003.
			f := p.get(ctx, "question-meta", metaURL, referer, true, true)
			switch {
			case f.err != nil:
				res(EndpointResult{"question-meta", "ERR", verdictBlocked, f.err.Error()})
			case f.status != http.StatusOK:
				res(EndpointResult{"question-meta", fmt.Sprintf("HTTP %d", f.status), verdictBlocked,
					fmt.Sprintf("blocked%s (403 = gate or stale cookie)", errCode(f.body))})
			default:
				var q questionMeta
				verdict := verdictBlocked
				note := "unparseable"
				if err := json.Unmarshal(f.body, &q); err == nil {
					note = fmt.Sprintf("title=%q answers=%d author=%s detail=%dB",
						q.Title, q.AnswerCount, q.Author.Name, len(q.Detail))
					verdict = verdictGreen
				} else {
					note += ": " + err.Error()
				}
				res(EndpointResult{"question-meta", "HTTP 200", verdict, note})
			}
		}

		if login("answers-feeds") {
			// x-zse-93 FORBIDDEN here: the cursorless entry rejects it.
			f := p.get(ctx, "answers-feeds", feedsURL, referer, false, true)
			switch {
			case f.err != nil:
				res(EndpointResult{"answers-feeds", "ERR", verdictBlocked, f.err.Error()})
			case f.status != http.StatusOK:
				res(EndpointResult{"answers-feeds", fmt.Sprintf("HTTP %d", f.status), verdictBlocked,
					fmt.Sprintf("blocked%s", errCode(f.body))})
			default:
				var fp feedsPage
				verdict := verdictBlocked
				note := "unparseable"
				if err := json.Unmarshal(f.body, &fp); err == nil {
					note = fmt.Sprintf("answers=%d", len(fp.Data))
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
					verdict = verdictGreen
				} else {
					note += ": " + err.Error()
				}
				res(EndpointResult{"answers-feeds", "HTTP 200", verdict, note})
			}
		}
	}
	if p.Args.Aid != "" {
		aid = p.Args.Aid
	}

	// 8. Comment root.
	if aid == "" {
		res(EndpointResult{"comments-root", "SKIPPED", verdictSkipped, "SKIPPED-UPSTREAM(answers-feeds) — or pass -aid"})
	} else if login("comments-root") {
		f := p.get(ctx, "comments-root",
			h.Web+"/api/v4/comment_v5/answers/"+aid+"/root_comment?order_by=score&limit=20&offset=",
			"https://www.zhihu.com/", false, true)
		switch {
		case f.err != nil:
			res(EndpointResult{"comments-root", "ERR", verdictBlocked, f.err.Error()})
		case f.status != http.StatusOK:
			res(EndpointResult{"comments-root", fmt.Sprintf("HTTP %d", f.status), verdictBlocked,
				fmt.Sprintf("blocked%s", errCode(f.body))})
		default:
			var cs commentsRoot
			verdict := verdictBlocked
			note := "unparseable"
			if err := json.Unmarshal(f.body, &cs); err == nil {
				note = fmt.Sprintf("roots=%d", len(cs.Data))
				if len(cs.Data) > 0 {
					r0 := cs.Data[0]
					note += fmt.Sprintf(" first=%q like=%d child_gap=%d",
						r0.Author.Name, r0.LikeCount, r0.ChildCommentCount-int64(len(r0.ChildComments)))
				}
				verdict = verdictGreen
			} else {
				note += ": " + err.Error()
			}
			res(EndpointResult{"comments-root", "HTTP 200", verdict, note})
		}
	}

	// 9. Header-matrix self-check: the two negative expectations that
	// make the per-endpoint x-zse-93 rule real. Informational — a
	// MISMATCH is intel (zhihu moved the gate), not a blocked plane.
	if p.CookieHeader == "" || qid == "" {
		res(EndpointResult{"header-matrix", "SKIPPED", verdictSkipped, "needs cookie + qid"})
	} else {
		metaURL := fmt.Sprintf("%s/api/v4/questions/%s?include=%s", h.Web, qid, metaInclude)
		feedsURL := fmt.Sprintf("%s/api/v4/questions/%s/feeds?include=%s&limit=5&order=default&platform=desktop",
			h.Web, qid, feedsInclude)

		f1 := p.get(ctx, "matrix-questions-nozse", metaURL, "https://www.zhihu.com/question/"+qid, false, true)
		switch {
		case f1.err != nil:
			res(EndpointResult{"matrix-questions-nozse", "ERR", verdictMismatch, f1.err.Error()})
		case f1.status == http.StatusForbidden:
			res(EndpointResult{"matrix-questions-nozse", "HTTP 403", verdictMatch,
				"version gate enforced as documented" + errCode(f1.body)})
		case f1.status == http.StatusOK:
			res(EndpointResult{"matrix-questions-nozse", "HTTP 200", verdictMismatch,
				"GATE MOVED: question meta answers without x-zse-93"})
		default:
			res(EndpointResult{"matrix-questions-nozse", fmt.Sprintf("HTTP %d", f1.status), verdictMismatch,
				"unexpected status"})
		}

		f2 := p.get(ctx, "matrix-feeds-withzse", feedsURL, "https://www.zhihu.com/question/"+qid, true, true)
		switch {
		case f2.err != nil:
			res(EndpointResult{"matrix-feeds-withzse", "ERR", verdictMismatch, f2.err.Error()})
		case f2.status == http.StatusForbidden:
			res(EndpointResult{"matrix-feeds-withzse", "HTTP 403", verdictMatch,
				"cursorless feeds entry rejects x-zse-93 as documented"})
		case f2.status == http.StatusOK:
			res(EndpointResult{"matrix-feeds-withzse", "HTTP 200", verdictMismatch,
				"GATE MOVED: feeds entry tolerates x-zse-93 now"})
		default:
			res(EndpointResult{"matrix-feeds-withzse", fmt.Sprintf("HTTP %d", f2.status), verdictMismatch,
				"unexpected status"})
		}
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

// census renders a dropped-card tally like " (article=1,pin=1)".
func census(dropped map[string]int) string {
	if len(dropped) == 0 {
		return ""
	}
	var parts []string
	for k, v := range dropped {
		parts = append(parts, fmt.Sprintf("%s=%d", k, v))
	}
	// Deterministic order for golden-ish report comparison.
	for i := 0; i < len(parts); i++ {
		for j := i + 1; j < len(parts); j++ {
			if parts[j] < parts[i] {
				parts[i], parts[j] = parts[j], parts[i]
			}
		}
	}
	return " (" + strings.Join(parts, ",") + ")"
}
