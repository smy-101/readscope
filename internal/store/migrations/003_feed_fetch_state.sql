-- 003_feed_fetch_state.sql: per-feed conditional-request memory.
-- One row per (source, feed): the ETag / Last-Modified the server sent
-- last time, plus when we last asked. Refresh sends them back as
-- If-None-Match / If-Modified-Since; a 304 answer skips parsing and
-- writing entirely. Servers that send no validators store empty
-- strings, which makes the next request unconditional — a plain
-- refresh, no side effects.

CREATE TABLE feed_fetch_state (
    source        TEXT NOT NULL,
    feed          TEXT NOT NULL,
    etag          TEXT NOT NULL DEFAULT '',
    last_modified TEXT NOT NULL DEFAULT '',
    last_fetched  TIMESTAMP NOT NULL,
    PRIMARY KEY (source, feed)
);
