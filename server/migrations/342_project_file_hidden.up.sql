-- Per-user "hide this file" preference scoped to a project. One row means the
-- member (user_id) has hidden a single attachment from their OWN view of a
-- project's file list. It is a personal view preference, not a shared curation
-- action: other members' lists are unaffected.
--
-- Deliberately no foreign keys or cascades (repo rule): relationships and
-- dependent cleanup live in the application layer. When an attachment is
-- deleted, DeleteAttachment reaps the now-orphaned rows in the same handler.
-- workspace_id is carried redundantly (project_id already implies it) to match
-- the project_resource shape and to keep workspace-scoped reads index-friendly.
CREATE TABLE project_file_hidden (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL,
    project_id    UUID NOT NULL,
    attachment_id UUID NOT NULL,
    user_id       UUID NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
