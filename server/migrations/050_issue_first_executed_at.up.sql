-- first_executed_at is stamped atomically the first time an issue's task
-- reaches a terminal `done` state. Analytics reads this as the single
-- source of truth for the issue_executed funnel event — atomic UPDATE …
-- WHERE first_executed_at IS NULL guarantees at-most-one emission per
-- issue regardless of retries, re-assignments, or comment-triggered
-- follow-up tasks.
ALTER TABLE issue
    ADD COLUMN first_executed_at TIMESTAMPTZ NULL;

-- A partial index on the NULL-until-set column lets the workspace-scoped
-- "how many issues executed so far?" count (nth_issue_for_workspace)
-- skip the large tail of never-executed issues.
CREATE INDEX IF NOT EXISTS idx_issue_first_executed_at
    ON issue (workspace_id, first_executed_at)
    WHERE first_executed_at IS NOT NULL;
