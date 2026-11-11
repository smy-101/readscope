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
