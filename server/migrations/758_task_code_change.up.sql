-- Per-run code changes: what the agent changed in each git checkout under the
-- run's working directory, captured by the daemon when the run ends (diff from
-- the commit the run started on to the working tree, untracked files
-- included). One row per task, replaced on each report. `repos` is a JSON
-- array of {path, branch, base, head, files[], files_truncated, patch,
-- patch_truncated}; the daemon caps each patch, so a row is read whole.
--
-- No foreign keys (repo rule): workspace teardown (DeleteTaskBatch and the
-- workspace leaf sweep) and issue deletion remove these rows in application
-- code. issue_id is copied from the task so issue deletion can find them
-- after the legacy issue -> agent_task_queue cascade has run.
CREATE TABLE IF NOT EXISTS task_code_change (
    task_id         UUID PRIMARY KEY,
    workspace_id    UUID NOT NULL,
    issue_id        UUID,
    repos           JSONB NOT NULL DEFAULT '[]'::jsonb,
    repos_truncated BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
