-- A run's code change, captured by the daemon when the run ends (MUL-7651).
-- One row per (run, scope, repository):
--   scope 'run'    — what this run changed, from its start to its end.
--   scope 'branch' — the delivered branch against where its line of work
--                    started, as of this run's end. Backs the issue-wide view
--                    when no pull request can supply it. The daemon omits it
--                    when it would equal the run row.
-- The patch lives in object storage (patch_url). A patch over the daemon's cap
-- is never uploaded: the row keeps the file list and says why (patch_omitted).
--
-- No foreign keys: issue and workspace teardown delete these rows and their
-- stored patches in application code.
CREATE TABLE task_code_change (
    id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    task_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    scope TEXT NOT NULL CHECK (scope IN ('run', 'branch')),
    source TEXT NOT NULL CHECK (source IN ('local_worktree', 'repo_checkout')),
    repo_key TEXT NOT NULL,
    repo_label TEXT NOT NULL,
    repo_url TEXT NOT NULL DEFAULT '',
    branch TEXT NOT NULL DEFAULT '',
    base_ref TEXT NOT NULL DEFAULT '',
    base_commit TEXT NOT NULL,
    head_commit TEXT NOT NULL,
    file_count INTEGER NOT NULL,
    additions INTEGER NOT NULL,
    deletions INTEGER NOT NULL,
    files JSONB NOT NULL DEFAULT '[]'::jsonb,
    files_truncated BOOLEAN NOT NULL DEFAULT false,
    patch_url TEXT,
    patch_size BIGINT NOT NULL DEFAULT 0,
    patch_omitted TEXT CHECK (patch_omitted IN ('too_large', 'unavailable')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
