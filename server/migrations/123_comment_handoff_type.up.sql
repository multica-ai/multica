-- Allow display-only handoff records on the issue timeline (MUL-3375 §6.2).
-- A handoff record is written via the service direct-write path (never
-- Handler.CreateComment), so it does NOT trigger an agent run; type='handoff'
-- keeps it out of conversation/comment-trigger counting and lets the UI render
-- a handoff card instead of a normal comment.
ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_type_check;
ALTER TABLE comment ADD CONSTRAINT comment_type_check
    CHECK (type IN ('comment', 'status_change', 'progress_update', 'system', 'handoff'));
