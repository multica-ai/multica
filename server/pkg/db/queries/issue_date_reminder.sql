-- name: ClaimArrivingDateReminders :many
-- CEREBRO-PATCH(issue-date-reminders): atomically claim issues whose start_date
-- or due_date falls on "today" in the assignee's timezone and that have not yet
-- fired a reminder for that date. The claim row is inserted in the same
-- statement (ON CONFLICT DO NOTHING) so concurrent sweepers and repeated ticks
-- ring each reminder exactly once. Only member-assigned, open issues qualify.
WITH eligible AS (
    SELECT i.id          AS issue_id,
           i.workspace_id,
           i.title,
           i.status,
           i.assignee_id,
           'due'::text    AS kind,
           date(i.due_date AT TIME ZONE cerebro_safe_timezone(u.timezone)) AS reminder_date
    FROM issue i
    JOIN "user" u ON u.id = i.assignee_id
    WHERE i.kind = 'issue'
      AND i.assignee_type = 'member'
      AND i.due_date IS NOT NULL
      AND i.status NOT IN ('done', 'cancelled')
      AND date(i.due_date AT TIME ZONE cerebro_safe_timezone(u.timezone))
          = date(now() AT TIME ZONE cerebro_safe_timezone(u.timezone))
    UNION ALL
    SELECT i.id          AS issue_id,
           i.workspace_id,
           i.title,
           i.status,
           i.assignee_id,
           'start'::text  AS kind,
           date(i.start_date AT TIME ZONE cerebro_safe_timezone(u.timezone)) AS reminder_date
    FROM issue i
    JOIN "user" u ON u.id = i.assignee_id
    WHERE i.kind = 'issue'
      AND i.assignee_type = 'member'
      AND i.start_date IS NOT NULL
      AND i.status NOT IN ('done', 'cancelled')
      AND date(i.start_date AT TIME ZONE cerebro_safe_timezone(u.timezone))
          = date(now() AT TIME ZONE cerebro_safe_timezone(u.timezone))
    LIMIT $1
),
claimed AS (
    INSERT INTO cerebro_issue_date_reminder (issue_id, kind, reminder_date)
    SELECT e.issue_id, e.kind, e.reminder_date FROM eligible e
    ON CONFLICT (issue_id, kind, reminder_date) DO NOTHING
    RETURNING issue_id, kind, reminder_date
)
SELECT e.issue_id,
       e.workspace_id,
       e.title,
       e.status,
       e.assignee_id,
       e.kind,
       e.reminder_date
FROM eligible e
JOIN claimed c
  ON c.issue_id = e.issue_id
 AND c.kind = e.kind
 AND c.reminder_date = e.reminder_date;
