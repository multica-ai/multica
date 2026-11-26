-- Run file changes (the transcript's file diff feature). One row per task
-- holds the sanitized manifest of file modifications the runtime observed
-- during the run, plus optional directory-root bodies. Git-root content is
-- served on demand from the runtime's pinned trees and never stored here.
-- No foreign keys: workspace teardown sweeps rows by workspace_id in
-- application code, and task cleanup removes the row in the same statement
-- that removes the task.
CREATE TABLE IF NOT EXISTS run_file_change (
    workspace_id uuid NOT NULL,
    task_id uuid NOT NULL,
    collection_id uuid NOT NULL,
    manifest jsonb NOT NULL,
    bodies jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now()
);
