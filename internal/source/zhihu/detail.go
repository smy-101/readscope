package zhihu

// detail.go — the zhihu half of DetailSource and DetailPager (ticket
// 06): the question's three detail layers (GLOSSARY: 三层详情 —
// question → answer → comment) on the measured v4 data plane. One
// open is TWO parallel requests — the question meta (x-zse-93 on) and
// the first answers page (x-zse-93 off, the cursorless entry's rule)
// — mapped into Content: the cleaned description (Body, persisted by
// the refresh layer into the body snapshot), the meta triple
// (Question, cached onto the detail_fetch row), the answer floors
// (Comments, one answer per top-level "comment" with the floor-head
// triple: author · 赞 N · N 条评论) and the feeds cursor (Next).
// Answer pages after the first ride paging.next verbatim
// (FetchFloorsPage); an answer's comments page at offset N is the
// root_comment page plus each root's child-gap completion walk
// (FetchAnswerComments). Everything HTML rides CleanHTML first — the
// render pipeline never learns zhihu exists.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/smy-101/readscope/internal/core"
)

// The comment-layer walk's measured constants (FeedScope 2026-11):
// child pages serve 50 and a hot root rarely needs more than one
// page, but the walk keeps room to page; roots complete concurrently
// or a popular answer's first page takes 20s+ serial. The bounds are
// deliberate restraint — account risk control outranks completeness.
const (
	childPageSize  = 50
	maxChildPages  = 6 // one root's completion walk: hard stop
	maxRootWalkers = 3 // roots completing concurrently
)

// commentNode is one comment_v5 node. root_comment pages serve their
// roots in this shape (each with a 1–2 item hot-child preview), and
// the child_comment completion pages serve the missing children in
// it; nesting (child_comments inside child_comments) is walked when
// the payload carries it, so the flattened tree keeps real parent
// links — the 评论层's @parent prefix needs them.
type commentNode struct {
	ID                json.Number           `json:"id"`
	Author            struct{ Name string } `json:"author"`
	LikeCount         int64                 `json:"like_count"`
	Content           string                `json:"content"`
	CreatedTime       int64                 `json:"created_time"`
	ChildCommentCount int64                 `json:"child_comment_count"`
	ChildComments     []commentNode         `json:"child_comments"`
	IsDelete          bool                  `json:"is_delete"`
}

// commentsPage is one root_comment (or child_comment) page.
type commentsPage struct {
	Data   []commentNode `json:"data"`
	Paging struct {
		Next  string `json:"next"`
		IsEnd bool   `json:"is_end"`
	} `json:"paging"`
}

// qidOf extracts the question id from a "zh:question/<qid>" item id.
func qidOf(itemID string) (string, bool) {
	return strings.CutPrefix(itemID, itemIDPrefix)
}

// questionMetaURL is the v4 meta door: asker, counts and the
// description behind one include string (probe.go's measured const).
func (s *Source) questionMetaURL(qid string) string {
	return fmt.Sprintf("%s/api/v4/questions/%s?include=%s", s.hosts.Web, qid, metaInclude)
}

// feedsURL is the answers feed's cursorless entry: exactly the lean
// include the entry accepts (the full include without a cursor 403s
// — measured, "勿手改"), first 5 answers, default order.
func (s *Source) feedsURL(qid string) string {
	return fmt.Sprintf("%s/api/v4/questions/%s/feeds?include=%s&limit=5&order=default&platform=desktop",
		s.hosts.Web, qid, feedsInclude)
}

// questionReferer clones the endpoint's matrix row with the qid the
// page itself would carry — the same clone-and-flip the probe does.
func questionReferer(ep Endpoint, qid string) Endpoint {
	ep.Referer = "https://www.zhihu.com/question/" + qid
	return ep
}

// DetailOnDemand implements source.DetailSource: question cards
// alone — the three detail layers are a question's (GLOSSARY: 问题卡
// vs 知乎日报); a daily story is a lazy body, and probing it into the
// detail machinery would only fetch a named refusal.
func (s *Source) DetailOnDemand(it core.Item) bool {
	_, ok := qidOf(it.ID)
	return ok
}

// Detail implements source.DetailSource: the two-request open. Both
// legs run concurrently (independent doors — serializing wastes a
// round trip); the meta leg's error wins a tie so a 404 question
// reports as gone even when the feeds leg also failed. Floors are
// answer cards only — a stray card is not a floor and not an error.
func (s *Source) Detail(ctx context.Context, it core.Item) (core.Content, error) {
	qid, ok := qidOf(it.ID)
	if !ok {
		return core.Content{}, fmt.Errorf("detail: %q is not a zhihu question id", it.ID)
	}

	var (
		wg                sync.WaitGroup
		metaErr, feedsErr error
		meta              questionMeta
		feeds             feedsPage
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		res := s.tr.Get(ctx, questionReferer(epQuestionMeta, qid), s.questionMetaURL(qid))
		if res.Err != nil {
			metaErr = fmt.Errorf("fetch question %s: %w", qid, res.Err)
			return
		}
		if err := json.Unmarshal(res.Body, &meta); err != nil {
			metaErr = fmt.Errorf("parse question %s: not zhihu JSON: %w", qid, err)
		}
	}()
	go func() {
		defer wg.Done()
		res := s.tr.Get(ctx, questionReferer(epAnswersFeeds, qid), s.feedsURL(qid))
		if res.Err != nil {
			feedsErr = fmt.Errorf("fetch answers %s: %w", qid, res.Err)
			return
		}
		if err := json.Unmarshal(res.Body, &feeds); err != nil {
			feedsErr = fmt.Errorf("parse answers %s: not zhihu JSON: %w", qid, err)
		}
	}()
	wg.Wait()
	if metaErr != nil {
		return core.Content{}, metaErr
	}
	if feedsErr != nil {
		return core.Content{}, feedsErr
	}

	return core.Content{
		Body:     CleanHTML(meta.Detail),
		Question: &core.QuestionInfo{Author: meta.Author.Name, AnswerCount: meta.AnswerCount, VisitCount: meta.VisitCount},
		Comments: answerFloors(feeds),
		Next:     feeds.Paging.Next,
	}, nil
}

// answerFloors maps one feeds page onto answer floors: author, the
// cleaned full text (it arrived with the floor — the 回答层 renders
// it, no second request), 赞 N (Score) and N 条评论 (NumChildren) —
// the floor-head triple, with zero-valued segments nil so the header
// drops them instead of printing 赞 0 (站方形态: 无赞不占位).
func answerFloors(page feedsPage) []core.Comment {
	out := make([]core.Comment, 0, len(page.Data))
	for _, a := range page.Data {
		t := a.Target
		if t.Type != "answer" {
			continue // a stray card mixed into the feed: not a floor, not an error
		}
		aid := t.ID.String()
		if aid == "" || aid == "0" {
			continue // nothing addressable; skip, never crash
		}
		cm := core.Comment{
			ID:          aid,
			Author:      t.Author.Name,
			Text:        CleanHTML(t.Content),
			NumChildren: nonzeroPtr(t.CommentCount),
		}
		if v := t.VoteupCount; v != 0 {
			cm.Score = nonzeroPtr(v)
		}
		if ts := t.CreatedTime; ts != 0 {
			tm := time.Unix(ts, 0).UTC()
			cm.CreatedAt = &tm
		}
		out = append(out, cm)
	}
	return out
}

// nonzeroPtr boxes n when it carries information, nil for 0 — the
// pointer keeps "absent" distinct from a real zero.
func nonzeroPtr(n int64) *int {
	if n == 0 {
		return nil
	}
	v := int(n)
	return &v
}

// CommentURL implements source.DetailSource: the question's www page
// for a question item id. Bare comment ids return "" — an answer or
// comment id cannot be paired with its question alone, and the
// reader's floor/reply targets route through AnswerURL instead,
// which carries the item.
func (s *Source) CommentURL(id string) string {
	if qid, ok := qidOf(id); ok {
		return questionURL(qid)
	}
	return ""
}

// AnswerURL implements source.DetailPager: the question page anchored
// at one answer — Ctrl+O/Ctrl+Y's target on the 回答层 and 评论层.
func (s *Source) AnswerURL(it core.Item, answerID string) string {
	qid, ok := qidOf(it.ID)
	if !ok || answerID == "" {
		return ""
	}
	return questionURL(qid) + "/answer/" + answerID
}

// FetchFloorsPage implements source.DetailPager: one answers page —
// the cursorless entry when cursor is "", else the page the server
// addressed (paging.next rides verbatim). The floors are the same
// shape Detail's Comments carry; the dedup that keeps a re-served
// answer from doubling a row lives in the refresh merge.
func (s *Source) FetchFloorsPage(ctx context.Context, it core.Item, cursor string) ([]core.Comment, string, error) {
	qid, ok := qidOf(it.ID)
	if !ok {
		return nil, "", fmt.Errorf("fetch floors: %q is not a zhihu question id", it.ID)
	}
	url := s.feedsURL(qid)
	if cursor != "" {
		url = cursor
	}
	res := s.tr.Get(ctx, questionReferer(epAnswersFeeds, qid), url)
	if res.Err != nil {
		return nil, "", fmt.Errorf("fetch answers %s: %w", qid, res.Err)
	}
	var page feedsPage
	if err := json.Unmarshal(res.Body, &page); err != nil {
		return nil, "", fmt.Errorf("parse answers %s: not zhihu JSON: %w", qid, err)
	}
	return answerFloors(page), page.Paging.Next, nil
}

// FetchAnswerComments implements source.DetailPager: one answer's
// comments at root offset — the roots served there, each root's
// child gap completed by the child_comment walk, tree order with the
// roots' ParentID set to the answer id (the cache stores the whole
// question as one tree: floors and their comments together).
// nextOffset is the paging.next URL's offset, -1 at the end.
func (s *Source) FetchAnswerComments(ctx context.Context, it core.Item, answerID string, offset int) ([]core.Comment, int, error) {
	if _, ok := qidOf(it.ID); !ok {
		return nil, -1, fmt.Errorf("fetch comments: %q is not a zhihu question id", it.ID)
	}
	if offset < 0 {
		offset = 0
	}
	res := s.tr.Get(ctx, epCommentsRoot,
		fmt.Sprintf("%s/api/v4/comment_v5/answers/%s/root_comment?order_by=score&limit=20&offset=%d",
			s.hosts.Web, answerID, offset))
	if res.Err != nil {
		return nil, -1, fmt.Errorf("fetch comments %s: %w", answerID, res.Err)
	}
	var page commentsPage
	if err := json.Unmarshal(res.Body, &page); err != nil {
		return nil, -1, fmt.Errorf("parse comments %s: not zhihu JSON: %w", answerID, err)
	}

	// Roots worth keeping, with their gaps completed. The walk is
	// bounded and failure-soft: a root whose completion fails keeps
	// the hot children the page already carried (a partial comment
	// tree beats a failed page).
	roots := make([]commentNode, 0, len(page.Data))
	for _, n := range page.Data {
		if n.IsDelete {
			continue
		}
		roots = append(roots, n)
	}
	s.completeRoots(ctx, roots)

	out := make([]core.Comment, 0, len(roots))
	for _, n := range roots {
		emitComments(n, answerID, &out)
	}

	next := -1
	if page.Paging.Next != "" && !page.Paging.IsEnd {
		if o, err := offsetFromURL(page.Paging.Next); err == nil {
			next = o
		}
	}
	return out, next, nil
}

// completeRoots completes every root's child gap in place — up to
// maxRootWalkers roots walking concurrently, each root's pages
// sequential, the whole thing bounded by maxChildPages per root.
// Roots with no gap skip the walk (the hot preview already carried
// everything, or there are no children at all).
func (s *Source) completeRoots(ctx context.Context, roots []commentNode) {
	pending := make([]*commentNode, 0, len(roots))
	for i := range roots {
		if childGap(roots[i]) > 0 {
			pending = append(pending, &roots[i])
		}
	}
	if len(pending) == 0 {
		return
	}
	sem := make(chan struct{}, maxRootWalkers)
	var wg sync.WaitGroup
	for _, r := range pending {
		wg.Add(1)
		go func(r *commentNode) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r.ChildComments = append(r.ChildComments, s.fetchMissingChildren(ctx, r)...)
		}(r)
	}
	wg.Wait()
}

// childGap is the pure half of the completion decision: how many
// children the page did NOT carry. child_comment_count and the
// hot-preview slice are the server's own accurate record (measured
// 2026-11: can_unfold is always false in root_comment responses, and
// child_comment_next_offset only covers the "some shown, more exist"
// shape) — the gap is their arithmetic, nothing else.
func childGap(n commentNode) int64 {
	if n.IsDelete {
		return 0 // a deleted root's count is not our debt
	}
	return n.ChildCommentCount - int64(len(n.ChildComments))
}

// fetchMissingChildren walks one root's child_comment pages and
// returns the children the hot preview did not already carry: the
// first page at the root's own offset (its child_comment_next_offset
// when the page carried one, "" from the start), is_delete rows and
// already-seen ids dropped (the pages happily re-serve the hot
// preview — the dedup is ours), the walk ending at the server's is_end,
// a missing next cursor, or the maxChildPages bound. A failed page
// ends the walk softly: what arrived so far is returned, and the
// caller keeps the preview children regardless.
func (s *Source) fetchMissingChildren(ctx context.Context, root *commentNode) []commentNode {
	seen := map[string]bool{}
	for _, k := range root.ChildComments {
		seen[k.ID.String()] = true
	}
	var out []commentNode
	nextURL := fmt.Sprintf("%s/api/v4/comment_v5/comment/%s/child_comment?limit=%d&offset=",
		s.hosts.Web, root.ID.String(), childPageSize)
	for range maxChildPages {
		res := s.tr.Get(ctx, epChildComments, nextURL)
		if res.Err != nil {
			return out // soft stop: a partial tree beats a failed page
		}
		var page commentsPage
		if err := json.Unmarshal(res.Body, &page); err != nil {
			return out
		}
		out = append(out, dedupChildren(page.Data, seen)...)
		if page.Paging.Next == "" || page.Paging.IsEnd {
			return out
		}
		nextURL = page.Paging.Next
	}
	return out
}

// dedupChildren is the pure filter between one child page and the
// root's growing child slice: deleted rows drop, ids already carried
// (the hot preview, or an earlier page of this walk) drop, and every
// survivor is marked seen so the next page filters against it too.
func dedupChildren(nodes []commentNode, seen map[string]bool) []commentNode {
	out := make([]commentNode, 0, len(nodes))
	for _, n := range nodes {
		if n.IsDelete {
			continue
		}
		id := n.ID.String()
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, n)
	}
	return out
}

// emitComments flattens one root and its (possibly nested) children
// into tree order: the root first, then its subtree, parents strictly
// before children — the seq order the comments cache requires. Plain
// roots pass through verbatim (root_comment serves plain text);
// anything with a tag in it is zhihu HTML and rides CleanHTML, so
// video-box cards and lazy imgs never reach the pipeline raw.
func emitComments(n commentNode, parentID string, out *[]core.Comment) {
	cm := core.Comment{
		ID:       n.ID.String(),
		ParentID: parentID,
		Author:   n.Author.Name,
		Text:     cleanIfHTML(n.Content),
	}
	if v := n.LikeCount; v != 0 {
		cm.Score = nonzeroPtr(v)
	}
	if ts := n.CreatedTime; ts != 0 {
		tm := time.Unix(ts, 0).UTC()
		cm.CreatedAt = &tm
	}
	*out = append(*out, cm)
	for _, k := range n.ChildComments {
		if k.IsDelete {
			continue
		}
		emitComments(k, cm.ID, out)
	}
}

// cleanIfHTML cleans zhihu's HTML fragments and passes plain text
// through — root_comment serves roots as plain text, child_comment
// pages carry <p> markup (measured); CleanHTML on plain text is
// harmless but also pointless, and this way the cache holds exactly
// what the API sent for the plain case.
func cleanIfHTML(s string) string {
	if strings.Contains(s, "<") {
		return CleanHTML(s)
	}
	return s
}

// offsetFromURL digs the root_comment walk's position out of a
// paging.next URL (…root_comment?…&offset=N) — the offset IS the
// cursor; there is nothing else to carry.
func offsetFromURL(raw string) (int, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(u.Query().Get("offset"))
}
