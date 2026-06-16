-- Squad: a collaborative unit that can be @mentioned or assigned to issues.
CREATE TABLE squad (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    leader_id UUID NOT NULL REFERENCES agent(id) ON DELETE RESTRICT,
    creator_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, name)
);

CREATE INDEX idx_squad_workspace ON squad(workspace_id);

-- Squad members: agents or workspace members belonging to a squad.
CREATE TABLE squad_member (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    squad_id UUID NOT NULL REFERENCES squad(id) ON DELETE CASCADE,
    member_type TEXT NOT NULL CHECK (member_type IN ('agent', 'member')),
    member_id UUID NOT NULL,
    role TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(squad_id, member_type, member_id)
);

CREATE INDEX idx_squad_member_squad ON squad_member(squad_id);
CREATE INDEX idx_squad_member_entity ON squad_member(member_type, member_id);

-- Extend issue assignee_type to support 'squad'.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_assignee_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_assignee_type_check
    CHECK (assignee_type IN ('member', 'agent', 'squad'));
