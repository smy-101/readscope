package hn

// Ticket 03 tests: the DetailSource extension. hn fetches a story's
// whole comment tree from /items/<id> in one request; these tests pin
// the fetch seam against a fake Algolia (fixture JSON, offline):
// tree flattening with parent links, deleted placeholders, empty
// trees, plain text, and error paths. The persistence composition
// lives in internal/refresh (TestDetail*), the DAO in internal/store.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
)

// storyFixture is one Algolia /items/<id> payload: a link post whose
// tree has every shape ticket 03 must survive —
//
//	floor 10 "root comment"            (top level → parent "")
//	  reply 11 "<i>nested</i> html"    (depth 2)
//	    reply 12 "deep"                (depth 3: parent is 11, not 10)
//	floor 13 [deleted]                  (placeholder: no author/text, children stay attached)
//	  reply 14 "child of deleted"      (parent 13 must survive)
//	floor 15 "plain text, no html"
const storyFixture = `{
  "id": 1001, "type": "story", "author": "alice", "title": "Why link posts win",
  "url": "https://example.com/article", "text": null, "points": 120,
  "created_at": "2023-11-14T22:13:20.000Z", "created_at_i": 1700000000,
  "parent_id": null, "children": [
    {"id": 10, "type": "comment", "author": "bob",
     "text": "root comment", "points": null, "created_at_i": 1700000100,
     "parent_id": 1001, "children": [
       {"id": 11, "type": "comment", "author": "carol",
        "text": "<i>nested</i> html", "points": null, "created_at_i": 1700000200,
        "parent_id": 10, "children": [
          {"id": 12, "type": "comment", "author": "dave",
           "text": "deep", "points": null, "created_at_i": 1700000300,
           "parent_id": 11, "children": []}
        ]}
     ]},
    {"id": 13, "type": "comment", "deleted": true,
     "text": null, "points": null, "created_at_i": 1700000150,
     "parent_id": 1001, "children": [
       {"id": 14, "type": "comment", "author": "erin",
        "text": "child of deleted", "points": null, "created_at_i": 1700000250,
        "parent_id": 13, "children": []}
     ]},
    {"id": 15, "type": "comment", "author": "frank",
     "text": "plain text, no html", "points": null, "created_at_i": 1700000050,
     "parent_id": 1001, "children": []}
  ]
}`

// emptyTreeFixture: a story with no comments at all.
const emptyTreeFixture = `{
  "id": 2002, "type": "story", "author": "gina", "title": "Quiet story",
  "url": "", "text": "<p>Text post body</p>", "points": 1,
  "created_at_i": 1700000000, "parent_id": null, "children": []
}`

// detailFake serves /items/<id> fixtures keyed by story id and counts
// requests, so tests can pin "one request per Detail".
type detailFake struct {
	*httptest.Server
	hits int
}

func detailFakeFor(t *testing.T, byStory map[string]string, status map[string]int) *detailFake {
	t.Helper()
	f := &detailFake{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/items/") {
			http.Error(w, "not the items endpoint", http.StatusNotFound)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/items/")
		f.hits++
		if code, ok := status[id]; ok {
			http.Error(w, "boom", code)
			return
		}
		body, ok := byStory[id]
		if !ok {
			http.Error(w, "no fixture", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

// storyItem builds the list-layer item a Detail call receives.
func storyItem(id string) core.Item {
	return core.Item{ID: ItemID(id), Source: ID, Feed: "front", Title: "story " + id}
}

func TestSourceImplementsDetailSource(t *testing.T) {
	// The optional extension lands as a type assertion, never a
	// registry entry: callers probe, sources that lack it (rss) are
	// untouched.
	s := newSource("http://127.0.0.1:1")
	if _, ok := any(s).(source.DetailSource); !ok {
		t.Fatal("hn.Source must implement source.DetailSource")
	}
}

func TestDetailFetchesWholeTreeInOneRequest(t *testing.T) {
	f := detailFakeFor(t, map[string]string{"1001": storyFixture}, nil)
	s := newSource(f.URL + "/api/v1")

	c, err := s.Detail(testCtx(), storyItem("1001"))
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if f.hits != 1 {
		t.Fatalf("got %d requests, want exactly 1 (whole tree per call)", f.hits)
	}

	// Tree order: floors in payload order, each floor's subtree
	// immediately after it (DFS pre-order), parents strictly before
	// children — the order the cache stores and the UI flattens from.
	wantIDs := []string{"10", "11", "12", "13", "14", "15"}
	wantParents := map[string]string{"10": "", "11": "10", "12": "11", "13": "", "14": "13", "15": ""}
	if len(c.Comments) != len(wantIDs) {
		t.Fatalf("got %d comments, want %d", len(c.Comments), len(wantIDs))
	}
	for i, cm := range c.Comments {
		if cm.ID != wantIDs[i] {
			t.Fatalf("comment[%d].ID = %q, want %q (tree order)", i, cm.ID, wantIDs[i])
		}
		if cm.ParentID != wantParents[cm.ID] {
			t.Fatalf("comment %s parent = %q, want %q", cm.ID, cm.ParentID, wantParents[cm.ID])
		}
	}
}

func TestDetailKeepsDeletedPlaceholderAttached(t *testing.T) {
	// A deleted comment loses author and text but keeps its node, so
	// its children's parent links still resolve.
	f := detailFakeFor(t, map[string]string{"1001": storyFixture}, nil)
	s := newSource(f.URL + "/api/v1")

	c, err := s.Detail(testCtx(), storyItem("1001"))
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	var deleted *core.Comment
	for i := range c.Comments {
		if c.Comments[i].ID == "13" {
			deleted = &c.Comments[i]
		}
	}
	if deleted == nil {
		t.Fatal("deleted comment 13 must survive as a placeholder node")
	}
	if deleted.Author != "" || deleted.Text != "" {
		t.Fatalf("deleted placeholder kept content: author=%q text=%q", deleted.Author, deleted.Text)
	}
	for _, cm := range c.Comments {
		if cm.ID == "14" && cm.ParentID != "13" {
			t.Fatalf("child of deleted re-parented: parent=%q", cm.ParentID)
		}
	}
}

func TestDetailEmptyTreeIsZeroComments(t *testing.T) {
	f := detailFakeFor(t, map[string]string{"2002": emptyTreeFixture}, nil)
	s := newSource(f.URL + "/api/v1")

	c, err := s.Detail(testCtx(), storyItem("2002"))
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(c.Comments) != 0 {
		t.Fatalf("got %d comments, want 0", len(c.Comments))
	}
}

func TestDetailCarriesCommentFields(t *testing.T) {
	f := detailFakeFor(t, map[string]string{"1001": storyFixture}, nil)
	s := newSource(f.URL + "/api/v1")

	c, err := s.Detail(testCtx(), storyItem("1001"))
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	first := c.Comments[0] // "10" bob "root comment"
	if first.Author != "bob" || first.Text != "root comment" {
		t.Fatalf("field mapping: author=%q text=%q", first.Author, first.Text)
	}
	if first.Score != nil {
		t.Fatalf("HN comments carry no points in Algolia's tree API; got score %v", *first.Score)
	}
	if first.CreatedAt == nil || !first.CreatedAt.Equal(time.Unix(1700000100, 0).UTC()) {
		t.Fatalf("created_at = %v, want 1700000100", first.CreatedAt)
	}
	// Plain text (no HTML) passes through verbatim — it is a snapshot,
	// the render pipeline decides how to lay it out.
	if last := c.Comments[len(c.Comments)-1]; last.Text != "plain text, no html" {
		t.Fatalf("plain text mangled: %q", last.Text)
	}
}

func TestDetailErrorsNameTheStory(t *testing.T) {
	f := detailFakeFor(t, map[string]string{"1001": storyFixture},
		map[string]int{"3003": http.StatusNotFound, "4004": http.StatusInternalServerError})
	s := newSource(f.URL + "/api/v1")

	for _, id := range []string{"3003", "4004"} {
		_, err := s.Detail(testCtx(), storyItem(id))
		if err == nil {
			t.Fatalf("story %s: want error, got none", id)
		}
		if !strings.Contains(err.Error(), id) {
			t.Fatalf("error %q must name the story id", err)
		}
	}
}

func TestDetailRejectsForeignItemID(t *testing.T) {
	// Detail receives items by their deterministic id; anything not
	// "hn:algolia:<storyID>" is a programming error, named loudly.
	s := newSource("http://127.0.0.1:1")
	_, err := s.Detail(testCtx(), core.Item{ID: "rss:ruanyq:x", Source: "rss"})
	if err == nil || !strings.Contains(err.Error(), "rss:ruanyq:x") {
		t.Fatalf("want error naming the id, got %v", err)
	}
}

func testCtx() context.Context { return context.Background() }

// CommentURL (ticket 04) points Ctrl+O/Ctrl+Y at the HN page of one
// comment — or of the story itself when handed the story's item id,
// which is how the reader's body layer targets text posts (they carry
// no external URL).
func TestCommentURL(t *testing.T) {
	s := newSource("http://127.0.0.1:1")
	for _, tc := range []struct{ in, want string }{
		{"12345", "https://news.ycombinator.com/item?id=12345"},
		{ItemID("1001"), "https://news.ycombinator.com/item?id=1001"},
	} {
		if got := s.CommentURL(tc.in); got != tc.want {
			t.Errorf("CommentURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
