-- Record the comment ids that were actually embedded in a daemon claim.
--
-- coalesced_comment_ids is the enqueue plan, not proof of delivery: daemons
-- predating coalesced-comments-v1 ignore the structured fields. Keeping a
-- separate delivered set lets completion reconciliation replay every comment
-- that the claiming daemon did not receive.
ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS delivered_comment_ids UUID[] NOT NULL DEFAULT '{}';

-- Active tasks created before this migration always received their primary
-- trigger through trigger_comment_content. We deliberately do not backfill
-- coalesced ids: an older daemon may have ignored them, and a duplicate replay
-- is safer than silently losing a deliberate instruction.
UPDATE agent_task_queue
SET delivered_comment_ids = ARRAY[trigger_comment_id]
WHERE trigger_comment_id IS NOT NULL
  AND status IN ('dispatched', 'running', 'waiting_local_directory');
