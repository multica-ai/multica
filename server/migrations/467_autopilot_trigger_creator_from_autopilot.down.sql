-- The backfill is irreversible: a filled row is indistinguishable from a trigger
-- its creator made, and clearing it would stop that trigger again. Restore only
-- migration 449's column comment.
COMMENT ON COLUMN autopilot_trigger.created_by_type IS
    'Actor type of the trigger''s immutable creator: member | agent. Only ''member'' yields a run principal. NULL for triggers created before MUL-6951 that had no published_by to backfill from.';
