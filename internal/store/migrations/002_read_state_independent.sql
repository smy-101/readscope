-- 002_read_state_independent.sql: read_state becomes self-owning.
-- read_at is now nullable (a row exists iff the item is read or starred,
-- so "unread" is representable), and the FK to items is dropped: a
-- favorite's star_snapshot must survive the items row disappearing
-- (cache eviction, feed pruning) — that is the snapshot's whole job.

CREATE TABLE read_state_new (
    item_id        TEXT PRIMARY KEY,
    read_at        TIMESTAMP,
    starred_at     TIMESTAMP,
    star_snapshot  TEXT             -- full item snapshot at star time (JSON)
);
INSERT INTO read_state_new (item_id, read_at, starred_at, star_snapshot)
    SELECT item_id, read_at, starred_at, star_snapshot FROM read_state;
DROP TABLE read_state;
ALTER TABLE read_state_new RENAME TO read_state;
CREATE INDEX idx_read_state_starred ON read_state(starred_at) WHERE starred_at IS NOT NULL;
