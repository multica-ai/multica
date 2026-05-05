-- Project-level access control.
--
-- access = 'workspace' (default): every workspace member sees the project
-- and everything inside it. Status quo for existing data.
--
-- access = 'restricted': only listed project_member rows + workspace
-- owners/admins (the always-on governance safety net) can see the project,
-- its issues, comments, attachments, artifacts, and real-time events.
--
-- is_private on issue is the standalone-issue escape hatch — for issues
-- without a project_id, creator + workspace admins only.

-- Idempotent: TestMigrationsAreIdempotent re-runs every up migration on a
-- DB that already has them applied, so every statement uses IF NOT EXISTS
-- (or is wrapped in DO/EXCEPTION) to be safe on the second pass.

ALTER TABLE project
    ADD COLUMN IF NOT EXISTS access TEXT NOT NULL DEFAULT 'workspace';

DO $$ BEGIN
    ALTER TABLE project ADD CONSTRAINT project_access_check
        CHECK (access IN ('workspace', 'restricted'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS project_member (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id   UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES "user"(id)   ON DELETE CASCADE,
    added_by     UUID REFERENCES "user"(id) ON DELETE SET NULL,
    added_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_project_member_user      ON project_member(user_id, project_id);
CREATE INDEX IF NOT EXISTS idx_project_member_workspace ON project_member(workspace_id);

ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS is_private BOOLEAN NOT NULL DEFAULT FALSE;

-- Performance: ListIssues filters by project_id frequently; restricted-access
-- filtering will JOIN project_member via user_id, so the existing
-- (workspace_id, ...) indexes still cover the common path.
