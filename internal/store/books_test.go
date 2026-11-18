package store

import (
	"testing"
	"time"
)

// The books DAO's three jobs, pinned from ticket 02's acceptance: the
// upsert is idempotent (unchanged metadata writes nothing and keeps
// added_at), delisting cascades progress/bookmarks away, and the
// bookshelf order puts in-progress books on top with the rest by
// added_at, newest first.
func TestUpsertBookIdempotent(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	now := nowUTC()

	id1, outcome, err := s.UpsertBook(ctx, "/books/a.epub", "书甲", "张三, 李四", "zh", now)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if outcome != BookInserted || id1 == 0 {
		t.Fatalf("first upsert: outcome=%v id=%d", outcome, id1)
	}

	// Same metadata again: zero write, same row.
	id2, outcome, err := s.UpsertBook(ctx, "/books/a.epub", "书甲", "张三, 李四", "zh", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if outcome != BookUnchanged {
		t.Fatal("unchanged metadata must not write")
	}
	if id2 != id1 {
		t.Fatalf("re-upsert changed the row id: %d ≠ %d", id2, id1)
	}

	// Metadata change: update in place, added_at preserved.
	_, outcome, err = s.UpsertBook(ctx, "/books/a.epub", "书甲(修订)", "张三", "zh", now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if outcome != BookUpdated {
		t.Fatal("changed metadata must write")
	}
	rows, err := s.BookRows(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("BookRows = %d rows, %v", len(rows), err)
	}
	if rows[0].Title != "书甲(修订)" || rows[0].Author != "张三" {
		t.Fatalf("metadata not updated: %+v", rows[0])
	}
	if !rows[0].AddedAt.Equal(now) {
		t.Fatalf("added_at must survive an update: %v", rows[0].AddedAt)
	}
}

func TestDelistMissingCascades(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	now := nowUTC()

	idA, _, err := s.UpsertBook(ctx, "/books/a.epub", "A", "", "", now)
	if err != nil {
		t.Fatalf("upsert A: %v", err)
	}
	idB, _, err := s.UpsertBook(ctx, "/books/b.epub", "B", "", "", now)
	if err != nil {
		t.Fatalf("upsert B: %v", err)
	}
	// A is mid-read: progress and a bookmark hang off its row.
	if _, err := s.Exec(ctx,
		`INSERT INTO progress (book_id, spine_index, block_offset, pct, updated_at) VALUES (?, 2, 10, 0.25, ?)`,
		idA, now); err != nil {
		t.Fatalf("seed progress: %v", err)
	}
	if _, err := s.Exec(ctx,
		`INSERT INTO bookmarks (book_id, spine_index, block_offset, note, created_at) VALUES (?, 1, 0, 'note', ?)`,
		idA, now); err != nil {
		t.Fatalf("seed bookmark: %v", err)
	}

	removed, err := s.DelistMissing(ctx, []string{"/books/b.epub"})
	if err != nil {
		t.Fatalf("delist: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	for _, q := range []string{
		`SELECT COUNT(*) FROM books WHERE id = ?`,
		`SELECT COUNT(*) FROM progress WHERE book_id = ?`,
		`SELECT COUNT(*) FROM bookmarks WHERE book_id = ?`,
	} {
		var n int
		if err := s.db.QueryRowContext(ctx, q, idA).Scan(&n); err != nil || n != 0 {
			t.Fatalf("cascade left rows behind (%s): n=%d err=%v", q, n, err)
		}
	}
	var nB int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM books WHERE id = ?`, idB).Scan(&nB); err != nil || nB != 1 {
		t.Fatalf("kept book vanished: n=%d err=%v", nB, err)
	}
}

func TestBookRowsOrder(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	base := nowUTC()

	// a: shelved long ago, never opened. b: opened just now. c:
	// shelved after a, never opened. Shelf order: b (in progress,
	// most recent read) → c → a (unread by added_at desc).
	idA, _, _ := s.UpsertBook(ctx, "/books/a.epub", "a", "", "", base)
	if _, _, err := s.UpsertBook(ctx, "/books/b.epub", "b", "", "", base.Add(time.Minute)); err != nil {
		t.Fatalf("upsert b: %v", err)
	}
	if _, _, err := s.UpsertBook(ctx, "/books/c.epub", "c", "", "", base.Add(2*time.Minute)); err != nil {
		t.Fatalf("upsert c: %v", err)
	}
	if _, err := s.Exec(ctx,
		`INSERT INTO progress (book_id, spine_index, block_offset, pct, updated_at) VALUES (?, 0, 5, 0.42, ?)`,
		idA, base.Add(3*time.Minute)); err != nil {
		t.Fatalf("seed progress: %v", err)
	}
	// Re-shelve b later still: a metadata update keeps added_at, so a
	// later re-scan cannot outrank c's shelling order.
	if _, _, err := s.UpsertBook(ctx, "/books/b.epub", "b2", "", "", base.Add(4*time.Minute)); err != nil {
		t.Fatalf("re-upsert b: %v", err)
	}

	rows, err := s.BookRows(ctx)
	if err != nil {
		t.Fatalf("BookRows: %v", err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.Title)
	}
	want := []string{"a", "c", "b2"} // a read most recently, then unread by added_at desc: c was shelved after b
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	for _, r := range rows {
		if r.Title == "a" {
			if !r.HasProgress || r.Pct != 0.42 || !r.ReadAt.Equal(base.Add(3*time.Minute)) {
				t.Fatalf("progress not surfaced on the row: %+v", r)
			}
		} else if r.HasProgress {
			t.Fatalf("unexpected progress on %q: %+v", r.Title, r)
		}
	}
}
