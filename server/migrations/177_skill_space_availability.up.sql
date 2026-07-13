-- Skills remain Workspace-owned reusable assets. Availability controls where
-- members and Agents may discover/use a Skill; it never grants Space data,
-- Integration, Secret, or Resource access.
ALTER TABLE skill
    ADD COLUMN availability_mode TEXT NOT NULL DEFAULT 'workspace'
        CHECK (availability_mode IN ('private', 'selected_spaces', 'workspace'));

-- Existing Skills were visible to the whole Workspace before this migration,
-- so preserve that behavior. New Skills default to Private (least privilege).
ALTER TABLE skill ALTER COLUMN availability_mode SET DEFAULT 'private';

ALTER TABLE skill
    ADD CONSTRAINT skill_workspace_id_id_unique UNIQUE (workspace_id, id);

CREATE TABLE skill_available_space (
    skill_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    space_id UUID NOT NULL,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (skill_id, space_id),
    FOREIGN KEY (workspace_id, skill_id)
        REFERENCES skill(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, space_id)
        REFERENCES workspace_space(workspace_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_skill_available_space_workspace_space
    ON skill_available_space(workspace_id, space_id, skill_id);

COMMENT ON COLUMN skill.availability_mode IS
    'Sharing scope only: private, selected_spaces, or workspace. Does not grant data or Integration access.';

COMMENT ON TABLE skill_available_space IS
    'Selected-Space sharing grants for Skills. A row changes discovery/use scope only and grants no underlying Space access.';
