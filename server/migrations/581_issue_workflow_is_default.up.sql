-- The workspace Default workflow becomes a real, editable row (MUL-7420).
-- Projects without a workflow_id use it. Before the row exists, Default is
-- the built-in statuses plus the custom statuses no other workflow lists;
-- the row is created from that snapshot the first time workflows are read
-- with the feature on, so statuses added for one workflow no longer pile up
-- in Default and in every cross-project board. At most one per workspace
-- (582).
ALTER TABLE issue_workflow ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT false;
