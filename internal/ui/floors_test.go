package ui

// Ticket 04 pure-function tests: the two-layer flattening. The cached
// tree arrives in seq order (floors in payload order, each followed by
// its subtree, parents strictly before children — ticket 03's
// migration 005 invariant), and these tests only feed trees that keep
// it, because that is the only shape the store can produce. External
// behaviour under test: floors split at top level, replies compressed
// to two visual levels with @parent pointing at the immediate parent.

import (
	"testing"

	"github.com/smy-101/readscope/internal/core"
)

// cm builds one comment; parent "" = top-level floor.
func cm(id, parent, author, text string) core.Comment {
	return core.Comment{ID: id, ParentID: parent, Author: author, Text: text}
}

// tree is a hand-written forest in seq order (parents before children).
// Shape:
//
//	f1 ─ r11 ─ r111 ─ r1111      (deep chain under floor 1)
//	   └ r12                     (second direct reply)
//	f2 ─ r21                     (one direct reply)
//	f3                            (floor without replies)
//	f4 ─ r41 (deleted) ─ r411    (deleted mid-node keeps children)
var deepTree = []core.Comment{
	cm("f1", "", "alice", "floor one"),
	cm("r11", "f1", "bob", "reply 1-1"),
	cm("r111", "r11", "carol", "reply 1-1-1"),
	cm("r1111", "r111", "dave", "reply 1-1-1-1"),
	cm("r12", "f1", "erin", "reply 1-2"),
	cm("f2", "", "frank", "floor two"),
	cm("r21", "f2", "grace", "reply 2-1"),
	cm("f3", "", "heidi", "floor three"),
	cm("f4", "", "ivan", "floor four"),
	cm("r41", "f4", "", ""), // deleted placeholder: no author, no text
	cm("r411", "r41", "judy", "reply under deleted"),
}

func TestBuildFloorsSplitsTopLevel(t *testing.T) {
	floors := buildFloors(deepTree)
	if len(floors) != 4 {
		t.Fatalf("floors = %d, want 4 (one per top-level comment)", len(floors))
	}
	want := []struct {
		id      string
		author  string
		replies int
	}{
		{"f1", "alice", 4},
		{"f2", "frank", 1},
		{"f3", "heidi", 0},
		{"f4", "ivan", 2},
	}
	for i, w := range want {
		f := floors[i]
		if f.cm.ID != w.id || f.cm.Author != w.author {
			t.Errorf("floor %d = %s/%s, want %s/%s", i, f.cm.ID, f.cm.Author, w.id, w.author)
		}
		if len(f.replies) != w.replies {
			t.Errorf("floor %s: replies = %d, want %d (whole subtree)", w.id, len(f.replies), w.replies)
		}
	}
}

func TestBuildFloorsKeepsTreeOrder(t *testing.T) {
	floors := buildFloors(deepTree)
	// f1's subtree in DFS pre-order: r11, r111, r1111, r12.
	got := ids(floors[0].replies)
	want := []string{"r11", "r111", "r1111", "r12"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("floor f1 replies = %v, want %v", got, want)
		}
	}
}

func ids(cs []core.Comment) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

func TestBuildFloorsSingleChain(t *testing.T) {
	tree := []core.Comment{
		cm("f", "", "alice", "floor"),
		cm("r1", "f", "bob", ""),
		cm("r2", "r1", "carol", ""),
		cm("r3", "r2", "dave", ""),
		cm("r4", "r3", "erin", ""),
		cm("r5", "r4", "frank", ""),
	}
	floors := buildFloors(tree)
	if len(floors) != 1 || len(floors[0].replies) != 5 {
		t.Fatalf("single chain = %d floors / %d replies, want 1/5", len(floors), len(floors[0].replies))
	}
}

func TestBuildFloorsEmptyTree(t *testing.T) {
	for _, tree := range [][]core.Comment{nil, {}} {
		if floors := buildFloors(tree); len(floors) != 0 {
			t.Fatalf("empty tree must flatten to no floors, got %d", len(floors))
		}
	}
}

// A reply whose parent id matches nothing (corrupt cache — the seq
// invariant makes this impossible from a real fetch) is promoted to
// its own floor rather than dropped: never lose a cached comment.
func TestBuildFloorsPromotesOrphan(t *testing.T) {
	tree := []core.Comment{
		cm("f1", "", "alice", "floor"),
		cm("ghost-parent-child", "nope", "bob", "orphan"),
	}
	floors := buildFloors(tree)
	if len(floors) != 2 {
		t.Fatalf("orphan must be promoted, floors = %d", len(floors))
	}
	if floors[1].cm.ID != "ghost-parent-child" || len(floors[1].replies) != 0 {
		t.Fatalf("promoted floor = %+v", floors[1])
	}
}

// An orphan promotion mid-subtree must not strand the floors already
// built: with the naive pointer-into-slice map, the reallocation the
// promotion triggers leaves earlier floors' map entries pointing at
// dead copies, so a later reply to floor 1 would vanish. The
// index-based map keeps every append landing in the live slice.
func TestBuildFloorsOrphanDoesNotStrandEarlierFloor(t *testing.T) {
	tree := []core.Comment{
		cm("f1", "", "alice", "floor one"),
		cm("r11", "f1", "bob", ""),
		cm("ghost", "nope", "mallory", "orphan"),
		cm("r12", "f1", "erin", ""), // after the orphan: still floor 1's
	}
	floors := buildFloors(tree)
	if len(floors) != 2 {
		t.Fatalf("floors = %d, want 2 (f1 + promoted orphan)", len(floors))
	}
	if got := ids(floors[0].replies); len(got) != 2 || got[0] != "r11" || got[1] != "r12" {
		t.Fatalf("floor 1 replies = %v, want [r11 r12] — r12 was stranded", got)
	}
	if floors[1].cm.ID != "ghost" {
		t.Fatalf("promoted orphan = %s, want ghost", floors[1].cm.ID)
	}
}

func TestFlattenRepliesTwoLayersWithParentPrefix(t *testing.T) {
	floors := buildFloors(deepTree)
	rows := flattenReplies(&floors[0]) // f1: r11, r111, r1111, r12
	want := []replyRow{
		{cm("r11", "f1", "bob", "reply 1-1"), 1, ""},
		{cm("r111", "r11", "carol", "reply 1-1-1"), 2, "bob"},
		{cm("r1111", "r111", "dave", "reply 1-1-1-1"), 2, "carol"},
		{cm("r12", "f1", "erin", "reply 1-2"), 1, ""},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d: %+v", len(rows), len(want), rows)
	}
	for i, w := range want {
		if rows[i].cm.ID != w.cm.ID || rows[i].depth != w.depth || rows[i].parentAuthor != w.parentAuthor {
			t.Errorf("row %d = {%s depth=%d parent=%q}, want {%s depth=%d parent=%q}",
				i, rows[i].cm.ID, rows[i].depth, rows[i].parentAuthor, w.cm.ID, w.depth, w.parentAuthor)
		}
	}
}

// A deleted mid-node has no author; its children's @parent prefix
// carries the empty author through for the renderer to replace with
// the [deleted] placeholder.
func TestFlattenRepliesDeletedParentHasEmptyPrefix(t *testing.T) {
	floors := buildFloors(deepTree)
	rows := flattenReplies(&floors[3]) // f4: r41 (deleted), r411
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[1].cm.ID != "r411" || rows[1].depth != 2 || rows[1].parentAuthor != "" {
		t.Fatalf("reply under deleted = {%s depth=%d parent=%q}, want depth 2 empty parent",
			rows[1].cm.ID, rows[1].depth, rows[1].parentAuthor)
	}
}

func TestFlattenRepliesEmpty(t *testing.T) {
	floors := buildFloors(deepTree)
	if rows := flattenReplies(&floors[2]); len(rows) != 0 { // f3: no replies
		t.Fatalf("floor without replies must flatten to no rows, got %d", len(rows))
	}
}
