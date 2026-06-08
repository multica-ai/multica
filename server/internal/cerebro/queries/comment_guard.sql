-- TECH-3099: session-level guard queries for the sub-issue comment guard.

-- name: HasTaskPostedOnIssue :one
-- Returns true when the given task has already stored at least one
-- cerebro_comment_task row for the given issue — used by the sub-issue
-- no-split-session check (cerebro_sub_issue_no_split_session flag).
SELECT EXISTS(
    SELECT 1 FROM cerebro_comment_task
    WHERE task_id = $1 AND issue_id = $2
) AS posted;
