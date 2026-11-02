-- Reuse the concurrently built index. Dropping the old constraint also drops
-- its single-column index; lookups by comment_id use the new key's prefix.
-- Daemons claim receipts continuously, so fail fast and retry on the next run
-- rather than queueing their claims behind a pending ACCESS EXCLUSIVE lock.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

ALTER TABLE task_supplement
    DROP CONSTRAINT IF EXISTS task_supplement_pkey,
    ADD CONSTRAINT task_supplement_pkey PRIMARY KEY USING INDEX task_supplement_comment_task_uidx;
-- Left behind by a previous rollback (548 down); it would keep one run per comment.
DROP INDEX IF EXISTS task_supplement_comment_uidx;
