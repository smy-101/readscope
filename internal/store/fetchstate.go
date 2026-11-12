// fetchstate.go: persistence for a feed's fetch-side memory. One row
// per (source, feed) in feed_fetch_state carries two independent
// kinds of state: the conditional-request validators of M2's ticket
// 05 (ETag / Last-Modified — a 304 refresh writes exactly one small
// row and never touches items or FTS), and M3 ticket 05's list-paging
// depth (how many pages Ctrl+N has appended, so a restart resumes
// where the reader left off instead of re-fetching known pages).
// Neither side's writes touch the other's columns.
package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/smy-101/readscope/internal/core"
)

// LoadFetchState returns the stored validators for one feed. ok=false
// means the feed was never fetched (or the server never sent
// validators): the next request must go out unconditional.
func (s *Store) LoadFetchState(ctx context.Context, sourceID, feedKey string) (st core.FetchState, ok bool, err error) {
	var lastFetched sql.NullTime
	err = s.db.QueryRowContext(ctx,
		`SELECT etag, last_modified, last_fetched FROM feed_fetch_state
		WHERE source = ? AND feed = ?`, sourceID, feedKey).
		Scan(&st.ETag, &st.LastModified, &lastFetched)
	if err == sql.ErrNoRows {
		return core.FetchState{}, false, nil
	}
	if err != nil {
		return core.FetchState{}, false, err
	}
	if lastFetched.Valid {
		st.LastFetched = lastFetched.Time
	}
	return st, true, nil
}

// SaveFetchState upserts one feed's validators after a fetch: a 200
// stores the fresh values (empty when the server sent none), a 304
// re-stores the kept ones with a bumped last_fetched.
func (s *Store) SaveFetchState(ctx context.Context, sourceID, feedKey string, st core.FetchState) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO feed_fetch_state
		(source, feed, etag, last_modified, last_fetched)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(source, feed) DO UPDATE SET
			etag = excluded.etag,
			last_modified = excluded.last_modified,
			last_fetched = excluded.last_fetched`,
		sourceID, feedKey, st.ETag, st.LastModified, st.LastFetched)
	return err
}

// LoadPagesFetched reports how many pages of one feed have been
// fetched contiguously from page 0 — the depth Ctrl+N appends at. A
// missing row is 0: never paged, so the next append fetches page 0
// itself (a feed cached by F5 carries depth ≥ 1 and appends from
// page 1).
func (s *Store) LoadPagesFetched(ctx context.Context, sourceID, feedKey string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT pages_fetched FROM feed_fetch_state
		WHERE source = ? AND feed = ?`, sourceID, feedKey).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}

// SavePagesFetched upserts one feed's paging depth. A fresh row gets
// empty validators (paged sources are unconditional by design); an
// existing row keeps its validators — the depth write only touches
// pages_fetched.
func (s *Store) SavePagesFetched(ctx context.Context, sourceID, feedKey string, pages int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO feed_fetch_state
		(source, feed, etag, last_modified, last_fetched, pages_fetched)
		VALUES (?, ?, '', '', ?, ?)
		ON CONFLICT(source, feed) DO UPDATE SET
			pages_fetched = excluded.pages_fetched`,
		sourceID, feedKey, time.Now().UTC(), pages)
	return err
}

// LoadCursor returns one feed's cursor-paging position — the opaque
// server cursor of the next page to fetch (zhihu's follow feed; the
// cursor variant of LoadPagesFetched). A missing row is "": never
// paged, so the next Ctrl+N bootstraps by fetching the first page.
func (s *Store) LoadCursor(ctx context.Context, sourceID, feedKey string) (string, error) {
	var cursor string
	err := s.db.QueryRowContext(ctx,
		`SELECT cursor FROM feed_fetch_state
		WHERE source = ? AND feed = ?`, sourceID, feedKey).Scan(&cursor)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return cursor, err
}

// SaveCursor upserts one feed's cursor-paging position. Like the
// depth write it only ever touches its own column: validators and
// depth on the same row survive untouched.
func (s *Store) SaveCursor(ctx context.Context, sourceID, feedKey, cursor string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO feed_fetch_state
		(source, feed, etag, last_modified, last_fetched, cursor)
		VALUES (?, ?, '', '', ?, ?)
		ON CONFLICT(source, feed) DO UPDATE SET
			cursor = excluded.cursor`,
		sourceID, feedKey, time.Now().UTC(), cursor)
	return err
}
