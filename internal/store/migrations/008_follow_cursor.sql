-- 008_follow_cursor.sql: per-feed cursor-paging position (M4 ticket 05).
-- The follow feed's pages are addressed by zhihu's opaque paging.next
-- cursor, not a page number — pages_fetched cannot address them. The
-- column holds the cursor of the NEXT page to fetch: '' = never paged
-- (Ctrl+N bootstraps a cold feed by fetching the first page), a value
-- = the exact resume position (a restart continues the walk, never
-- re-fetching a known page). Lives beside the validators and the
-- paging depth in feed_fetch_state: three independent residents of
-- one row, no write touches the others' columns.

ALTER TABLE feed_fetch_state ADD COLUMN cursor TEXT NOT NULL DEFAULT '';
