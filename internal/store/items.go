package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/smy-101/readscope/internal/core"
)

// SaveFeedItems refreshes one feed's snapshot in a single transaction:
// unseen items insert, known items update their content in place
// (ids are deterministic), and the FTS index rows for those items are
// rebuilt. read_state rows are never touched here — read/star survive
// every refresh by construction. It returns how many items were new
// and the feed's total item count after the save. A feed serving the
// same id twice in one response keeps the first occurrence.
func (s *Store) SaveFeedItems(ctx context.Context, sourceID, feedKey string, items []core.Item) (newCount, total int, err error) {
	for _, it := range items {
		if it.Source != sourceID || it.Feed != feedKey {
			return 0, 0, fmt.Errorf("store: item %q does not belong to %s/%s", it.ID, sourceID, feedKey)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback() // no-op after Commit

	existing := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM items WHERE source = ? AND feed = ?`, sourceID, feedKey)
	if err != nil {
		return 0, 0, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, 0, err
		}
		existing[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	inBatch := map[string]bool{}
	for _, it := range items {
		if inBatch[it.ID] {
			continue // duplicate guid served twice: keep the first
		}
		inBatch[it.ID] = true

		if existing[it.ID] {
			_, err = tx.ExecContext(ctx, `UPDATE items
				SET title = ?, url = ?, author = ?, published_at = ?, fetched_at = ?, body = ?
				WHERE id = ?`,
				it.Title, it.URL, it.Author, it.PublishedAt, it.FetchedAt, it.Body, it.ID)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO items
				(id, source, feed, title, url, author, published_at, fetched_at, body)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				it.ID, it.Source, it.Feed, it.Title, it.URL, it.Author, it.PublishedAt, it.FetchedAt, it.Body)
			newCount++
		}
		if err != nil {
			return 0, 0, fmt.Errorf("store item %s: %w", it.ID, err)
		}
		// Rebuild the FTS row even when the items row only updated: the
		// snapshot text may have changed.
		if _, err := tx.ExecContext(ctx, `DELETE FROM fts WHERE item_id = ?`, it.ID); err != nil {
			return 0, 0, fmt.Errorf("clear fts %s: %w", it.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO fts (title, body, item_id) VALUES (?, ?, ?)`,
			ftsText(it.Title), ftsText(it.Body), it.ID); err != nil {
			return 0, 0, fmt.Errorf("index fts %s: %w", it.ID, err)
		}
	}

	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM items WHERE source = ? AND feed = ?`, sourceID, feedKey).Scan(&total); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return newCount, total, nil
}

// SearchFTS runs a full-text query over the unified index and returns
// matching item ids, newest fetch first. Used by doctor-adjacent tests
// today; the TUI search screens consume it from M2/M4 on.
func (s *Store) SearchFTS(ctx context.Context, query string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id FROM fts WHERE fts MATCH ? AND item_id != '' ORDER BY rank`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id sql.NullString
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id.Valid && id.String != "" {
			ids = append(ids, id.String)
		}
	}
	return ids, rows.Err()
}

// CountFeed reports how many items one feed holds (refresh's total when
// the fetch itself failed).
func (s *Store) CountFeed(ctx context.Context, sourceID, feedKey string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM items WHERE source = ? AND feed = ?`, sourceID, feedKey).Scan(&n)
	return n, err
}

// ftsText converts an HTML snapshot (or plain title) to indexable text:
// text nodes joined with spaces, script/style dropped, whitespace runs
// collapsed. Bodies keep their HTML in items.body; the FTS copy is
// plain so queries never match markup. A parse failure falls back to
// the raw string — indexing must never fail on dirty feeds.
func ftsText(s string) string {
	if !strings.ContainsAny(s, "<&") {
		return collapseSpace(s)
	}
	doc, err := parseHTMLText(s)
	if err != nil {
		return collapseSpace(s)
	}
	return collapseSpace(doc)
}
