-- Per-issue overrides for platform-defined wakeup rules. A missing row means
-- the rule's default (enabled, no supplementary instruction). Only
-- 'child_done' exists today: wake the parent's assignee when a stage of its
-- sub-issues finishes. No foreign keys; issue and workspace deletion remove
-- these rows in the application deletion graph.
CREATE TABLE IF NOT EXISTS issue_system_wakeup (
 issue_id uuid NOT NULL,
 workspace_id uuid NOT NULL,
 rule text NOT NULL CHECK (rule IN ('child_done')),
 enabled boolean NOT NULL DEFAULT true,
 instruction text NOT NULL DEFAULT '',
 updated_by uuid,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
