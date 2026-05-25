-- FIR-2130: roles as a first-class permission subject.
--
-- A role is a named subject that grants attach to (subject_type='role'). Roles
-- are assigned to members and agents; the permission resolver expands an
-- actor's assigned role IDs into Actor.RoleIDs and matches role-scoped grants
-- against them. This mirrors cerebro_group but a role's assignees can be either
-- members or agents, so the assignment table carries (subject_type, subject_id)
-- rather than a user_id FK.

CREATE TABLE IF NOT EXISTS cerebro_role (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_role_workspace_name_unique UNIQUE (workspace_id, name)
);

CREATE TABLE IF NOT EXISTS cerebro_role_assignment (
    role_id UUID NOT NULL REFERENCES cerebro_role(id) ON DELETE CASCADE,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('member', 'agent')),
    subject_id UUID NOT NULL,
    added_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_role_assignment_unique UNIQUE (role_id, subject_type, subject_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_role_workspace
    ON cerebro_role (workspace_id);

CREATE INDEX IF NOT EXISTS idx_cerebro_role_assignment_subject
    ON cerebro_role_assignment (subject_type, subject_id);
