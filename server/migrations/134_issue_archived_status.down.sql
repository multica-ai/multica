-- Revert the 'archived' status. Any issues currently in 'archived' must be moved
-- to a status that survives the tightened CHECK; 'cancelled' is the closest
-- terminal, board-hidden state.
UPDATE issue SET status = 'cancelled' WHERE status = 'archived';
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;
ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled'));
