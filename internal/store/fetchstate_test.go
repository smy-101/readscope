package store

import (
	"context"
	"testing"
	"time"

	"github.com/smy-101/readscope/internal/core"
)

func TestFetchStateRoundtrip(t *testing.T) {
	s := openTemp(t)
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	ctx := context.Background()

	// A feed never fetched has no state — the first request must be
	// unconditional, so "missing" has to be distinguishable from "empty".
	got, ok, err := s.LoadFetchState(ctx, "rss", "ex")
	if err != nil {
		t.Fatalf("LoadFetchState (missing): %v", err)
	}
	if ok || got != (core.FetchState{}) {
		t.Fatalf("missing state = (%+v, %v), want zero and false", got, ok)
	}

	st := core.FetchState{
		ETag:         `W/"abc123"`,
		LastModified: "Mon, 02 Jan 2006 15:04:05 GMT",
		LastFetched:  time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC),
	}
	if err := s.SaveFetchState(ctx, "rss", "ex", st); err != nil {
		t.Fatalf("SaveFetchState: %v", err)
	}
	got, ok, err = s.LoadFetchState(ctx, "rss", "ex")
	if err != nil || !ok {
		t.Fatalf("LoadFetchState: %v, %v", err, ok)
	}
	if got.ETag != st.ETag || got.LastModified != st.LastModified {
		t.Fatalf("validators = %+v, want %+v", got, st)
	}
	if !got.LastFetched.Equal(st.LastFetched) {
		t.Fatalf("LastFetched = %v, want %v", got.LastFetched, st.LastFetched)
	}

	// State is per (source, feed): another feed stays missing.
	if _, ok, _ := s.LoadFetchState(ctx, "rss", "other"); ok {
		t.Fatal("state leaked across feeds")
	}
	if _, ok, _ := s.LoadFetchState(ctx, "hn", "ex"); ok {
		t.Fatal("state leaked across sources")
	}

	// A 200 refresh overwrites validators in place (upsert): the next
	// conditional request must carry the new values, not the old ones.
	next := core.FetchState{ETag: `"v2"`, LastFetched: time.Now().UTC()}
	if err := s.SaveFetchState(ctx, "rss", "ex", next); err != nil {
		t.Fatalf("SaveFetchState (upsert): %v", err)
	}
	got, ok, err = s.LoadFetchState(ctx, "rss", "ex")
	if err != nil || !ok {
		t.Fatalf("LoadFetchState (upsert): %v, %v", err, ok)
	}
	if got.ETag != next.ETag || got.LastModified != "" {
		t.Fatalf("upsert = %+v, want etag %q and cleared last-modified", got, next.ETag)
	}
}

func TestFetchStateSurvivesReopen(t *testing.T) {
	path := t.TempDir() + "/test.db"
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	st := core.FetchState{ETag: `"persist"`, LastFetched: time.Now().UTC()}
	if err := s.SaveFetchState(context.Background(), "rss", "ex", st); err != nil {
		t.Fatalf("SaveFetchState: %v", err)
	}
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	got, ok, err := s2.LoadFetchState(context.Background(), "rss", "ex")
	if err != nil || !ok || got.ETag != st.ETag {
		t.Fatalf("state after reopen = (%+v, %v, %v)", got, ok, err)
	}
}
