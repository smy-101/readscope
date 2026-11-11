// floors.go: the pure half of the three-layer detail (ticket 04).
// The cached comment tree arrives in seq order — floors in payload
// order, each followed by its subtree, parents strictly before
// children (migration 005's structural invariant) — so both passes
// below are single forward scans: no sorting, no recursion over an
// intermediate tree structure.

package ui

import "github.com/smy-101/readscope/internal/core"

// floor is one top-level comment (GLOSSARY: 楼层) with its whole
// reply subtree (回复树) kept in tree order, ready to render as one
// screen of the floor layer.
type floor struct {
	cm      core.Comment   // the top-level comment itself
	replies []core.Comment // every descendant, tree order (parents before children)
}

// replyRow is one row of the flattened reply layer: depth 1 is a
// direct reply of the floor, depth 2 is every deeper reply compressed
// into the second visual level. parentAuthor names the immediate
// parent for the @parent prefix; it is "" when the parent is a
// deleted node (no author), and the renderer substitutes the
// [deleted] placeholder.
type replyRow struct {
	cm           core.Comment
	depth        int // 1 | 2
	parentAuthor string
}

// buildFloors splits a cached tree into floors. One forward scan with
// a comment-id → floor-index map (indexes, not pointers: appends to
// the growing slice reallocate, and a pointer taken before an orphan
// promotion would write replies into a dead copy): ParentID "" opens
// a new floor, anything else appends to its parent's floor. A reply
// whose parent id matches nothing (impossible from a real fetch,
// possible from a corrupt cache) is promoted to its own floor — a
// cached comment is never silently dropped.
func buildFloors(tree []core.Comment) []floor {
	owner := make(map[string]int, len(tree))
	floors := make([]floor, 0)
	for _, c := range tree {
		idx, known := owner[c.ParentID]
		if c.ParentID == "" || !known {
			floors = append(floors, floor{cm: c}) // top-level or promoted orphan
			owner[c.ID] = len(floors) - 1
			continue
		}
		flo := &floors[idx]
		flo.replies = append(flo.replies, c)
		owner[c.ID] = idx
	}
	return floors
}

// flattenReplies compresses one floor's reply tree into the two-level
// row sequence of the reply layer: direct replies at depth 1, every
// deeper reply at depth 2 carrying its immediate parent's author for
// the @parent prefix. One forward scan with an id → depth map (the
// tree order guarantees the parent was seen first); the floor comment
// itself is depth 0 and never appears as a row.
func flattenReplies(f *floor) []replyRow {
	depth := map[string]int{f.cm.ID: 0}
	author := map[string]string{f.cm.ID: f.cm.Author}
	rows := make([]replyRow, 0, len(f.replies))
	for _, c := range f.replies {
		d := depth[c.ParentID] + 1
		depth[c.ID], author[c.ID] = d, c.Author
		row := replyRow{cm: c, depth: d}
		if d >= 2 {
			row.depth = 2 // deeper levels compress into the second
			row.parentAuthor = author[c.ParentID]
		}
		rows = append(rows, row)
	}
	return rows
}
