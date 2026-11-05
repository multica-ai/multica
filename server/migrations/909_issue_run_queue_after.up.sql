-- Run queue ordering by dependency. Replaces the per-issue number (queue_order),
-- which is dropped together with its index, so every issue leaves the queue
-- and the numbers are not carried over.
-- run_queued: the issue is in the automatic run queue (a backlog issue moves to
-- todo once every issue in run_after_issue_ids has finished).
-- run_after_issue_ids: issues it waits on. No foreign key on purpose: an id whose
-- issue was deleted no longer names an unfinished issue and stops blocking.
ALTER TABLE issue DROP COLUMN IF EXISTS queue_order;
ALTER TABLE issue ADD COLUMN run_queued BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE issue ADD COLUMN run_after_issue_ids UUID[] NOT NULL DEFAULT '{}';
