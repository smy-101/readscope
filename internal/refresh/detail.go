// detail.go: the detail-layer composition (ticket 03), the same
// fetch-then-persist shape as Source() but for one opened item: probe
// the source for the optional DetailSource extension, fetch the whole
// comment tree, snapshot it through the comments DAO (whole-tree
// replace). The cache-first read is the store's CommentsForItem —
// this function is the online half, what a background refresh or a
// first open calls.

package refresh

import (
	"context"
	"fmt"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
	"github.com/smy-101/readscope/internal/store"
)

// Detail fetches one item's comment tree from its source and caches
// it whole. The returned Content is what was fetched and saved — the
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
	if err := st.SaveComments(ctx, it.ID, c.Comments); err != nil {
		return core.Content{}, fmt.Errorf("cache comments %s: %w", it.ID, err)
	}
	return c, nil
}
