// books.go: the EPUB shelf's persistence (M6 ticket 02). books rows
// are the library scan's upsert target — path is the identity, the
// file's metadata fills the row, and a row whose file vanished from
// disk is deleted outright (progress/bookmarks ride the schema's ON
// DELETE CASCADE). BookRows is the bookshelf screen's read: the shelf
// order (in-progress on top) lives here, one query, so the screen and
// any future consumer see the same order.
package store

import (
	"context"
	"database/sql"
	"time"
)

// UpsertBook inserts a new book row keyed by absolute path, or
// refreshes an existing one's metadata. changed=false means the row
// already held exactly this metadata and nothing was written — the
// scan's idempotency contract (a re-scan of an unchanged library is
// zero writes, and added_at never moves on an update).
func (s *Store) UpsertBook(ctx context.Context, path, title, author, language string, now time.Time) (id int64, changed bool, err error) {
	var (
		existingID                int64
		exTitle, exAuthor, exLang string
	)
	err = s.db.QueryRowContext(ctx,
		`SELECT id, title, author, language FROM books WHERE path = ?`, path).
		Scan(&existingID, &exTitle, &exAuthor, &exLang)
	switch {
	case err == sql.ErrNoRows:
		res, err := s.db.ExecContext(ctx,
			`INSERT INTO books (path, title, author, language, added_at) VALUES (?, ?, ?, ?, ?)`,
			path, title, author, language, now)
		if err != nil {
			return 0, false, err
		}
		id, err := res.LastInsertId()
		return id, true, err
	case err != nil:
		return 0, false, err
	}
	if exTitle == title && exAuthor == author && exLang == language {
		return existingID, false, nil
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE books SET title = ?, author = ?, language = ? WHERE id = ?`,
		title, author, language, existingID); err != nil {
		return 0, false, err
	}
	return existingID, true, nil
}

// DelistMissing deletes every book row whose path is not in keep —
// the scan's 下架 half. progress and bookmarks go with the row (ON
// DELETE CASCADE): a file that left the disk takes its reading state
// along, by the 删文件即下架 contract. A book whose file is merely
// broken stays shelved; only disappearance delists. Paths are matched
// one by one in a transaction (SQLite's bound-parameter ceiling is
// not a thousand-book library's friend).
func (s *Store) DelistMissing(ctx context.Context, keep []string) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM books`)
	if err != nil {
		return 0, err
	}
	var stored []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return 0, err
		}
		stored = append(stored, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	kept := make(map[string]bool, len(keep))
	for _, p := range keep {
		kept[p] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() // no-op after Commit
	removed := 0
	for _, p := range stored {
		if kept[p] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM books WHERE path = ?`, p); err != nil {
			return 0, err
		}
		removed++
	}
	return removed, tx.Commit()
}

// BookRow is one bookshelf row: the books row joined with its reading
// progress. HasProgress is false for never-opened books (Pct/ReadAt
// are the zero values then).
type BookRow struct {
	ID          int64
	Path        string
	Title       string
	Author      string
	Language    string
	AddedAt     time.Time
	HasProgress bool
	Pct         float64
	ReadAt      time.Time // progress.updated_at
}

// BookRows lists the shelf: books being read first, most recently
// read on top (progress.updated_at desc); never-opened books behind
// them by added_at desc. The bookshelf screen renders straight from
// this order.
func (s *Store) BookRows(ctx context.Context) ([]BookRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id, b.path, b.title, b.author, b.language, b.added_at,
		       p.pct, p.updated_at
		FROM books b LEFT JOIN progress p ON p.book_id = b.id
		ORDER BY (p.updated_at IS NULL), p.updated_at DESC, b.added_at DESC, b.path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BookRow
	for rows.Next() {
		var r BookRow
		var pct sql.NullFloat64
		var readAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Path, &r.Title, &r.Author, &r.Language,
			&r.AddedAt, &pct, &readAt); err != nil {
			return nil, err
		}
		if r.HasProgress = readAt.Valid; r.HasProgress {
			r.Pct, r.ReadAt = pct.Float64, readAt.Time
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
