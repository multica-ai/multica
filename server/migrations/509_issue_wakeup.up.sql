CREATE TABLE issue_wakeup (
 id uuid NOT NULL, workspace_id uuid NOT NULL, issue_id uuid NOT NULL,
 agent_id uuid NOT NULL, created_by uuid NOT NULL, source_task_id uuid,
 parent_comment_id uuid, instruction text NOT NULL,
 kind text NOT NULL CHECK (kind IN ('event','at','every','cron')),
 mode text NOT NULL CHECK (mode IN ('once','continuous')),
 event_types text[] NOT NULL DEFAULT '{}', filter_agent_id uuid, filter_task_id uuid,
 interval_seconds bigint, cron_expression text, timezone text NOT NULL DEFAULT 'UTC',
 next_fire_at timestamptz, enabled boolean NOT NULL DEFAULT true,
 disabled_at timestamptz, revision bigint NOT NULL DEFAULT 1,
 last_task_id uuid, last_error text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE issue_wakeup_receipt (
 id uuid NOT NULL, wakeup_id uuid NOT NULL, revision bigint NOT NULL,
 event_key text NOT NULL, event_type text NOT NULL, payload jsonb NOT NULL,
 task_id uuid, processed_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
