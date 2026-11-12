// detail.go: the detail-layer composition (ticket 03), the same
// fetch-then-persist shape as Source() but for one opened item: probe
// the source for the optional DetailSource extension, fetch the whole
// comment tree, snapshot it through the store's comments DAO
// (whole-tree replace). The cache-first read is the store's
// CommentsForItem — this function is the online half, what a
// background refresh or a first open calls.
//
// Ticket 06 grows the composition for sources whose detail layers
// page on the network (source.DetailPager — zhihu questions): the
// open's Content may carry a body (persisted into the body snapshot)
// and a question meta triple (cached onto the detail_fetch row), and
// the fresh first page MERGES with the cached tree instead of
// replacing it — a refresh must not unload the answers Ctrl+N paged
// in or the comment subtrees already read. hn's single-request tree
// still replaces whole (nothing can be lost that way). The two
// Ctrl+N compositions — one more floors page, one more comments page
// — fold their page into the cache the same way.

package refresh

import (
	"context"
	"fmt"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
	"github.com/smy-101/readscope/internal/store"
)

// Detail fetches one item's detail layer from its source and caches
// it. The returned Content is what was fetched and merged — the
// caller renders it or re-reads the cache. A source without the
// DetailSource extension (rss: its body already rides the list
// snapshot) is a clean named error. A failed fetch leaves the cache
// untouched, so a previously opened post keeps reading offline.
func Detail(ctx context.Context, st *store.Store, srcs *core.Sources, it core.Item) (core.Content, error) {
	src, err := source.New(it.Source, srcs)
	if err != nil {
		return core.Content{}, err
	}
	ds, ok := src.(source.DetailSource)
	if !ok {
		return core.Content{}, fmt.Errorf("source %q provides no detail layer", it.Source)
	}
	c, err := ds.Detail(ctx, it)
	if err != nil {
		return core.Content{}, err
	}
	// A paging detail source merges with the cache: its first page is
	// a window, not the whole discussion, and a background refresh
	// must not unload what the reader already paged into.
	if _, ok := src.(source.DetailPager); ok {
		if old, err := st.CommentsForItem(ctx, it.ID); err == nil {
			c.Comments = mergeDetailTree(old, c.Comments)
		}
	}
	if err := st.SaveComments(ctx, it.ID, c.Comments); err != nil {
		return core.Content{}, fmt.Errorf("cache comments %s: %w", it.ID, err)
	}
	if c.Body != "" {
		if err := st.SaveBody(ctx, it.ID, c.Body); err != nil {
			return core.Content{}, fmt.Errorf("cache body %s: %w", it.ID, err)
		}
	}
	if c.Question != nil {
		if err := st.SaveQuestionInfo(ctx, it.ID, *c.Question); err != nil {
			return core.Content{}, fmt.Errorf("cache question info %s: %w", it.ID, err)
		}
	}
	return c, nil
}

// DetailFloorsPage fetches one more floors page (the question layer's
// Ctrl+N) and folds it into the cache: new floors append at the tail,
// ids the cache already holds skip whole (the server re-serves
// answers across pages — 去重防排序抖动), and the merged tree is what
// both the cache and the caller now hold. next is the cursor of the
// page after this one ("" = the walk's end).
func DetailFloorsPage(ctx context.Context, st *store.Store, srcs *core.Sources, it core.Item, cursor string) ([]core.Comment, string, error) {
	dp, err := detailPagerOf(srcs, it)
	if err != nil {
		return nil, "", err
	}
	page, next, err := dp.FetchFloorsPage(ctx, it, cursor)
	if err != nil {
		return nil, "", err
	}
	old, err := st.CommentsForItem(ctx, it.ID)
	if err != nil {
		return nil, "", fmt.Errorf("read cache %s: %w", it.ID, err)
	}
	merged := appendFloors(old, page)
	if err := st.SaveComments(ctx, it.ID, merged); err != nil {
		return nil, "", fmt.Errorf("cache floors %s: %w", it.ID, err)
	}
	return merged, next, nil
}

// DetailCommentsPage fetches one answer's comments page (the comment
// layer's first entry and its Ctrl+N) and folds it into the cache
// under that answer floor: nodes the cache already holds skip whole,
// the merged tree is what the cache and the caller now hold.
// nextOffset is the next page's position (-1 = the answer's comments
// are exhausted).
func DetailCommentsPage(ctx context.Context, st *store.Store, srcs *core.Sources, it core.Item, answerID string, offset int) ([]core.Comment, int, error) {
	dp, err := detailPagerOf(srcs, it)
	if err != nil {
		return nil, -1, err
	}
	page, next, err := dp.FetchAnswerComments(ctx, it, answerID, offset)
	if err != nil {
		return nil, -1, err
	}
	old, err := st.CommentsForItem(ctx, it.ID)
	if err != nil {
		return nil, -1, fmt.Errorf("read cache %s: %w", it.ID, err)
	}
	merged, err := appendAnswerComments(old, answerID, page)
	if err != nil {
		return nil, -1, err
	}
	if err := st.SaveComments(ctx, it.ID, merged); err != nil {
		return nil, -1, fmt.Errorf("cache comments %s: %w", it.ID, err)
	}
	return merged, next, nil
}

// detailPagerOf probes a source for the detail-paging extension.
func detailPagerOf(srcs *core.Sources, it core.Item) (source.DetailPager, error) {
	src, err := source.New(it.Source, srcs)
	if err != nil {
		return nil, err
	}
	dp, ok := src.(source.DetailPager)
	if !ok {
		return nil, fmt.Errorf("source %q pages no detail layers", it.Source)
	}
	return dp, nil
}

// mergeDetailTree folds a fresh first page into the cached tree: the
// fresh floors lead with fresh scores and counts, and every old
// floor the fresh page did not re-serve survives at the tail with
// the comment subtree the cache already holds. Nothing ever unloads
// — a re-rank that shuffles a Ctrl+N'd floor off page 1 must not
// drop it (client-side, "deleted upstream" and "re-ranked" are
// indistinguishable, and losing a read comment tree is the worse
// miss). A floor deleted upstream therefore lingers as a stale tail
// row: it was a real answer when fetched, and offline data stays
// readable.
func mergeDetailTree(old, fresh []core.Comment) []core.Comment {
	if len(old) == 0 {
		return fresh
	}
	oldFloors, repliesOf := splitFloors(old)
	freshIDs := make(map[string]bool, len(fresh))
	out := make([]core.Comment, 0, len(old)+len(fresh))
	for _, f := range fresh {
		freshIDs[f.ID] = true
		out = append(out, f)
		out = append(out, repliesOf[f.ID]...)
	}
	for _, of := range oldFloors {
		if freshIDs[of.ID] {
			continue // the fresh page's copy leads
		}
		out = append(out, of)
		out = append(out, repliesOf[of.ID]...)
	}
	return out
}

// appendFloors folds one fetched floors page into the cached tree:
// floors the cache already holds skip whole (the server happily
// re-serves an answer on a later page — the dedup that keeps a
// re-fetch from doubling a row), the rest append at the tail in page
// order — never interleaved, the tail is where the walk left off.
func appendFloors(old, page []core.Comment) []core.Comment {
	if len(page) == 0 {
		return old
	}
	held := make(map[string]bool, len(old))
	for _, c := range old {
		held[c.ID] = true
	}
	out := old
	for _, f := range page {
		if held[f.ID] || f.ParentID != "" {
			continue // already cached, or not a floor (defensive)
		}
		out = append(out, f)
	}
	return out
}

// appendAnswerComments folds one fetched comments page into the tree
// under its answer floor: nodes the cache already holds skip whole
// (a re-fetched root keeps its cached gap-completed children; new
// children of a cached root still land), the rest splice in right
// after the answer's existing subtree — tree order keeps parents
// strictly before children. An answer floor the cache does not hold
// is a wiring bug (the reader enters the comment layer from a floor
// on screen) and fails by name.
func appendAnswerComments(tree []core.Comment, answerID string, page []core.Comment) ([]core.Comment, error) {
	floorOf := make(map[string]int, len(tree))
	answerIdx := -1
	for i, c := range tree {
		if c.ParentID == "" {
			floorOf[c.ID] = i
			if c.ID == answerID {
				answerIdx = i
			}
			continue
		}
		if fi, ok := floorOf[c.ParentID]; ok {
			floorOf[c.ID] = fi // a reply's replies belong to the same floor
		}
	}
	if answerIdx < 0 {
		return nil, fmt.Errorf("append comments: answer %s is not a cached floor", answerID)
	}
	insertAt := answerIdx + 1 // splice right after the answer's subtree
	for i := answerIdx + 1; i < len(tree) && floorOf[tree[i].ID] == answerIdx; i++ {
		insertAt = i + 1
	}
	held := make(map[string]bool, len(tree))
	for _, c := range tree {
		held[c.ID] = true
	}
	fresh := make([]core.Comment, 0, len(page))
	for _, c := range page {
		if !held[c.ID] {
			fresh = append(fresh, c)
			held[c.ID] = true
		}
	}
	if len(fresh) == 0 {
		return tree, nil
	}
	out := make([]core.Comment, 0, len(tree)+len(fresh))
	out = append(out, tree[:insertAt]...)
	out = append(out, fresh...)
	out = append(out, tree[insertAt:]...)
	return out, nil
}

// splitFloors splits a cached tree into its top-level floors (in
// order) and a floor-id → subtree map, linking by parent chain so
// any parent-before-child order resolves (the same robustness
// buildFloors applies at the UI). Orphaned replies (a corrupt cache)
// drop.
func splitFloors(tree []core.Comment) ([]core.Comment, map[string][]core.Comment) {
	floors := make([]core.Comment, 0)
	repliesOf := make(map[string][]core.Comment)
	floorOf := make(map[string]string, len(tree)) // node id → its floor's id
	for _, c := range tree {
		if c.ParentID == "" {
			floors = append(floors, c)
			floorOf[c.ID] = c.ID
			continue
		}
		if fid, ok := floorOf[c.ParentID]; ok {
			repliesOf[fid] = append(repliesOf[fid], c)
			floorOf[c.ID] = fid
		}
	}
	return floors, repliesOf
}
