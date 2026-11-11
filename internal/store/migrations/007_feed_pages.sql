-- 007_feed_pages.sql: per-feed list-paging depth (M3 ticket 05).
-- How many pages of one feed have been fetched contiguously from page
-- 0 (Ctrl+N appends, F5 never lowers it). Lives in feed_fetch_state —
-- already one row per (source, feed) — so validators and depth share
-- a row without clobbering each other: 0 means "never paged", which
-- stays distinct from "fetched the first page" (depth 1).

ALTER TABLE feed_fetch_state ADD COLUMN pages_fetched INTEGER NOT NULL DEFAULT 0;
