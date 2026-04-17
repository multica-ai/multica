-- Reverse Phase 2a's author/actor relaxations.
-- NOTE: re-tightening NOT NULL fails if any cached rows have NULL author_id /
-- actor_id (which is the entire purpose of this migration). The down path
-- truncates the cache rows that violate the constraint before re-applying.

ALTER TABLE issue_reaction DROP COLUMN IF EXISTS gitlab_actor_user_id;
DELETE FROM issue_reaction WHERE actor_id IS NULL OR actor_type IS NULL;
ALTER TABLE issue_reaction ALTER COLUMN actor_type SET NOT NULL;
ALTER TABLE issue_reaction ALTER COLUMN actor_id   SET NOT NULL;

ALTER TABLE comment DROP COLUMN IF EXISTS gitlab_author_user_id;
DELETE FROM comment WHERE author_id IS NULL OR author_type IS NULL;
ALTER TABLE comment ALTER COLUMN author_type SET NOT NULL;
ALTER TABLE comment ALTER COLUMN author_id   SET NOT NULL;
