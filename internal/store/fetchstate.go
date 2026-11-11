// fetchstate.go: persistence for a feed's conditional-request memory
// (ticket 05). Validators live per (source, feed) row — separate from
// the items snapshot, so a 304 refresh writes exactly one small row and
// never touches items or FTS.
package store

import (
	"context"
	"database/sql"

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
