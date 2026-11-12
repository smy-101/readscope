package refresh

// body.go — the lazy-body composition (ticket 03), the fetch-then-
// persist shape of Detail() for one opened item whose full text does
// NOT ride the list snapshot: probe the source for the optional
// BodySource extension, fetch the body, persist it into the items
// body column. The cache-first read is the item row itself — the UI
// renders it on open and only calls this when the snapshot is empty,
// so a re-open never dials again (二次打开渲染缓存零请求).

import (
	"context"
	"fmt"

	"github.com/smy-101/readscope/internal/core"
	"github.com/smy-101/readscope/internal/source"
	"github.com/smy-101/readscope/internal/store"
)

// Body fetches one item's full text from its source and caches it
// into the body snapshot. A source without the BodySource extension
// (rss and hn: their bodies ride the list snapshot already) is a
// clean named error. A failed fetch leaves the cache untouched.
func Body(ctx context.Context, st *store.Store, srcs *core.Sources, it core.Item) (string, error) {
	src, err := source.New(it.Source, srcs)
	if err != nil {
		return "", err
	}
	bs, ok := src.(source.BodySource)
	if !ok {
		return "", fmt.Errorf("source %q provides no on-demand body", it.Source)
	}
	body, err := bs.FetchBody(ctx, it)
	if err != nil {
		return "", err
	}
	if err := st.SaveBody(ctx, it.ID, body); err != nil {
		return "", fmt.Errorf("cache body %s: %w", it.ID, err)
	}
	return body, nil
}
