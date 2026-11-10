-- MUL-8472: membership of a private project. Rows only matter while the
-- project is private; a workspace-visible project keeps its rows so flipping
-- it back to private restores the same list.
--
-- No foreign keys (repo rule): project deletion, member removal and workspace
-- teardown delete these rows in application code.
CREATE TABLE IF NOT EXISTS project_member (
    workspace_id UUID NOT NULL,
    project_id   UUID NOT NULL,
    user_id      UUID NOT NULL,
    added_by     UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);
