-- 010_question_detail.sql: the zhihu question's detail layers (M4
-- ticket 06). Two columns, two caches:
--
-- comments.num_children — the server-reported child-comment count
-- (a zhihu answer floor's comment_count, the floor-head triple's
-- third segment). NULL = the source reports none (hn: the reply
-- count is the tree itself, len(replies)). It is snapshot data, not
-- derived: the comment layer may never have been opened, and the
-- count must survive offline.
--
-- detail_fetch.question — the question layer's meta triple
-- (asker · answer_count · visit_count) as one JSON core.QuestionInfo,
-- written when the detail fetch lands and read on every re-open
-- (cache-first: the meta line renders offline). It rides the opened
-- marker's row because it shares its lifecycle: both arrive with the
-- first Detail fetch and both outlive every later whole-tree
-- replace — SaveComments' upsert deliberately does not touch it.

ALTER TABLE comments ADD COLUMN num_children INTEGER;

ALTER TABLE detail_fetch ADD COLUMN question TEXT NOT NULL DEFAULT '';
