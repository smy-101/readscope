-- 006_detail_fetch.sql: the opened marker (ticket 04).
-- One row per item whose detail layer was ever fetched: it separates
-- "0 comments cached" (opened, discussion genuinely empty — render the
-- cache, refresh in background) from "never opened" (show the loading
-- state, fetch now). Written only inside SaveComments' whole-tree
-- transaction, so marker and tree can never disagree.

CREATE TABLE detail_fetch (
    item_id    TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    fetched_at TIMESTAMP NOT NULL
);
