package store

import (
	"testing"
	"time"
)

// The reading-progress seam (M6 ticket 03): the reader saves the
// 续读位 anchor (spine_index, block_offset) plus a display-only pct
// on chapter boundaries and exit; the shelf row and the re-open read
// it back through GetProgress. Bookmarks ride the same book row:
// add at an anchor (note optional), list newest-first, delete by
// anchor — the PK is (book, spine, block), so re-adding at the same
// anchor refreshes the note in place.
func TestProgressRoundTrip(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	now := nowUTC()
	id, _, err := s.UpsertBook(ctx, "/books/a.epub", "书甲", "", "", now)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Never opened: ok=false, zero Progress, no error.
	if p, ok, err := s.GetProgress(ctx, id); err != nil || ok || p != (Progress{}) {
		t.Fatalf("GetProgress on unread book = %+v ok=%v err=%v", p, ok, err)
	}

	// First save: chapter 0, block 3.
	if err := s.SaveProgress(ctx, id, 0, 3, 0.04, now); err != nil {
		t.Fatalf("save: %v", err)
	}
	p, ok, err := s.GetProgress(ctx, id)
	if err != nil || !ok {
		t.Fatalf("GetProgress after save: ok=%v err=%v", ok, err)
	}
	if p.SpineIndex != 0 || p.BlockOffset != 3 || p.Pct != 0.04 || !p.UpdatedAt.Equal(now) {
		t.Fatalf("round trip = %+v", p)
	}

	// A later session advances to chapter 7, block 12: the row is
	// overwritten (PK book_id), not appended.
	later := now.Add(24 * time.Hour)
	if err := s.SaveProgress(ctx, id, 7, 12, 0.63, later); err != nil {
		t.Fatalf("advance: %v", err)
	}
	p, ok, err = s.GetProgress(ctx, id)
	if err != nil || !ok {
		t.Fatalf("GetProgress after advance: ok=%v err=%v", ok, err)
	}
	if p.SpineIndex != 7 || p.BlockOffset != 12 || p.Pct != 0.63 || !p.UpdatedAt.Equal(later) {
		t.Fatalf("advance = %+v", p)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM progress`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("progress rows = %d err=%v (upsert must not append)", n, err)
	}
}

func TestBookmarkAddListDelete(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	now := nowUTC()
	id, _, err := s.UpsertBook(ctx, "/books/a.epub", "书甲", "", "", now)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Two bookmarks, the later one added last: list is newest first.
	if err := s.AddBookmark(ctx, id, 0, 3, "开篇金句", now); err != nil {
		t.Fatalf("add 1: %v", err)
	}
	if err := s.AddBookmark(ctx, id, 7, 12, "", now.Add(time.Minute)); err != nil {
		t.Fatalf("add 2: %v", err)
	}
	bms, err := s.Bookmarks(ctx, id)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(bms) != 2 {
		t.Fatalf("bookmarks = %d, want 2", len(bms))
	}
	if bms[0].SpineIndex != 7 || bms[0].Note != "" {
		t.Fatalf("newest first violated: %+v", bms[0])
	}
	if bms[1].SpineIndex != 0 || bms[1].Note != "开篇金句" {
		t.Fatalf("oldest second: %+v", bms[1])
	}

	// Re-add at the same anchor: note refreshes, created_at keeps the
	// original slot (the position is the identity, not the edit).
	if err := s.AddBookmark(ctx, id, 7, 12, "回来这里", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	bms, err = s.Bookmarks(ctx, id)
	if err != nil {
		t.Fatalf("list after re-add: %v", err)
	}
	if len(bms) != 2 || bms[0].Note != "回来这里" || !bms[0].CreatedAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("re-add must refresh note in place: %+v", bms)
	}

	// Delete by anchor removes exactly that row.
	if err := s.DeleteBookmark(ctx, id, 0, 3); err != nil {
		t.Fatalf("delete: %v", err)
	}
	bms, err = s.Bookmarks(ctx, id)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(bms) != 1 || bms[0].SpineIndex != 7 {
		t.Fatalf("delete removed the wrong row: %+v", bms)
	}
}
