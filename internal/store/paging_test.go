package store

import (
	"context"
	"testing"
	"time"

	"github.com/smy-101/readscope/internal/core"
)

// Ticket 05: per-feed paging depth lives beside the conditional
// validators in feed_fetch_state — one row per (source, feed), either
// side writable without clobbering the other.
func TestPagesFetchedRoundtrip(t *testing.T) {
	s := openTemp(t)
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	ctx := context.Background()

	// A feed never paged has depth 0 — "missing" and "first page" must
	// stay distinguishable (Ctrl+N at depth 0 fetches page 0).
	if n, err := s.LoadPagesFetched(ctx, "hn", "front"); err != nil || n != 0 {
		t.Fatalf("missing depth = (%d, %v), want 0", n, err)
	}

	if err := s.SavePagesFetched(ctx, "hn", "front", 2); err != nil {
		t.Fatalf("SavePagesFetched: %v", err)
	}
	if n, err := s.LoadPagesFetched(ctx, "hn", "front"); err != nil || n != 2 {
		t.Fatalf("depth = (%d, %v), want 2", n, err)
	}

	// Validators on the same row survive a depth write untouched, and
	// a validator write never resets the depth.
	state := core.FetchState{ETag: `"v1"`, LastFetched: time.Now().UTC()}
	if err := s.SaveFetchState(ctx, "hn", "front", state); err != nil {
		t.Fatalf("SaveFetchState: %v", err)
	}
	if err := s.SavePagesFetched(ctx, "hn", "front", 3); err != nil {
		t.Fatalf("SavePagesFetched after validators: %v", err)
	}
	got, ok, err := s.LoadFetchState(ctx, "hn", "front")
	if err != nil || !ok || got.ETag != `"v1"` {
		t.Fatalf("validators after depth write = (%+v, %v, %v)", got, ok, err)
	}
	if n, err := s.LoadPagesFetched(ctx, "hn", "front"); err != nil || n != 3 {
		t.Fatalf("depth after validator write = (%d, %v), want 3", n, err)
	}
}

// M4 ticket 05: the cursor-paging position (zhihu's follow feed) is
// the third independent resident of the same row — the opaque
// paging.next of the next page to fetch. "" = never paged (Ctrl+N
// bootstraps by fetching the first page), a value = the exact resume
// position a restart continues from.
func TestCursorRoundtrip(t *testing.T) {
	s := openTemp(t)
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	ctx := context.Background()

	// A feed never paged has no cursor: "" and "stored cursor" must
	// stay distinguishable, exactly like depth 0 vs 1.
	if c, err := s.LoadCursor(ctx, "zhihu", "follow"); err != nil || c != "" {
		t.Fatalf("missing cursor = (%q, %v), want empty", c, err)
	}

	const next = "https://www.zhihu.com/api/v3/moments?limit=10&desktop=true&after_id=1912345678901234567"
	if err := s.SaveCursor(ctx, "zhihu", "follow", next); err != nil {
		t.Fatalf("SaveCursor: %v", err)
	}
	if c, err := s.LoadCursor(ctx, "zhihu", "follow"); err != nil || c != next {
		t.Fatalf("cursor = (%q, %v), want the stored paging.next", c, err)
	}

	// The cursor write never clobbers the row's other residents, and
	// neither of them resets the cursor: validators, depth and cursor
	// are three independent columns of one row.
	if err := s.SaveFetchState(ctx, "zhihu", "follow", core.FetchState{ETag: `"v1"`, LastFetched: time.Now().UTC()}); err != nil {
		t.Fatalf("SaveFetchState: %v", err)
	}
	if err := s.SavePagesFetched(ctx, "zhihu", "follow", 2); err != nil {
		t.Fatalf("SavePagesFetched: %v", err)
	}
	if c, err := s.LoadCursor(ctx, "zhihu", "follow"); err != nil || c != next {
		t.Fatalf("cursor after neighbor writes = (%q, %v), want unchanged", c, err)
	}
	if n, err := s.LoadPagesFetched(ctx, "zhihu", "follow"); err != nil || n != 2 {
		t.Fatalf("depth after cursor write = (%d, %v), want 2", n, err)
	}
	if err := s.SaveCursor(ctx, "zhihu", "follow", "c2"); err != nil {
		t.Fatalf("SaveCursor advance: %v", err)
	}
	if got, ok, err := s.LoadFetchState(ctx, "zhihu", "follow"); err != nil || !ok || got.ETag != `"v1"` {
		t.Fatalf("validators after cursor advance = (%+v, %v, %v)", got, ok, err)
	}
}
