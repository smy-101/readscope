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
