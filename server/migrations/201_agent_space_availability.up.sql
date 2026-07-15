-- Agent availability is its Space assignment, separate from invocation audience
-- (`permission_mode` + `agent_invocation_target`). A concrete run receives data
-- access to exactly one assigned Space through its task-scoped token.
--
--   private         -> only the agent owner; any active Space they may use
--   selected_spaces -> only the explicitly selected active Spaces
--   workspace       -> every active Space in the workspace
--
-- Existing agents are backfilled conservatively from their invocation mode:
-- private agents stay private; public_to agents keep their previous
-- workspace-wide location reach. Their member/team invocation targets remain
-- untouched and continue to narrow who may invoke them.
ALTER TABLE agent
    ADD COLUMN availability_mode TEXT NOT NULL DEFAULT 'private'
        CHECK (availability_mode IN ('private', 'selected_spaces', 'workspace'));

UPDATE agent
SET availability_mode = CASE
    WHEN permission_mode = 'public_to' THEN 'workspace'
    ELSE 'private'
END;

ALTER TABLE agent
    ADD CONSTRAINT agent_workspace_id_id_unique UNIQUE (workspace_id, id);

CREATE TABLE agent_available_space (
    agent_id    UUID NOT NULL,
    workspace_id UUID NOT NULL,
    space_id    UUID NOT NULL,
    created_by  UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, space_id),
    FOREIGN KEY (workspace_id, agent_id)
        REFERENCES agent(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, space_id)
        REFERENCES workspace_space(workspace_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_agent_available_space_workspace_space
    ON agent_available_space(workspace_id, space_id, agent_id);

COMMENT ON COLUMN agent.availability_mode IS
    'Space assignment. private = owner only in a concrete active Space they can use; selected_spaces = only rows in agent_available_space; workspace = every active Space. Each run is still bound to exactly one Space.';

COMMENT ON TABLE agent_available_space IS
    'Selected-Space assignments for agents with availability_mode=selected_spaces. A run may receive read/write access to its one bound Space only.';
