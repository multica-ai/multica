-- Artifact: typed first-class outputs from agents and humans (reports, plans,
-- decisions, diagrams, notes). Scoped to an issue, a project, or the workspace.
-- Distinct from attachment (which is a file blob) and comment (which is a thread
-- entry): an artifact is a durable, addressable, renderable document.

CREATE TABLE artifact (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id   UUID REFERENCES project(id) ON DELETE CASCADE,
    issue_id     UUID REFERENCES issue(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('report', 'plan', 'decision', 'diagram', 'note')),
    title        TEXT NOT NULL,
    body         TEXT NOT NULL DEFAULT '',
    metadata     JSONB NOT NULL DEFAULT '{}'::jsonb,
    author_type  TEXT NOT NULL CHECK (author_type IN ('member', 'agent')),
    author_id    UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Scope is exactly one of: workspace (both null), project, or issue.
    CONSTRAINT artifact_single_scope CHECK (
        NOT (project_id IS NOT NULL AND issue_id IS NOT NULL)
    )
);

CREATE INDEX idx_artifact_workspace ON artifact(workspace_id, created_at DESC);
CREATE INDEX idx_artifact_project   ON artifact(project_id, created_at DESC) WHERE project_id IS NOT NULL;
CREATE INDEX idx_artifact_issue     ON artifact(issue_id, created_at DESC)   WHERE issue_id IS NOT NULL;
CREATE INDEX idx_artifact_kind      ON artifact(workspace_id, kind);
