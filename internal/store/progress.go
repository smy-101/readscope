// progress.go: the reader's persistence (M6 ticket 03). Progress is
// the 续读位 anchor (spine_index, block_offset) plus a display-only
// pct — the anchor survives width changes and machines, pct feeds the
// shelf's percent column. Bookmarks hang off the same book row with
// the position as identity: re-adding at an anchor refreshes the note
// in place rather than stacking duplicates.
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Progress is one book's saved reading position.
type Progress struct {
	SpineIndex  int
	BlockOffset int
	Pct         float64 // display only; derived from the anchor at save time
	UpdatedAt   time.Time
}

// GetProgress loads the book's saved 续读位. ok=false means the book
// was never opened (the reader then starts at the book's head).
func (s *Store) GetProgress(ctx context.Context, bookID int64) (p Progress, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT spine_index, block_offset, pct, updated_at FROM progress WHERE book_id = ?`,
		bookID).Scan(&p.SpineIndex, &p.BlockOffset, &p.Pct, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Progress{}, false, nil
	}
	if err != nil {
		return Progress{}, false, err
	}
	return p, true, nil
}

// SaveProgress upserts the 续读位 (progress's PK is book_id): the
// latest write wins, the row never duplicates.
func (s *Store) SaveProgress(ctx context.Context, bookID int64, spineIndex, blockOffset int, pct float64, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO progress (book_id, spine_index, block_offset, pct, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(book_id) DO UPDATE SET
		   spine_index = excluded.spine_index,
		   block_offset = excluded.block_offset,
		   pct = excluded.pct,
		   updated_at = excluded.updated_at`,
		bookID, spineIndex, blockOffset, pct, now)
	return err
}

// Bookmark is one saved position with an optional note.
type Bookmark struct {
	SpineIndex  int
	BlockOffset int
	Note        string
	CreatedAt   time.Time
}

// Bookmarks lists the book's bookmarks, newest first.
func (s *Store) Bookmarks(ctx context.Context, bookID int64) ([]Bookmark, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT spine_index, block_offset, note, created_at
		 FROM bookmarks WHERE book_id = ?
		 ORDER BY created_at DESC, spine_index DESC, block_offset DESC`,
		bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bookmark
	for rows.Next() {
		var b Bookmark
		if err := rows.Scan(&b.SpineIndex, &b.BlockOffset, &b.Note, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AddBookmark saves a bookmark at the anchor. The PK is
// (book_id, spine_index, block_offset), so a re-add at the same
// position refreshes the note and keeps the original created_at slot.
func (s *Store) AddBookmark(ctx context.Context, bookID int64, spineIndex, blockOffset int, note string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO bookmarks (book_id, spine_index, block_offset, note, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(book_id, spine_index, block_offset) DO UPDATE SET note = excluded.note`,
		bookID, spineIndex, blockOffset, note, now)
	return err
}

// DeleteBookmark removes the bookmark at the anchor, if any.
func (s *Store) DeleteBookmark(ctx context.Context, bookID int64, spineIndex, blockOffset int) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM bookmarks WHERE book_id = ? AND spine_index = ? AND block_offset = ?`,
		bookID, spineIndex, blockOffset)
	return err
}
