package store

// Ticket 03 tests: the comments DAO — per-item whole-tree replace
// writes and tree-order read-backs, against real SQLite (the same
// migrated helper as the items/readstate tests). The hn fetch that
// produces these slices is tested in internal/source/hn; the
// fetch-then-cache composition in internal/refresh.

import (
	"testing"

	"github.com/smy-101/readscope/internal/core"
)

// treeFixture is one small comment forest in tree order (floors 10
// and 13, each with one nested reply).
func treeFixture() []core.Comment {
	return []core.Comment{
		{ID: "10", ParentID: "", Author: "bob", Text: "root comment"},
		{ID: "11", ParentID: "10", Author: "carol", Text: "<i>nested</i> html"},
		{ID: "13", ParentID: "", Author: "", Text: ""}, // deleted placeholder
		{ID: "14", ParentID: "13", Author: "erin", Text: "child of deleted"},
	}
}

func mustSaveItems(t *testing.T, s *Store, its ...core.Item) {
	t.Helper()
	for _, it := range its {
		if _, _, err := s.SaveFeedItems(t.Context(), it.Source, it.Feed, []core.Item{it}); err != nil {
			t.Fatalf("save item %s: %v", it.ID, err)
		}
	}
}

func hnStory() core.Item {
	return core.Item{
		ID: "hn:algolia:1001", Source: "hn", Feed: "front",
		Title: "Hot story", Author: "alice", FetchedAt: nowUTC(),
	}
}

// Save → read-back is the offline-reading contract: what Detail
// fetched once is retrievable verbatim, in tree order, with parent
// links intact ("" meaning top-level floor both ways across the
// NULL boundary).
func TestCommentsRoundTripPreservesTree(t *testing.T) {
	s := migrated(t)
	mustSaveItems(t, s, hnStory())

	if err := s.SaveComments(t.Context(), "hn:algolia:1001", treeFixture()); err != nil {
		t.Fatalf("SaveComments: %v", err)
	}
	got, err := s.CommentsForItem(t.Context(), "hn:algolia:1001")
	if err != nil {
		t.Fatalf("CommentsForItem: %v", err)
	}
	want := treeFixture()
	if len(got) != len(want) {
		t.Fatalf("got %d comments, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID || got[i].ParentID != want[i].ParentID ||
			got[i].Author != want[i].Author || got[i].Text != want[i].Text {
			t.Fatalf("comment[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// Re-Detail the same story (a later open, a background refresh) is a
// whole-tree replace: the second save fully swaps the first, so no
// duplicate rows survive and stale floors disappear.
func TestSaveCommentsReplacesWholeTree(t *testing.T) {
	s := migrated(t)
	mustSaveItems(t, s, hnStory())

	if err := s.SaveComments(t.Context(), "hn:algolia:1001", treeFixture()); err != nil {
		t.Fatalf("first save: %v", err)
	}
	fresh := []core.Comment{
		{ID: "20", ParentID: "", Author: "zoe", Text: "new floor"},
		{ID: "21", ParentID: "20", Author: "yan", Text: "new reply"},
	}
	if err := s.SaveComments(t.Context(), "hn:algolia:1001", fresh); err != nil {
		t.Fatalf("second save: %v", err)
	}
	got, err := s.CommentsForItem(t.Context(), "hn:algolia:1001")
	if err != nil {
		t.Fatalf("CommentsForItem: %v", err)
	}
	if len(got) != 2 || got[0].ID != "20" || got[1].ID != "21" {
		t.Fatalf("whole-tree replace left residue: %+v", got)
	}

	// An empty tree replaces just as fully (a story whose comments
	// were all removed upstream).
	if err := s.SaveComments(t.Context(), "hn:algolia:1001", nil); err != nil {
		t.Fatalf("empty save: %v", err)
	}
	got, err = s.CommentsForItem(t.Context(), "hn:algolia:1001")
	if err != nil {
		t.Fatalf("CommentsForItem after empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty tree replace left %d rows", len(got))
	}
}

// Comments belong to an existing item (the FK is the integrity
// contract): caching comments for a row that was never fetched is an
// error, not silent orphan data.
func TestSaveCommentsRequiresExistingItem(t *testing.T) {
	s := migrated(t)
	if err := s.SaveComments(t.Context(), "hn:algolia:9999", treeFixture()); err == nil {
		t.Fatal("want FK error for unknown item, got nil")
	}
}

// Ticket 04: the opened marker. "0 cached comments" and "never
// opened" must stay distinguishable — a story whose discussion is
// empty was still opened (and must not re-fetch as if first open on
// every visit). DetailFetched is that signal: false until the first
// whole-tree save lands, true from then on, per item.
func TestDetailFetchedMarksOpened(t *testing.T) {
	s := migrated(t)
	mustSaveItems(t, s, hnStory())

	if ok, err := s.DetailFetched(t.Context(), "hn:algolia:1001"); err != nil || ok {
		t.Fatalf("before any save: DetailFetched = %v, %v; want false, nil", ok, err)
	}
	if err := s.SaveComments(t.Context(), "hn:algolia:1001", treeFixture()); err != nil {
		t.Fatalf("SaveComments: %v", err)
	}
	if ok, err := s.DetailFetched(t.Context(), "hn:algolia:1001"); err != nil || !ok {
		t.Fatalf("after save: DetailFetched = %v, %v; want true, nil", ok, err)
	}
	// Another item stays unopened.
	other := hnStory()
	other.ID = "hn:algolia:2002"
	mustSaveItems(t, s, other)
	if ok, _ := s.DetailFetched(t.Context(), "hn:algolia:2002"); ok {
		t.Fatal("unrelated item must not inherit the marker")
	}
}

// The empty-tree half of the contract: a story with no comments was
// still opened — the marker lands even when the tree is empty, which
// is exactly the case a naive "comments exist?" probe cannot tell
// from never-opened.
func TestDetailFetchedSurvivesEmptyTree(t *testing.T) {
	s := migrated(t)
	mustSaveItems(t, s, hnStory())
	if err := s.SaveComments(t.Context(), "hn:algolia:1001", nil); err != nil {
		t.Fatalf("empty save: %v", err)
	}
	if ok, err := s.DetailFetched(t.Context(), "hn:algolia:1001"); err != nil || !ok {
		t.Fatalf("empty tree: DetailFetched = %v, %v; want true, nil", ok, err)
	}
}

// The marker rides the tree's own transaction: a save that fails
// (unknown item) must not leave a marker behind — opened and tree
// always agree.
func TestDetailFetchedAtomicWithTree(t *testing.T) {
	s := migrated(t)
	if err := s.SaveComments(t.Context(), "hn:algolia:9999", treeFixture()); err == nil {
		t.Fatal("want FK error for unknown item")
	}
	if ok, err := s.DetailFetched(t.Context(), "hn:algolia:9999"); err != nil || ok {
		t.Fatalf("failed save must not mark opened: %v, %v", ok, err)
	}
}

// --- ticket 06: the zhihu question's detail caches ------------------------

// zhihuQuestion is one cached question card the detail tests hang
// the comment tree and the meta triple off.
func zhihuQuestion() core.Item {
	return core.Item{
		ID: "zh:question/1001", Source: "zhihu", Feed: "hot",
		Title: "热门问题", URL: "https://www.zhihu.com/question/1001",
		FetchedAt: nowUTC(),
	}
}

// answerTree is two zhihu answer floors — the first with a loaded
// comment subtree, the second bare — in tree order, the way the
// detail fetch and the merges produce them. NumChildren is the
// floor-head's server-reported comment count (nil for the second
// floor's degenerate shape).
func answerTree() []core.Comment {
	seven := 7
	return []core.Comment{
		{ID: "9001", ParentID: "", Author: "张三", Text: "<p>回答甲</p>", NumChildren: &seven},
		{ID: "c1", ParentID: "9001", Author: "李四", Text: "根评论一"},
		{ID: "c2", ParentID: "c1", Author: "王五", Text: "子回复"},
		{ID: "9002", ParentID: "", Author: "赵六", Text: "<p>回答乙</p>"},
	}
}

// num_children is snapshot data that must survive the round trip —
// the floor head's "N 条评论" renders from cache on offline re-opens,
// before (and without) the comment layer ever being opened.
func TestCommentsRoundTripPreservesNumChildren(t *testing.T) {
	s := migrated(t)
	mustSaveItems(t, s, zhihuQuestion())
	if err := s.SaveComments(t.Context(), "zh:question/1001", answerTree()); err != nil {
		t.Fatalf("SaveComments: %v", err)
	}
	got, err := s.CommentsForItem(t.Context(), "zh:question/1001")
	if err != nil {
		t.Fatalf("CommentsForItem: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d comments, want 4", len(got))
	}
	if got[0].NumChildren == nil || *got[0].NumChildren != 7 {
		t.Fatalf("floor 9001 num_children = %v, want 7", got[0].NumChildren)
	}
	if got[3].NumChildren != nil {
		t.Fatalf("floor 9002 num_children = %v, want nil", got[3].NumChildren)
	}
	if got[1].NumChildren != nil {
		t.Fatalf("reply num_children = %v, want nil (hn-shaped nodes keep nil)", got[1].NumChildren)
	}
}

// The question meta triple caches onto the opened marker's row and
// survives every later whole-tree replace (Ctrl+N appends re-save
// the tree constantly): SaveComments' upsert must not clobber it.
func TestQuestionInfoSurvivesTreeReplaces(t *testing.T) {
	s := migrated(t)
	mustSaveItems(t, s, zhihuQuestion())

	if err := s.SaveComments(t.Context(), "zh:question/1001", answerTree()); err != nil {
		t.Fatalf("save tree: %v", err)
	}
	q := core.QuestionInfo{Author: "问主", AnswerCount: 1234, VisitCount: 5600000}
	if err := s.SaveQuestionInfo(t.Context(), "zh:question/1001", q); err != nil {
		t.Fatalf("SaveQuestionInfo: %v", err)
	}
	// A later replace (more floors, the background refresh): the
	// triple stays — it is the question's, not the tree's.
	if err := s.SaveComments(t.Context(), "zh:question/1001", answerTree()); err != nil {
		t.Fatalf("re-save tree: %v", err)
	}
	got, ok, err := s.QuestionInfoForItem(t.Context(), "zh:question/1001")
	if err != nil || !ok {
		t.Fatalf("QuestionInfoForItem = %v, %v; want true, nil", ok, err)
	}
	if got != q {
		t.Fatalf("question info = %+v, want %+v", got, q)
	}
}

// A never-opened question has no meta triple: ok=false, zero value,
// no error — the caller renders without the meta line.
func TestQuestionInfoAbsentWhenNeverOpened(t *testing.T) {
	s := migrated(t)
	mustSaveItems(t, s, zhihuQuestion())
	got, ok, err := s.QuestionInfoForItem(t.Context(), "zh:question/1001")
	if err != nil || ok {
		t.Fatalf("QuestionInfoForItem = %+v, %v, %v; want zero, false, nil", got, ok, err)
	}
}

// A corrupt cached triple (a hand-edited row, a shape moved) must
// read as absent, not crash the open — the meta line degrades away.
func TestQuestionInfoCorruptRowReadsAbsent(t *testing.T) {
	s := migrated(t)
	mustSaveItems(t, s, zhihuQuestion())
	if err := s.SaveComments(t.Context(), "zh:question/1001", nil); err != nil {
		t.Fatalf("open the row: %v", err)
	}
	if _, err := s.Exec(t.Context(),
		`UPDATE detail_fetch SET question = '{not json' WHERE item_id = 'zh:question/1001'`); err != nil {
		t.Fatalf("corrupt the row: %v", err)
	}
	_, ok, err := s.QuestionInfoForItem(t.Context(), "zh:question/1001")
	if err != nil || ok {
		t.Fatalf("corrupt row: QuestionInfoForItem = %v, %v; want false, nil", ok, err)
	}
}
