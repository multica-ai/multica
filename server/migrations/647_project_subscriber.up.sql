-- Members who watch every issue in a project. Issue-level subscriptions stay
-- the source of per-issue intent; this row only adds the project's issues to
-- the member's notification fanout.
--
-- No foreign keys (repo rule): project deletion, member removal and workspace
-- teardown delete these rows in application code.
CREATE TABLE IF NOT EXISTS project_subscriber (
    workspace_id UUID NOT NULL,
    project_id   UUID NOT NULL,
    user_id      UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);
