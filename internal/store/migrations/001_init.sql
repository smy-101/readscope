-- 001_init.sql: initial schema (items / read state / comments / books / progress / bookmarks / FTS)

CREATE TABLE items (
    id           TEXT PRIMARY KEY,   -- deterministic per-source id, e.g. "hn:algolia:<id>", "rss:<feedkey>:<guid>"
    source       TEXT NOT NULL,
    feed         TEXT NOT NULL,
    title        TEXT NOT NULL,
    url          TEXT NOT NULL DEFAULT '',
    author       TEXT NOT NULL DEFAULT '',
    published_at TIMESTAMP,          -- NULL allowed: feeds without dates
    fetched_at   TIMESTAMP NOT NULL,
    body         TEXT NOT NULL DEFAULT ''  -- cached snapshot, offline-readable
);
CREATE INDEX idx_items_feed ON items(feed, published_at DESC);

CREATE TABLE read_state (
    item_id        TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    read_at        TIMESTAMP NOT NULL,
    starred_at     TIMESTAMP,
    star_snapshot  TEXT             -- full snapshot at star time (JSON), readable after the original is gone
);
CREATE INDEX idx_read_state_starred ON read_state(starred_at) WHERE starred_at IS NOT NULL;

CREATE TABLE comments (
    id          TEXT NOT NULL,       -- source-scoped comment id
    item_id     TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    parent_id   TEXT,                -- NULL = top-level floor
    author      TEXT NOT NULL DEFAULT '',
    text        TEXT NOT NULL DEFAULT '',
    score       INTEGER,
    created_at  TIMESTAMP,
    PRIMARY KEY (item_id, id)
);

CREATE TABLE books (
    id        INTEGER PRIMARY KEY,
    path      TEXT NOT NULL UNIQUE,  -- absolute path within the library directory
    title     TEXT NOT NULL,
    author    TEXT NOT NULL DEFAULT '',
    language  TEXT NOT NULL DEFAULT '',
    added_at  TIMESTAMP NOT NULL
);

CREATE TABLE progress (
    book_id       INTEGER PRIMARY KEY REFERENCES books(id) ON DELETE CASCADE,
    spine_index   INTEGER NOT NULL,
    block_offset  INTEGER NOT NULL,  -- (spine_index, block_offset) survives resize/re-wrap
    pct           REAL NOT NULL,
    updated_at    TIMESTAMP NOT NULL
);

CREATE TABLE bookmarks (
    book_id       INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    spine_index   INTEGER NOT NULL,
    block_offset  INTEGER NOT NULL,
    note          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMP NOT NULL,
    PRIMARY KEY (book_id, spine_index, block_offset)
);

-- Unified full-text index: feed items carry item_id, book chapters carry book_id.
CREATE VIRTUAL TABLE fts USING fts5(title, body, item_id UNINDEXED, book_id UNINDEXED);
