-- 009_items_meta.sql: the meta column (ticket 03).
-- A source's own preformatted meta line for the list's 元信息列, served
-- as one atomic string the way the source spells it (zhihu daily's
-- hint "作者 · 2 分钟阅读" mixes author and reading time inseparably).
-- Sources that compose their meta from semantic fields (hn's heat
-- radar from points/num_comments/author, the hot board's heat from
-- points, the follow feed's author) leave it empty forever — the UI
-- routes per feed, so the column only ever holds what the source
-- itself could not decompose.

ALTER TABLE items ADD COLUMN meta TEXT NOT NULL DEFAULT '';
