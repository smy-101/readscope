package heybox

// detail_test.go — the post detail's two layers (ticket 03) against
// one fake heybox on loopback serving the smoke's own sample shapes
// (testdata/link-tree.json — a trimmed cut of the captured P11 tree,
// testdata/sub-comments.json — the P12 楼中楼 page) plus synthesized
// second pages: the tree→Content mapping (body degradations, floors,
// hot subs, the floors cursor), the 楼中楼 gap completion (缺口计算
// + 去重 + has_more 终止), the two walks' request fingerprints, and
// the pure halves (postBody shapes, floorsCursor, orphan re-parenting).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/smy-101/readscope/internal/core"
)

// treeFixture loads the trimmed captured tree page.
func treeFixture(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/link-tree.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var page map[string]any
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return page
}

// subFixture loads the trimmed captured 楼中楼 page.
func subFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/sub-comments.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

// detailPlane is one fake heybox detail host: the tree at both
// offsets (page 2 synthesized unless overridden) and the 楼中楼 door
// per root (the captured page unless overridden), every leg's query
// recorded per path. The signature triple's presence is checked —
// the transport tests pin its arithmetic.
type detailPlane struct {
	trees   map[string][]byte // offset → body
	subs    map[string][]byte // root_comment_id → body
	treeGot []url.Values
	subGot  []url.Values
}

func (p *detailPlane) handler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for _, k := range []string{"hkey", "_time", "nonce"} {
		if q.Get(k) == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"msg":"缺少必要参数","status":"failed"}`)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case treePath:
		body, ok := p.trees[q.Get("offset")]
		if !ok {
			fmt.Fprint(w, `{"status":"ok","result":{"link":{},"comments":[],"total_page":0}}`)
			return
		}
		p.treeGot = append(p.treeGot, q)
		_, _ = w.Write(body)
	case subPath:
		// A root's later pages are keyed "<root>|<lastval>"; the first
		// page (no lastval) keys the root alone.
		key := q.Get("root_comment_id")
		if q.Get("lastval") != "" {
			key += "|" + q.Get("lastval")
		}
		body, ok := p.subs[key]
		if !ok {
			body, ok = p.subs[q.Get("root_comment_id")]
		}
		if !ok {
			fmt.Fprint(w, `{"status":"ok","result":{"has_more":false,"lastval":0,"comments":[]}}`)
			return
		}
		p.subGot = append(p.subGot, q)
		_, _ = w.Write(body)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// detailSrcAt lands a source against one detail plane.
func detailSrcAt(t *testing.T, p *detailPlane) *Source {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(p.handler))
	t.Cleanup(srv.Close)
	return newSourceAt(srv.URL, WithMinGap(0))
}

// treeItem builds the post card the detail opens.
func treeItem() core.Item {
	return core.Item{
		ID: ItemID("a1b2c3d4e5f6"), Source: ID, Feed: RecommendFeedKey,
		Title: "成功忽悠两个入坑", Author: "白素贞",
		URL: "https://api.xiaoheihe.cn/v3/bbs/app/api/web/share?link_id=a1b2c3d4e5f6",
	}
}

// planeFromFixture seeds a plane with the captured tree (as offset
// "0") and the captured 楼中楼 page (served to every root).
func planeFromFixture(t *testing.T) *detailPlane {
	raw, err := json.Marshal(treeFixture(t))
	if err != nil {
		t.Fatalf("re-marshal fixture: %v", err)
	}
	return &detailPlane{
		trees: map[string][]byte{"0": raw},
		subs:  map[string][]byte{"997948522": subFixture(t), "997615008": subFixture(t)},
	}
}

// fixtureCounts pins the fixture's own shape: 9 floors, three with
// children (child_num 5/1/3 carrying 2/1/2 inline previews → gaps
// 3/0/1), total_page 1. The gap tests below read these off.
func fixtureCounts(t *testing.T) {
	t.Helper()
	res := treeFixture(t)["result"].(map[string]any)
	if got := len(res["comments"].([]any)); got != 9 {
		t.Fatalf("fixture floors = %d, want 9 (a trimmed edit broke the fixture)", got)
	}
	if tp := res["total_page"]; tp != float64(1) {
		t.Fatalf("fixture total_page = %v, want 1", tp)
	}
}

// TestDetailMapsTreeToBodyAndFloors: the open's request fingerprint
// (hex link_id, offset 0, limit 20), the body (the video post's text
// with the cube marker degraded and the [视频] share link appended),
// the floors (9 roots, ParentID "", child_num verbatim zero included)
// and the hot subs (parented by replyid — a sub of a sub keeps its
// real parent). total_page 1 → no floors cursor.
func TestDetailMapsTreeToBodyAndFloors(t *testing.T) {
	fixtureCounts(t)
	p := planeFromFixture(t)
	p.subs = nil // no gaps walked here: the mapping, not the completion
	src := detailSrcAt(t, p)

	c, err := src.Detail(t.Context(), treeItem())
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	// The request: hex link_id straight off the item id, first page.
	if len(p.treeGot) != 1 {
		t.Fatalf("tree dials = %d, want 1", len(p.treeGot))
	}
	q := p.treeGot[0]
	if q.Get("link_id") != "a1b2c3d4e5f6" {
		t.Errorf("link_id = %q, want the hex id (decimal ids are the captcha honeypot)", q.Get("link_id"))
	}
	if q.Get("offset") != "0" || q.Get("limit") != "20" {
		t.Errorf("offset/limit = %q/%q, want 0/20", q.Get("offset"), q.Get("limit"))
	}
	// The body: video post — degraded text plus the share link marker.
	if !strings.Contains(c.Body, "成功忽悠两个入坑[滑稽]") {
		t.Errorf("body missing the degraded text: %q", c.Body)
	}
	if !strings.Contains(c.Body, "<a href=") || !strings.Contains(c.Body, "[视频]") {
		t.Errorf("video body missing the [视频] share link: %q", c.Body)
	}
	// Floors and subs: 9 roots (ParentID "") + 5 inline subs.
	var roots, subs int
	byID := map[string]core.Comment{}
	for _, cm := range c.Comments {
		byID[cm.ID] = cm
		if cm.ParentID == "" {
			roots++
		} else {
			subs++
		}
	}
	if roots != 9 || subs != 5 {
		t.Fatalf("roots/subs = %d/%d, want 9/5", roots, subs)
	}
	root := byID["997948522"]
	if root.ParentID != "" {
		t.Errorf("root 997948522 ParentID = %q, want \"\"", root.ParentID)
	}
	if root.Author != "韩望舒" {
		t.Errorf("root author = %q, want the fixture's username", root.Author)
	}
	if root.NumChildren == nil || *root.NumChildren != 5 {
		t.Errorf("root child_num = %v, want 5 verbatim (zero-request fast path reads it)", root.NumChildren)
	}
	if root.Score == nil || *root.Score != 4 {
		t.Errorf("root up = %v, want 4", root.Score)
	}
	// The sub-of-sub chain: 998127763 replies to 998059957 (itself a
	// sub of the root) — the flattened 楼中楼's @parent needs the real
	// parent link.
	if byID["998127763"].ParentID != "998059957" {
		t.Errorf("sub 998127763 ParentID = %q, want 998059957", byID["998127763"].ParentID)
	}
	if byID["998059957"].ParentID != "997948522" {
		t.Errorf("sub 998059957 ParentID = %q, want the root", byID["998059957"].ParentID)
	}
	// A childless floor: child_num 0 stays data, not nil.
	if bare := byID["997952961"]; bare.NumChildren == nil || *bare.NumChildren != 0 {
		t.Errorf("childless floor NumChildren = %v, want 0 verbatim", bare.NumChildren)
	}
	if c.Next != "" {
		t.Errorf("Next = %q, want \"\" (total_page 1 exhausts the walk)", c.Next)
	}
}

// TestDetailCompletesFloorGaps: the two gapped floors (gaps 3 and 1)
// each walk their 楼中楼 — root_comment_id + hex link_id + limit, no
// lastval on a first page — the re-served hot previews dedup, the
// fresh subs land parented (a held parent keeps its real id), and
// has_more=false ends the walk. The gapless floor (child_num 1, one
// preview) and every childless floor never dial.
func TestDetailCompletesFloorGaps(t *testing.T) {
	p := planeFromFixture(t)
	src := detailSrcAt(t, p)

	c, err := src.Detail(t.Context(), treeItem())
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(p.subGot) != 2 {
		t.Fatalf("sub dials = %d, want 2 (only the gapped floors 997948522 and 997615008)", len(p.subGot))
	}
	roots := map[string]bool{}
	for _, q := range p.subGot {
		roots[q.Get("root_comment_id")] = true
		if q.Get("link_id") != "a1b2c3d4e5f6" {
			t.Errorf("sub link_id = %q, want the hex id", q.Get("link_id"))
		}
		if q.Get("limit") != "20" {
			t.Errorf("sub limit = %q, want 20", q.Get("limit"))
		}
		if q.Get("lastval") != "" {
			t.Errorf("first sub page carried lastval %q, want none (the measured shape)", q.Get("lastval"))
		}
	}
	if !roots["997948522"] || !roots["997615008"] {
		t.Errorf("walked roots = %v, want the two gapped floors", roots)
	}
	// The gap filled: the captured page re-serves both hot previews
	// (deduped) and adds two fresh subs parented by their replyids.
	ids := map[string]core.Comment{}
	for _, cm := range c.Comments {
		ids[cm.ID] = cm
	}
	for _, id := range []string{"998151957", "998703867"} {
		if _, ok := ids[id]; !ok {
			t.Errorf("fresh sub %s missing from the completed tree", id)
		}
	}
	if sub := ids["998151957"]; sub.ParentID != "998059957" {
		t.Errorf("fresh sub 998151957 ParentID = %q, want its held parent 998059957", sub.ParentID)
	}
	if sub := ids["998703867"]; sub.ParentID != "998127763" {
		t.Errorf("fresh sub 998703867 ParentID = %q, want its held parent 998127763", sub.ParentID)
	}
	// No duplicates: every id exactly once.
	seen := map[string]int{}
	for _, cm := range c.Comments {
		seen[cm.ID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("comment %s served %d times — the re-served preview must dedup", id, n)
		}
	}
	// The visible count ends at the SERVER's truth, not child_num: the
	// captured root reports child_num 5 but its walk ends at
	// has_more=false having served 4 (the missing one is deleted
	// upstream — the host's own arithmetic, P12's measurement). The
	// floor header shows the tree's count once replies are loaded, so
	// 楼中楼可见数与楼层头一致, by construction of this walk.
	subsOf522 := 0
	floorOf := map[string]string{}
	for _, cm := range c.Comments {
		if cm.ParentID == "" {
			floorOf[cm.ID] = cm.ID
		} else if f, ok := floorOf[cm.ParentID]; ok {
			floorOf[cm.ID] = f
		}
	}
	for _, cm := range c.Comments {
		if floorOf[cm.ID] == "997948522" && cm.ID != "997948522" {
			subsOf522++
		}
	}
	if subsOf522 != 4 {
		t.Errorf("floor 997948522 visible subs = %d, want 4 (the server's has_more end)", subsOf522)
	}
	// Tree order survived: parents strictly before children.
	pos := map[string]int{}
	for i, cm := range c.Comments {
		pos[cm.ID] = i
	}
	for _, cm := range c.Comments {
		if cm.ParentID != "" && pos[cm.ParentID] > pos[cm.ID] {
			t.Errorf("sub %s lands before its parent %s", cm.ID, cm.ParentID)
		}
	}
}

// TestDetailZeroCommentPostDialsOnce: a post whose floors are all
// childless opens with exactly one dial — the tree — and answers an
// empty cursor. (The reader's 0-评论 prompt rides this: nothing to
// walk, nothing to fetch.)
func TestDetailZeroCommentPostDialsOnce(t *testing.T) {
	zero := `{"status":"ok","result":{"link":{"text":"正文","share_url":"https://x/share?link_id=a1b2c3d4e5f6"},
	 "comments":[{"comment":[{"commentid":1,"userid":1,"create_at":1794386229,"text":"一楼","child_num":0,"up":0,
	   "floor_num":1,"user":{"username":"甲"}}]}],
	 "total_page":1,"total_floor_num":1}}`
	p := &detailPlane{trees: map[string][]byte{"0": []byte(zero)}}
	src := detailSrcAt(t, p)

	c, err := src.Detail(t.Context(), treeItem())
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(p.treeGot) != 1 || len(p.subGot) != 0 {
		t.Fatalf("dials tree/sub = %d/%d, want 1/0", len(p.treeGot), len(p.subGot))
	}
	if len(c.Comments) != 1 || c.Next != "" {
		t.Fatalf("comments/next = %d/%q, want 1/\"\"", len(c.Comments), c.Next)
	}
}

// TestFetchFloorsPageWalksOffsets: the cursor is an offset — page 2
// at cursor "1", the walk ending at total_page ("" past the last
// page, "" on a page that served nothing). New floors from page 2
// arrive with their own previews.
func TestFetchFloorsPageWalksOffsets(t *testing.T) {
	p := planeFromFixture(t)
	// Grow the fixture's total_page and serve a second page with one
	// new floor carrying one preview.
	res := treeFixture(t)["result"].(map[string]any)
	res["total_page"] = 2
	page1, err := json.Marshal(map[string]any{"status": "ok", "result": res})
	if err != nil {
		t.Fatal(err)
	}
	page2 := `{"status":"ok","result":{"link":{"text":"正文"},
	 "comments":[{"comment":[
	   {"commentid":888001,"userid":9,"create_at":1794386229,"text":"十一楼","child_num":1,"up":2,
	    "floor_num":11,"user":{"username":"十一"}},
	   {"commentid":888002,"userid":10,"create_at":1794386229,"text":"回十一楼","child_num":0,"up":0,
	    "replyid":888001,"user":{"username":"十二"}}]}],
	 "total_page":2,"total_floor_num":11}}`
	p.trees["0"], p.trees["1"] = page1, []byte(page2)
	src := detailSrcAt(t, p)

	_, next, err := src.FetchFloorsPage(t.Context(), treeItem(), "")
	if err != nil {
		t.Fatalf("page 0: %v", err)
	}
	if next != "1" {
		t.Fatalf("page 0 next = %q, want 1", next)
	}
	more, next, err := src.FetchFloorsPage(t.Context(), treeItem(), next)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if next != "" {
		t.Fatalf("page 1 next = %q, want \"\" (total_page 2 served)", next)
	}
	if len(more) != 2 {
		t.Fatalf("page 2 tree = %d nodes, want 2 (the new floor and its preview)", len(more))
	}
	if more[0].ID != "888001" || more[0].ParentID != "" || more[0].Author != "十一" {
		t.Errorf("page 2 floor = %+v, want 888001 as a floor by 十一", more[0])
	}
	if more[1].ID != "888002" || more[1].ParentID != "888001" {
		t.Errorf("page 2 preview = %+v, want 888002 parented by its floor", more[1])
	}
	// Past the end: an offset with no page answers empty — the walk's
	// end signal, not an error.
	end, next, err := src.FetchFloorsPage(t.Context(), treeItem(), "2")
	if err != nil {
		t.Fatalf("page 2 (unserved): %v", err)
	}
	if len(end) != 0 || next != "" {
		t.Fatalf("unserved offset = %d nodes/next %q, want 0/\"\"", len(end), next)
	}
	// A malformed cursor refuses by name.
	if _, _, err := src.FetchFloorsPage(t.Context(), treeItem(), "soon"); err == nil {
		t.Error("a non-numeric cursor must refuse")
	}
}

// TestFetchAnswerCommentsWalksLastval: the 楼中楼 walk's page shape —
// subs parented by replyid (a 0/absent replyid belongs to the root),
// the cursor riding the server's lastval while has_more holds, and
// the second page carrying the previous lastval verbatim.
func TestFetchAnswerCommentsWalksLastval(t *testing.T) {
	p := &detailPlane{subs: map[string][]byte{
		"997948522": subFixture(t),
		"777001": []byte(`{"status":"ok","result":{"has_more":true,"lastval":777500,
		 "comments":[
		  {"commentid":777101,"userid":1,"create_at":1794386229,"text":"根回一线","up":1,
		   "replyid":777001,"user":{"username":"回一"}},
		  {"commentid":777102,"userid":2,"create_at":1794386229,"text":"无回复对象","up":0,
		   "user":{"username":"回二"}}]}}`),
		"777001|777500": []byte(`{"status":"ok","result":{"has_more":false,"lastval":777900,
		 "comments":[
		  {"commentid":777103,"userid":3,"create_at":1794386229,"text":"第二页回","up":0,
		   "replyid":777101,"user":{"username":"回三"}}]}}`),
	}}
	src := detailSrcAt(t, p)

	subs, next, err := src.FetchAnswerComments(t.Context(), treeItem(), "777001", "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("subs = %d, want 2", len(subs))
	}
	if subs[0].ParentID != "777001" || subs[0].Score == nil || *subs[0].Score != 1 {
		t.Errorf("sub 777101 = %+v (want the root parent, up 1)", subs[0])
	}
	if subs[1].ParentID != "777001" {
		t.Errorf("sub 777102 ParentID = %q, want the root (replyid absent)", subs[1].ParentID)
	}
	if next != "777500" {
		t.Fatalf("next = %q, want the server's lastval 777500", next)
	}
	// Page two rides the cursor verbatim and ends the walk.
	page2, next, err := src.FetchAnswerComments(t.Context(), treeItem(), "777001", next)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(page2) != 1 || page2[0].ParentID != "777101" {
		t.Fatalf("page 2 = %+v, want one sub of 777101", page2)
	}
	if next != "" {
		t.Errorf("page 2 next = %q, want \"\" (has_more ended)", next)
	}
	if len(p.subGot) != 2 || p.subGot[1].Get("lastval") != "777500" {
		t.Fatalf("second dial must carry lastval=777500, got %+v", p.subGot)
	}
	// A bare non-post id refuses by name (the pager's contract).
	if _, _, err := src.FetchAnswerComments(t.Context(), core.Item{ID: "hb:other"}, "777001", ""); err == nil {
		t.Error("a foreign item id must refuse")
	}
}

// TestPostBodyShapes pins the degradation family on the pure half:
// plain text (cube degraded, paragraphs joined), the serialized
// text-block array (text blocks + image URLs → the HTML assembly the
// pipeline renders [图片] markers from), article HTML passed to the
// pipeline, the imgs list's lenient shapes, and the empty answer.
func TestPostBodyShapes(t *testing.T) {
	plain := postBody(treeLink{Text: "一段正文\n\n第二段[cube_滑稽]"})
	if plain != "一段正文\n\n第二段[滑稽]" {
		t.Errorf("plain body = %q", plain)
	}
	if got := postBody(treeLink{Text: "  "}); got != "" {
		t.Errorf("blank text = %q, want \"\" (the caller keeps the excerpt)", got)
	}
	// Rich text: text blocks + an image URL block.
	blocks := postBody(treeLink{Text: `[{"type":"text","text":"看图[cube_笑c]"},{"type":"image","url":"https://img/x.jpg"}]`})
	for _, want := range []string{"<p>看图[笑c]</p>", `<img src="https://img/x.jpg">`} {
		if !strings.Contains(blocks, want) {
			t.Errorf("block body missing %q: %q", want, blocks)
		}
	}
	// Article HTML rides to the pipeline verbatim (cube rule applied).
	art := postBody(treeLink{Text: "<p>文章<br>正文[cube_狗头]</p>", IsArticle: 1})
	if art != "<p>文章<br>正文[狗头]</p>" {
		t.Errorf("article body = %q", art)
	}
	// The link's imgs: object entries, string entries, and garbage —
	// none of them may fail the body.
	imgs := postBody(treeLink{Text: "配图", Imgs: json.RawMessage(`[{"url":"https://img/a.jpg"},{"url":"","origin_url":"https://img/b.jpg"},"https://img/c.jpg",42,{"no_url":1}]`)})
	for _, want := range []string{`src="https://img/a.jpg"`, `src="https://img/b.jpg"`, `src="https://img/c.jpg"`} {
		if !strings.Contains(imgs, want) {
			t.Errorf("imgs body missing %s: %q", want, imgs)
		}
	}
	if strings.Contains(imgs, "42") {
		t.Errorf("a garbage imgs entry leaked into the body: %q", imgs)
	}
	// Video without a share_url still says so plainly.
	if got := postBody(treeLink{Text: "视频帖", HasVideo: 1}); !strings.Contains(got, "[视频]") {
		t.Errorf("video body = %q, want the marker", got)
	}
}

// TestFloorsCursor pins the pure cursor rule: total_page bounds the
// walk, an empty page ends it regardless.
func TestFloorsCursor(t *testing.T) {
	page := treeResult{TotalPage: 3, Comments: []commentGroup{{}}}
	if got := floorsCursor(page, 0); got != "1" {
		t.Errorf("cursor(3 pages, at 0) = %q, want 1", got)
	}
	if got := floorsCursor(page, 2); got != "" {
		t.Errorf("cursor(3 pages, at 2) = %q, want \"\"", got)
	}
	if got := floorsCursor(treeResult{TotalPage: 3}, 0); got != "" {
		t.Errorf("cursor(empty page) = %q, want \"\" (the end signal)", got)
	}
}

// TestWalkSubsReparentsOrphans: a sub whose replyid names an id
// neither the tree nor the walk holds (deleted upstream) re-parents
// onto the root — the row stays in its 楼中楼 instead of being
// promoted to a floor by the UI's orphan rule.
func TestWalkSubsReparentsOrphans(t *testing.T) {
	p := &detailPlane{subs: map[string][]byte{
		"555001": []byte(`{"status":"ok","result":{"has_more":false,"lastval":0,
		 "comments":[
		  {"commentid":555101,"userid":1,"create_at":1794386229,"text":"孤儿","up":0,
		   "replyid":555999,"user":{"username":"孤儿"}},
		  {"commentid":555102,"userid":2,"create_at":1794386229,"text":"正常","up":0,
		   "replyid":555101,"user":{"username":"正常"}}]}}`),
	}}
	src := detailSrcAt(t, p)
	root := core.Comment{ID: "555001", NumChildren: func() *int { v := 2; return &v }()}
	walked, err := src.walkSubs(t.Context(), "a1b2c3d4e5f6", root.ID, map[string]bool{root.ID: true})
	if err != nil {
		t.Fatalf("walkSubs: %v", err)
	}
	if len(walked) != 2 {
		t.Fatalf("walked = %d, want 2", len(walked))
	}
	if walked[0].ParentID != "555001" {
		t.Errorf("orphan ParentID = %q, want the root 555001", walked[0].ParentID)
	}
	if walked[1].ParentID != "555101" {
		t.Errorf("known-parent sub ParentID = %q, want 555101", walked[1].ParentID)
	}
}

// TestDetailOnDemandAndURLs: every post id carries the detail, a
// foreign id does not, and the Ctrl+O door is the share_url.
func TestDetailOnDemandAndURLs(t *testing.T) {
	src := newSourceAt("http://x")
	if !src.DetailOnDemand(treeItem()) {
		t.Error("a post card must carry the detail layer")
	}
	if src.DetailOnDemand(core.Item{ID: "hb:other"}) {
		t.Error("a foreign id must not")
	}
	if got := src.CommentURL("997948522"); got != "" {
		t.Errorf("CommentURL = %q, want \"\" (a bare comment id addresses no page)", got)
	}
	it := treeItem()
	if got := src.AnswerURL(it, "997948522"); got != it.URL {
		t.Errorf("AnswerURL = %q, want the share_url", got)
	}
}
