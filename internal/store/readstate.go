// readstate.go: the read/star state machine's persistence. Read state
// and favorites share one row per item (read_state); both columns are
// touched only here, so refreshes rebuilding items can never disturb
// them. Starring snapshots the full item as JSON — the favorite stays
// readable even after the items row is gone.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/smy-101/readscope/internal/core"
)

// SetRead marks one item read (read_at = now) or unread (read_at
// cleared; the row disappears entirely unless it is also starred).
func (s *Store) SetRead(ctx context.Context, itemID string, read bool) error {
	if read {
		_, err := s.db.ExecContext(ctx, `INSERT INTO read_state (item_id, read_at)
			VALUES (?, ?) ON CONFLICT(item_id) DO UPDATE SET read_at = excluded.read_at`,
			itemID, time.Now().UTC())
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE read_state SET read_at = NULL WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM read_state
		WHERE item_id = ? AND read_at IS NULL AND starred_at IS NULL`, itemID)
	return err
}

// markStarred updates one item's star columns, leaving read state
// untouched (star and read are orthogonal).
func (s *Store) markStarred(ctx context.Context, itemID string, star bool, snapshot string) error {
	if star {
		_, err := s.db.ExecContext(ctx, `INSERT INTO read_state (item_id, read_at, starred_at, star_snapshot)
			VALUES (?, NULL, ?, ?)
			ON CONFLICT(item_id) DO UPDATE SET starred_at = excluded.starred_at, star_snapshot = excluded.star_snapshot`,
			itemID, time.Now().UTC(), snapshot)
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE read_state SET starred_at = NULL, star_snapshot = NULL
		WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM read_state
		WHERE item_id = ? AND read_at IS NULL AND starred_at IS NULL`, itemID)
	return err
}

// SetStarred stars an item, snapshotting its full content at star time,
// or unstars it (snapshot dropped with the star).
func (s *Store) SetStarred(ctx context.Context, it core.Item, star bool) error {
	var snapshot string
	if star {
		raw, err := json.Marshal(it)
		if err != nil {
			return fmt.Errorf("snapshot %s: %w", it.ID, err)
		}
		snapshot = string(raw)
	}
	return s.markStarred(ctx, it.ID, star, snapshot)
}

// MarkFeedRead marks every cached item of one feed read in a single
// statement; existing star columns survive the upsert.
func (s *Store) MarkFeedRead(ctx context.Context, sourceID, feedKey string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO read_state (item_id, read_at)
		SELECT id, ? FROM items WHERE source = ? AND feed = ?
		ON CONFLICT(item_id) DO UPDATE SET read_at = excluded.read_at`,
		time.Now().UTC(), sourceID, feedKey)
	return err
}

// StarredItems lists favorites across all feeds/sources, newest star
// first. The items row is preferred (it carries the freshest snapshot);
// when it is gone the item is rebuilt from the star-time JSON snapshot,
// which is the whole point of keeping one.
func (s *Store) StarredItems(ctx context.Context) ([]ItemRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.id, i.source, i.feed, i.title, i.url, i.author,
		i.published_at, i.fetched_at, i.body, r.read_at IS NOT NULL, r.star_snapshot
		FROM read_state r LEFT JOIN items i ON i.id = r.item_id
		WHERE r.starred_at IS NOT NULL
		ORDER BY r.starred_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemRow
	for rows.Next() {
		// Every items column is nullable: a LEFT JOIN miss (source item
		// gone) leaves them NULL until the snapshot fills the item in.
		var id, source, feed, title, url, author, body, snapshot sql.NullString
		var published, fetched sql.NullTime
		var r ItemRow
		if err := rows.Scan(&id, &source, &feed, &title, &url, &author,
			&published, &fetched, &body, &r.Read, &snapshot); err != nil {
			return nil, err
		}
		if id.Valid && id.String != "" {
			r.Item = core.Item{
				ID: id.String, Source: source.String, Feed: feed.String,
				Title: title.String, URL: url.String, Author: author.String,
				Body: body.String,
			}
			if published.Valid {
				r.Item.PublishedAt = &published.Time
			}
			if fetched.Valid {
				r.Item.FetchedAt = fetched.Time
			}
		} else {
			// Source item gone: fall back to the star-time snapshot.
			if err := json.Unmarshal([]byte(snapshot.String), &r.Item); err != nil {
				return nil, fmt.Errorf("star snapshot for %s: %w", r.Item.ID, err)
			}
		}
		r.Starred = true
		out = append(out, r)
	}
	return out, rows.Err()
}
