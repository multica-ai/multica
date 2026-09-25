-- Dropping the column is the whole rollback: no CHECK constraint was touched
-- on the way up, so there is nothing to narrow back and no rows to rewrite.
ALTER TABLE comment DROP COLUMN IF EXISTS quick_action_id;
