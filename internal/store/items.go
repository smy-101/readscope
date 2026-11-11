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
// every refresh by construction. Ids are unique per source, so a
// known-item check scans the whole source, and an id arriving under a
// different feed re-homes its single row to that feed (HN story ids
// roam between Front and New; rss ids embed the feed key, making
// this exactly equivalent for them). It returns how many items were
// new and the feed's total item count after the save. A feed serving
// the same id twice in one response keeps the first occurrence.
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
	rows, err := tx.QueryContext(ctx, `SELECT id FROM items WHERE source = ?`, sourceID)
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
				SET feed = ?, title = ?, url = ?, author = ?, published_at = ?, fetched_at = ?, body = ?
				WHERE id = ?`,
				feedKey, it.Title, it.URL, it.Author, it.PublishedAt, it.FetchedAt, it.Body, it.ID)
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

// ItemRow pairs a cached item with its read/star state for list
// rendering: the unread dot, star marker and dim styling key off these.
type ItemRow struct {
	Item    core.Item
	Read    bool
	Starred bool
}

// FeedItems returns one feed's cached items newest first — items without
// a publish date slot by fetch time — each carrying its read/star state,
// optionally only the unread ones. The TUI list renders straight from
// this: cache first, no network anywhere.
func (s *Store) FeedItems(ctx context.Context, sourceID, feedKey string, unreadOnly bool) ([]ItemRow, error) {
	q := `SELECT i.id, i.source, i.feed, i.title, i.url, i.author,
		i.published_at, i.fetched_at, i.body, r.read_at IS NOT NULL, r.starred_at IS NOT NULL
		FROM items i LEFT JOIN read_state r ON r.item_id = i.id
		WHERE i.source = ? AND i.feed = ?`
	if unreadOnly {
		q += ` AND r.read_at IS NULL`
	}
	q += ` ORDER BY COALESCE(i.published_at, i.fetched_at) DESC, i.id`
	rows, err := s.db.QueryContext(ctx, q, sourceID, feedKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemRow
	for rows.Next() {
		var r ItemRow
		var published sql.NullTime
		if err := rows.Scan(&r.Item.ID, &r.Item.Source, &r.Item.Feed, &r.Item.Title,
			&r.Item.URL, &r.Item.Author, &published, &r.Item.FetchedAt, &r.Item.Body, &r.Read, &r.Starred); err != nil {
			return nil, err
		}
		if published.Valid {
			r.Item.PublishedAt = &published.Time
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FeedCount is one feed's list-layer stats: how many cached items it
// holds and how many are still unread.
type FeedCount struct {
	Total  int
	Unread int
}

// FeedCounts reports Total/Unread per feed key for one source, in a
// single grouped query — the sidebar's unread badges and the status
// bar's "N 条 · M 新" both read from here. Feeds with no cached items
// are absent; the sidebar counts them as zero.
func (s *Store) FeedCounts(ctx context.Context, sourceID string) (map[string]FeedCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.feed, COUNT(*), SUM(r.read_at IS NULL)
		FROM items i LEFT JOIN read_state r ON r.item_id = i.id
		WHERE i.source = ?
		GROUP BY i.feed`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]FeedCount{}
	for rows.Next() {
		var feed string
		var c FeedCount
		var unread sql.NullInt64
		if err := rows.Scan(&feed, &c.Total, &unread); err != nil {
			return nil, err
		}
		c.Unread = int(unread.Int64)
		out[feed] = c
	}
	return out, rows.Err()
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
