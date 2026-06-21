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
-- Agent auto-start is OPT-IN (FIR-1597). An agent-assigned issue only auto-starts
-- when the effective agent_start_kind resolves to the matching date AND that
-- date carries an explicit time-of-day. The effective kind is the per-issue
-- choice (cerebro_issue_date_time.agent_start_kind) falling back to the
-- workspace default (cerebro_workspace_settings.default_agent_start_kind),
-- defaulting to 'none'. Requiring a time means an opted-in run always fires at a
-- deliberate wall-clock moment, never silently at midnight.
--
-- Four branches:
--   1. due,   member-assigned -> notification ("Due today"), tz from assignee.
--   2. start, member-assigned -> notification ("Starts today"), tz from assignee.
--   3. start, agent-assigned, opted-in to 'start' -> auto-start the agent. tz
--      from the issue's member creator (the human who scheduled it), UTC fallback.
--   4. due,   agent-assigned, opted-in to 'due'   -> auto-start the agent. tz
--      from the issue's member creator, UTC fallback.
-- The Go sweeper auto-starts every agent-assigned claimed row and notifies the
-- member ones.
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
    LEFT JOIN cerebro_workspace_settings ws ON ws.workspace_id = i.workspace_id
    WHERE i.kind = 'issue'
      AND i.assignee_type = 'agent'
      AND i.assignee_id IS NOT NULL
      AND i.start_date IS NOT NULL
      AND i.status NOT IN ('done', 'cancelled')
      AND COALESCE(t.agent_start_kind, ws.default_agent_start_kind, 'none') = 'start'
      AND t.start_time IS NOT NULL
      AND i.start_date = date(now() AT TIME ZONE cerebro_safe_timezone(cu.timezone))
      AND (now() AT TIME ZONE cerebro_safe_timezone(cu.timezone))
          >= (i.start_date + t.start_time)
    UNION ALL
    SELECT i.id          AS issue_id,
           i.workspace_id,
           i.title,
           i.status,
           i.assignee_id,
           i.assignee_type,
           'due'::text    AS kind,
           i.due_date     AS reminder_date
    FROM issue i
    LEFT JOIN "user" cu ON cu.id = i.creator_id AND i.creator_type = 'member'
    LEFT JOIN cerebro_issue_date_time t ON t.issue_id = i.id
    LEFT JOIN cerebro_workspace_settings ws ON ws.workspace_id = i.workspace_id
    WHERE i.kind = 'issue'
      AND i.assignee_type = 'agent'
      AND i.assignee_id IS NOT NULL
      AND i.due_date IS NOT NULL
      AND i.status NOT IN ('done', 'cancelled')
      AND COALESCE(t.agent_start_kind, ws.default_agent_start_kind, 'none') = 'due'
      AND t.due_time IS NOT NULL
      AND i.due_date = date(now() AT TIME ZONE cerebro_safe_timezone(cu.timezone))
      AND (now() AT TIME ZONE cerebro_safe_timezone(cu.timezone))
          >= (i.due_date + t.due_time)
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
