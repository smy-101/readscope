package heybox

// detail.go — the post detail's two layers (ticket 03): the post
// body (plain text blocks, article HTML, image/video degradations)
// and the comment tree (root comments as floors, 楼中楼 flattened
// underneath each). One open rides TWO doors, both smoke-verified:
// /bbs/app/link/tree (hex link_id — the decimal spelling is a
// captcha honeypot, finding 2 of the smoke) serves the post and its
// first page of floors with hot sub-comments inline, and
// /bbs/app/comment/sub/comments (root_comment_id + lastval cursor)
// serves one floor's 楼中楼 pages. The DetailPager shape is zhihu's:
// floors page by offset (total_page bounds the walk), subs page by
// lastval, and a floor's child_num gap completes eagerly during the
// fetch that carried the floor — the reader never lands on a 楼中楼
// that shows fewer rows than its header promises (缺口计算 + 去重 +
// 游标终止, the zhihu completion walk translated to this host's
// cursor).

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
)

// The detail family's doors and page shapes, measured legs of the
// smoke matrix (P11 tree, P12/P13 subs). floorPageSize is the ask
// the smoke ran on both tree legs; subPageSize the sub walk's. The
// walk bound is deliberate restraint (zhihu's maxChildPages, same
// number, same job): a runaway root stops at six pages, still
// reachable page by page through the reply layer's own Ctrl+N.
const (
	treePath      = "/bbs/app/link/tree"
	subPath       = "/bbs/app/comment/sub/comments"
	floorPageSize = 20
	subPageSize   = 20
	maxSubPages   = 6
)

// The compile-time pins for the detail extensions this file adds:
// callers probe with type assertions, and these guarantee the
// assertions can't silently rot.
var _ source.DetailSource = (*Source)(nil)
var _ source.DetailPager = (*Source)(nil)

// hexIDOfItem peels the hex link_id off a "hb:link/<hexid>" row id —
// every detail door keys on it (the list layer validated the hex at
// card time; no re-validation needed here).
func hexIDOfItem(itemID string) (string, bool) {
	return strings.CutPrefix(itemID, itemIDPrefix)
}

// treeResult is link/tree's result payload: the post itself (link),
// the floor groups (comments), and the floors paging bound
// total_page. The page's other two counters stay deliberately
// unparsed: total_floor_num disagreed with the groups the smoke's
// own page served, and has_more_floors carries nothing total_page
// plus the empty-page end guard do not (P11: has_more_floors 0 with
// total_page 1 — one signal, two spellings); one bound, one rule.
type treeResult struct {
	Link      treeLink       `json:"link"`
	Comments  []commentGroup `json:"comments"`
	TotalPage int            `json:"total_page"`
}

// treeLink is the post half of the tree payload. Text carries the
// body (plain text, a JSON text-block array, or article HTML — the
// AaronZ345 contract); imgs is deliberately RawMessage because its
// shape drifted between host versions (URL strings vs objects, the
// research's one flagged compatibility risk) and a malformed image
// list must never fail the whole tree parse.
type treeLink struct {
	Title     string          `json:"title"`
	Text      string          `json:"text"`
	IsArticle int             `json:"is_article"`
	HasVideo  int             `json:"has_video"`
	ShareURL  string          `json:"share_url"`
	Imgs      json.RawMessage `json:"imgs"`
}

// commentGroup is one floor of the tree page: the root comment
// first, then the hot sub-comments the page previews inline (each
// parented by replyid — a sub's parent may be the root or another
// inline sub; the group's order is parents-first, the tree order
// the comments cache requires).
type commentGroup struct {
	Comment []treeComment `json:"comment"`
}

// treeComment is one comment node as both doors serve it (the tree
// groups and the sub pages share the shape). Roots carry floor_num
// and no replyid; subs carry replyid (their parent comment's id).
type treeComment struct {
	CommentID json.Number `json:"commentid"`
	ReplyID   json.Number `json:"replyid"`
	Text      string      `json:"text"`
	Up        int64       `json:"up"`
	ChildNum  int64       `json:"child_num"`
	CreateAt  int64       `json:"create_at"`
	User      struct {
		Username string `json:"username"`
	} `json:"user"`
}

// subResult is sub/comments' result payload: one flat page of the
// root's sub-comments plus the lastval cursor pair. A page re-serves
// the hot previews the tree already carried — the dedup is ours
// (held ids skip), the zhihu child-walk rule translated verbatim.
type subResult struct {
	Comments []treeComment `json:"comments"`
	Lastval  json.Number   `json:"lastval"`
	HasMore  bool          `json:"has_more"`
}

// --- DetailSource ---------------------------------------------------------

// DetailOnDemand implements source.DetailSource: every post card
// (a "hb:link/<hexid>" id) carries the two-layer detail.
func (s *Source) DetailOnDemand(it core.Item) bool {
	_, ok := hexIDOfItem(it.ID)
	return ok
}

// Detail implements source.DetailSource: the post's first tree page
// — body through the degradation rules, floors with their 楼中楼
// gaps completed, and the floors cursor (the offset of the page
// after this one, "" when total_page says the walk is done).
func (s *Source) Detail(ctx context.Context, it core.Item) (core.Content, error) {
	hexID, ok := hexIDOfItem(it.ID)
	if !ok {
		return core.Content{}, fmt.Errorf("detail: %q is not a heybox post id", it.ID)
	}
	page, err := s.fetchTree(ctx, hexID, 0)
	if err != nil {
		return core.Content{}, err
	}
	return core.Content{
		Body:     postBody(page.Link),
		Comments: s.completeGaps(ctx, hexID, floorTree(page)),
		Next:     floorsCursor(page, 0),
	}, nil
}

// CommentURL implements source.DetailSource: "" — a heybox comment
// id is a bare decimal with no page of its own (the share page is
// the post's), and heybox is a DetailPager source, so the reader's
// Ctrl+O/Ctrl+Y route through AnswerURL, which carries the item.
func (s *Source) CommentURL(id string) string { return "" }

// AnswerURL implements source.DetailPager: every layer of a heybox
// post opens the post itself — the share_url the card carried (the
// ticket's 「Ctrl+O/Ctrl+Y 走 share_url」).
func (s *Source) AnswerURL(it core.Item, answerID string) string { return it.URL }

// --- DetailPager ----------------------------------------------------------

// FetchFloorsPage implements source.DetailPager: one more floor page
// at the cursor's offset ("" = 0 — the same page Detail served),
// with the new floors' 楼中楼 gaps completed like Detail's. next is
// the following page's offset, "" at total_page's end or when the
// page served no floors (an empty page ends the walk, the interface
// contract's end signal).
func (s *Source) FetchFloorsPage(ctx context.Context, it core.Item, cursor string) ([]core.Comment, string, error) {
	hexID, ok := hexIDOfItem(it.ID)
	if !ok {
		return nil, "", fmt.Errorf("fetch floors: %q is not a heybox post id", it.ID)
	}
	offset := 0
	if cursor != "" {
		v, err := strconv.Atoi(cursor)
		if err != nil || v < 0 {
			return nil, "", fmt.Errorf("fetch floors %s: bad cursor %q", hexID, cursor)
		}
		offset = v
	}
	page, err := s.fetchTree(ctx, hexID, offset)
	if err != nil {
		return nil, "", err
	}
	return s.completeGaps(ctx, hexID, floorTree(page)), floorsCursor(page, offset), nil
}

// FetchAnswerComments implements source.DetailPager: one 楼中楼 page
// of the floor answerID names — "" the first page (the measured
// request carries no lastval; the host serves the root's subs from
// the start, re-serving the hot previews the tree already carried —
// the merge's dedup is refresh's), else the previous next verbatim.
// next is the page's lastval while has_more holds; "" = the walk's
// end.
func (s *Source) FetchAnswerComments(ctx context.Context, it core.Item, answerID string, cursor string) ([]core.Comment, string, error) {
	hexID, ok := hexIDOfItem(it.ID)
	if !ok {
		return nil, "", fmt.Errorf("fetch comments: %q is not a heybox post id", it.ID)
	}
	if answerID == "" {
		return nil, "", fmt.Errorf("fetch comments %s: no floor id", hexID)
	}
	return s.fetchSubPage(ctx, hexID, answerID, cursor)
}

// --- the fetch legs --------------------------------------------------------

// fetchTree dials one link/tree page at offset — the P11 leg
// verbatim: hex link_id, offset, limit 20, signed variant A.
func (s *Source) fetchTree(ctx context.Context, hexID string, offset int) (treeResult, error) {
	res := s.tr.Get(ctx, "link-tree", treePath, url.Values{
		"link_id": {hexID},
		"offset":  {strconv.Itoa(offset)},
		"limit":   {strconv.Itoa(floorPageSize)},
	})
	if res.Err != nil {
		return treeResult{}, fmt.Errorf("fetch tree %s: %w", hexID, res.Err)
	}
	var page treeResult
	if err := json.Unmarshal(res.Result, &page); err != nil {
		return treeResult{}, fmt.Errorf("parse tree %s: not heybox JSON: %w", hexID, err)
	}
	return page, nil
}

// fetchSubPage dials one sub/comments page — the P12/P13 leg: the
// hex link_id, the root's decimal comment id, limit, and lastval
// only from the second page on (the first page's measured shape
// carries none). Subs arrive flat with replyid parents; a sub whose
// replyid is 0 or absent belongs to the root itself.
func (s *Source) fetchSubPage(ctx context.Context, hexID, rootID, lastval string) ([]core.Comment, string, error) {
	params := url.Values{
		"link_id":         {hexID},
		"root_comment_id": {rootID},
		"limit":           {strconv.Itoa(subPageSize)},
	}
	if lastval != "" {
		params.Set("lastval", lastval)
	}
	res := s.tr.Get(ctx, "sub-comments", subPath, params)
	if res.Err != nil {
		return nil, "", fmt.Errorf("fetch subs %s: %w", rootID, res.Err)
	}
	var page subResult
	if err := json.Unmarshal(res.Result, &page); err != nil {
		return nil, "", fmt.Errorf("parse subs %s: not heybox JSON: %w", rootID, err)
	}
	out := make([]core.Comment, 0, len(page.Comments))
	for _, c := range page.Comments {
		cm := nodeToComment(c, rootID)
		if cm.ID == "" {
			continue // no commentid: nothing addressable, skip never crash
		}
		out = append(out, cm)
	}
	next := ""
	if page.HasMore && page.Lastval.String() != "" && page.Lastval.String() != "0" {
		next = page.Lastval.String()
	}
	return out, next, nil
}

// --- the pure mapping halves ----------------------------------------------

// floorTree maps one tree page's groups onto the comments cache's
// tree shape: each group's first node is the floor (ParentID ""),
// the rest are its hot sub-comments parented by replyid (a sub of a
// sub keeps its real parent — the flattened 楼中楼 shows the ↳ 回复
// prefix on it). Groups without an addressable root are not floors and not
// errors. Page-level dedup is the merge's (refresh); a page never
// serves its own repeat.
func floorTree(page treeResult) []core.Comment {
	out := make([]core.Comment, 0, len(page.Comments))
	for _, g := range page.Comments {
		if len(g.Comment) == 0 {
			continue
		}
		root := nodeToComment(g.Comment[0], "")
		if root.ID == "" {
			continue
		}
		out = append(out, root)
		for _, sub := range g.Comment[1:] {
			cm := nodeToComment(sub, root.ID) // replyid 0/absent → the root
			if cm.ID == "" {
				continue
			}
			out = append(out, cm)
		}
	}
	return out
}

// nodeToComment maps one comment node under parentID: text through
// Degrade (the cube rule), up folded nonzero (赞 0 occupies nothing,
// the zhihu emit rule), create_at when the host sent one, and
// child_num VERBATIM zero included — it is data (the reply layer's
// zero-request fast path reads "reported zero" off it), the same
// role answer floors' comment_count plays.
func nodeToComment(c treeComment, parentID string) core.Comment {
	cm := core.Comment{
		ID:       c.CommentID.String(),
		ParentID: parentID,
		Author:   c.User.Username,
		Text:     Degrade(c.Text),
	}
	if rid := c.ReplyID.String(); rid != "" && rid != "0" && parentID != "" {
		cm.ParentID = rid
	}
	if v := c.Up; v != 0 {
		n := int(v)
		cm.Score = &n
	}
	if ts := c.CreateAt; ts != 0 {
		t := time.Unix(ts, 0).UTC()
		cm.CreatedAt = &t
	}
	n := int(c.ChildNum)
	cm.NumChildren = &n
	return cm
}

// floorsCursor is the tree page's next offset: pages run 0..
// total_page-1, so a next exists only while offset+1 stays under
// total_page — and a page that served no floors ends the walk even
// if the host's total lied upward (an empty page is the end signal,
// the interface contract).
func floorsCursor(page treeResult, offset int) string {
	if len(page.Comments) == 0 || offset+1 >= page.TotalPage {
		return ""
	}
	return strconv.Itoa(offset + 1)
}

// completeGaps walks every floor's 楼中楼 gap in place: a floor
// whose child_num exceeds the sub-comments its groups carried gets
// the sub walk's pages appended (deduped, parents resolved) after
// the floor's own inline previews — tree order keeps parents
// strictly before children — sequentially, because the politeness
// ledger paces by dial and parallel walkers would bunch their dials
// on one ledger wake (the smoke's zero-429 came from serial gaps;
// pacing correctness over completion speed). A failed walk is soft
// and silent, the zhihu rule: the floor keeps its hot previews, and
// a partial comment tree beats a failed page. A floor re-served on
// a later page re-walks its gap (the source cannot see the cache
// that already holds the walked subs — the merge drops them): a
// bounded waste the smoke's measured zero-overlap offsets make
// rare, the same re-walk cost zhihu's completion carries.
func (s *Source) completeGaps(ctx context.Context, hexID string, tree []core.Comment) []core.Comment {
	out := make([]core.Comment, 0, len(tree))
	held := make(map[string]bool, len(tree))
	for _, c := range tree {
		held[c.ID] = true
	}
	for i := 0; i < len(tree); {
		c := tree[i]
		out = append(out, c)
		i++
		if c.ParentID != "" {
			continue // a sub: only floors own a gap
		}
		subs := 0
		for i < len(tree) && tree[i].ParentID != "" { // the floor's inline previews
			out = append(out, tree[i])
			i++
			subs++
		}
		var gap int64
		if c.NumChildren != nil {
			gap = int64(*c.NumChildren) - int64(subs)
		}
		if gap <= 0 {
			continue // no debt: the preview carried everything (or none owed)
		}
		walked, _ := s.walkSubs(ctx, hexID, c.ID, held) // soft: previews stay
		out = append(out, walked...)
	}
	return out
}

// walkSubs pages one root's 楼中楼 from the start (the door serves
// all of the root's subs, re-serving the previews the tree carried
// — held ids skip, the dedup), stopping at the server's has_more
// end, a missing cursor, or the maxSubPages bound. A sub whose
// parent is neither the root nor an id this walk or tree already
// holds (deleted upstream) re-parents onto the root: it belongs to
// this 楼中楼's family, and a stray parent id would promote the row
// to a floor at the UI — the worse lie.
func (s *Source) walkSubs(ctx context.Context, hexID, rootID string, held map[string]bool) ([]core.Comment, error) {
	var out []core.Comment
	lastval := ""
	for range maxSubPages {
		page, next, err := s.fetchSubPage(ctx, hexID, rootID, lastval)
		if err != nil {
			return out, err // soft stop: what arrived so far stays
		}
		for _, c := range page {
			if held[c.ID] {
				continue
			}
			held[c.ID] = true
			if c.ParentID != rootID && !held[c.ParentID] {
				c.ParentID = rootID
			}
			out = append(out, c)
		}
		if next == "" {
			return out, nil
		}
		lastval = next
	}
	return out, nil
}

// --- the post body --------------------------------------------------------

// postBody renders the post's full text under the spec's
// degradation rules. Three body kinds ride link.text: plain text
// (with [cube_X] markers), a serialized text-block array (the
// rich-text contract: text blocks and image URLs), and article HTML
// (is_article posts — passed to the render pipeline, which does the
// typography). Images — block URLs and the link's own imgs list —
// force the HTML assembly so the pipeline renders its [图片] link
// markers (the all-source convention); a video post appends its
// marker line linking the share_url (视频帖不解析, the spec's
// honesty rule). An empty text answers "" — the caller keeps the
// list snapshot's excerpt instead.
func postBody(l treeLink) string {
	if strings.TrimSpace(l.Text) == "" {
		return ""
	}
	if l.IsArticle == 1 && strings.Contains(l.Text, "<") {
		return Degrade(l.Text) // HTML: cube markers live in text nodes, tags are safe
	}
	paras, imgs := parseTextBlocks(l.Text)
	imgs = append(imgs, linkImgs(l.Imgs)...)
	hasVideo := l.HasVideo == 1
	if len(imgs) == 0 && !hasVideo {
		return strings.Join(paras, "\n\n") // plain: one paragraph per source line
	}
	var b strings.Builder
	for _, p := range paras {
		if p != "" {
			fmt.Fprintf(&b, "<p>%s</p>", html.EscapeString(p))
		}
	}
	for _, u := range imgs {
		fmt.Fprintf(&b, "<img src=%q>", u)
	}
	if hasVideo {
		if l.ShareURL != "" {
			fmt.Fprintf(&b, "<p><a href=%q>[%s]</a></p>", l.ShareURL, videoMarker)
		} else {
			fmt.Fprintf(&b, "<p>[%s]</p>", videoMarker)
		}
	}
	return b.String()
}

// videoMarker is the video post's honesty line — the list stays
// fast, the detail says what it lost.
const videoMarker = "视频"

// textBlock is one block of the serialized rich-text body. The
// contract's spelling is {"type":"text","text":"…"}; blocks carrying
// a URL are images regardless of their type string (the shapes
// drifted between host versions; a URL is the one honest signal).
// Unknown blocks without either drop — a marker we cannot name is
// noise, not content.
type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	URL  string `json:"url"`
}

// parseTextBlocks splits link.text into paragraphs and image URLs:
// a JSON block array when it parses (the rich-text contract), else
// the whole string as one plain paragraph. Cube degradation rides
// per block — paragraphs join plain, blocks join as HTML text.
func parseTextBlocks(text string) (paras []string, imgs []string) {
	s := strings.TrimSpace(text)
	if !strings.HasPrefix(s, "[") {
		return []string{Degrade(s)}, nil
	}
	var blocks []textBlock
	if err := json.Unmarshal([]byte(s), &blocks); err != nil {
		return []string{Degrade(s)}, nil // not JSON after all: plain
	}
	for _, b := range blocks {
		switch {
		case b.URL != "":
			imgs = append(imgs, b.URL)
		case strings.TrimSpace(b.Text) != "":
			paras = append(paras, Degrade(b.Text))
		}
	}
	return paras, imgs
}

// linkImgs extracts the link's imgs list leniently: entries may be
// objects (url/origin_url) or plain URL strings depending on host
// version, and a malformed entry or list is skipped — never a
// failed tree parse (the research flagged this as the one shape
// risk; the 04 smoke ticket calibrates against real samples).
func linkImgs(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		var obj struct {
			URL    string `json:"url"`
			Origin string `json:"origin_url"`
		}
		if err := json.Unmarshal(e, &obj); err == nil && (obj.URL != "" || obj.Origin != "") {
			if obj.URL != "" {
				out = append(out, obj.URL)
			} else {
				out = append(out, obj.Origin)
			}
			continue
		}
		var s string
		if err := json.Unmarshal(e, &s); err == nil && s != "" {
			out = append(out, s)
		}
	}
	return out
}
