-- 005_comments_seq.sql: explicit tree order for cached comments.
-- The comments DAO stores each fetched tree whole and reads it back in
-- the fetch's tree order (floors in payload order, each followed by
-- its subtree, parents strictly before children) — the order the M3
-- detail layers flatten from. rowid order (the pre-005 mechanism) is
-- environmental: VACUUM may renumber it for tables without an
-- INTEGER PRIMARY KEY. seq makes the invariant structural. Legacy
-- rows all carry 0 and keep rowid order through the tiebreak.

ALTER TABLE comments ADD COLUMN seq INTEGER NOT NULL DEFAULT 0;
