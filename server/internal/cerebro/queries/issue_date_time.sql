-- Optional time-of-day for an issue's start_date / due_date, plus the per-issue
-- agent auto-start choice. See migrations 9087_cerebro_issue_date_time and
-- 9095_cerebro_agent_start_kind. Read/written through the cerebro issuedatetime
-- handler; the date-reminder sweeper joins the table directly in its own claim
-- query (pkg/db/queries/issue_date_reminder.sql).

-- name: GetCerebroIssueDateTime :one
SELECT issue_id, start_time, due_time, agent_start_kind, updated_at
FROM cerebro_issue_date_time
WHERE issue_id = $1;

-- name: UpsertCerebroIssueDateTime :one
INSERT INTO cerebro_issue_date_time (issue_id, start_time, due_time, agent_start_kind, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (issue_id) DO UPDATE
SET start_time       = EXCLUDED.start_time,
    due_time         = EXCLUDED.due_time,
    agent_start_kind = EXCLUDED.agent_start_kind,
    updated_at       = now()
RETURNING issue_id, start_time, due_time, agent_start_kind, updated_at;

-- name: DeleteCerebroIssueDateTime :exec
DELETE FROM cerebro_issue_date_time
WHERE issue_id = $1;
