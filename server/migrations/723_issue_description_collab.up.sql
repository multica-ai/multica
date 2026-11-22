-- Live co-editing of issue descriptions (MUL-9778).
--
-- description_revision counts changes to the description alone. Live saves
-- and merges are checked against it instead of the issue-wide revision, which
-- every status, assignee, comment or metadata write also advances. A constant
-- default keeps the column add metadata-only.
--
-- issue_description_collab_state is the Yjs state behind the stored
-- description; issue_description_revision keeps the description of recent
-- revisions, so a write based on an older one is merged instead of refused.
-- Primary keys are built concurrently in 720–723.
ALTER TABLE issue ADD COLUMN IF NOT EXISTS description_revision BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS issue_description_collab_state (
    issue_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    ystate BYTEA NOT NULL,
    revision BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS issue_description_revision (
    issue_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    revision BIGINT NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
