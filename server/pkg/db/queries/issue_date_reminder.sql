-- name: ClaimArrivingDateReminders :many
-- CEREBRO-PATCH(issue-date-reminders): atomically claim issues whose start_date
-- or due_date has arrived and that have not yet fired for that date. The claim
-- row is inserted in the same statement (ON CONFLICT DO NOTHING) so concurrent
-- sweepers and repeated ticks fire each one exactly once.
--
-- Arrival is gated on the OPTIONAL time-of-day in cerebro_issue_date_time: a row
-- becomes eligible only once now() has passed (date + time) in the resolving
-- timezone. With no time set the time defaults to 00:00, so the date keeps
-- firing at the start of the day exactly as before.
--
-- Three branches:
--   1. due,   member-assigned -> notification ("Due today"), tz from assignee.
--   2. start, member-assigned -> notification ("Starts today"), tz from assignee.
--   3. start, agent-assigned  -> auto-start the assigned agent. tz from the
--      issue's member creator (the human who scheduled it), UTC fallback.
-- The Go sweeper branches on assignee_type + kind to pick notify vs auto-start.
WITH eligible AS (
    SELECT i.id          AS issue_id,
           i.workspace_id,
           i.title,
           i.status,
           i.assignee_id,
           i.assignee_type,
           'due'::text    AS kind,
           i.due_date     AS reminder_date
    FROM issue i
    JOIN "user" u ON u.id = i.assignee_id
    LEFT JOIN cerebro_issue_date_time t ON t.issue_id = i.id
    WHERE i.kind = 'issue'
      AND i.assignee_type = 'member'
      AND i.due_date IS NOT NULL
      AND i.status NOT IN ('done', 'cancelled')
      AND i.due_date = date(now() AT TIME ZONE cerebro_safe_timezone(u.timezone))
      AND (now() AT TIME ZONE cerebro_safe_timezone(u.timezone))
          >= (i.due_date + COALESCE(t.due_time, '00:00'::time))
    UNION ALL
    SELECT i.id          AS issue_id,
           i.workspace_id,
           i.title,
           i.status,
           i.assignee_id,
           i.assignee_type,
           'start'::text  AS kind,
           i.start_date   AS reminder_date
    FROM issue i
    JOIN "user" u ON u.id = i.assignee_id
    LEFT JOIN cerebro_issue_date_time t ON t.issue_id = i.id
    WHERE i.kind = 'issue'
      AND i.assignee_type = 'member'
      AND i.start_date IS NOT NULL
      AND i.status NOT IN ('done', 'cancelled')
      AND i.start_date = date(now() AT TIME ZONE cerebro_safe_timezone(u.timezone))
      AND (now() AT TIME ZONE cerebro_safe_timezone(u.timezone))
          >= (i.start_date + COALESCE(t.start_time, '00:00'::time))
    UNION ALL
    SELECT i.id          AS issue_id,
           i.workspace_id,
           i.title,
           i.status,
           i.assignee_id,
           i.assignee_type,
           'start'::text  AS kind,
           i.start_date   AS reminder_date
    FROM issue i
    LEFT JOIN "user" cu ON cu.id = i.creator_id AND i.creator_type = 'member'
    LEFT JOIN cerebro_issue_date_time t ON t.issue_id = i.id
    WHERE i.kind = 'issue'
      AND i.assignee_type = 'agent'
      AND i.assignee_id IS NOT NULL
      AND i.start_date IS NOT NULL
      AND i.status NOT IN ('done', 'cancelled')
      AND i.start_date = date(now() AT TIME ZONE cerebro_safe_timezone(cu.timezone))
      AND (now() AT TIME ZONE cerebro_safe_timezone(cu.timezone))
          >= (i.start_date + COALESCE(t.start_time, '00:00'::time))
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
       e.assignee_type,
       e.kind,
       e.reminder_date
FROM eligible e
JOIN claimed c
  ON c.issue_id = e.issue_id
 AND c.kind = e.kind
 AND c.reminder_date = e.reminder_date;
