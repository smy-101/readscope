-- 004_hn_heat_columns.sql: HN heat snapshots on cached items.
-- Points and comment count as of fetched_at (M3 ticket 02): the list's
-- heat radar ("N pts · N comments · author") and the ranked ordering of
-- hn's popularity feeds both read these columns, and a refresh updates
-- them in place like any other snapshot field. NULL = not applicable:
-- rows from sources without heat semantics (rss) stay NULL forever and
-- render exactly as before.

ALTER TABLE items ADD COLUMN points INTEGER;
ALTER TABLE items ADD COLUMN num_comments INTEGER;
