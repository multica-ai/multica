-- A running Agent must never inherit its runtime owner's workspace-wide data
-- reach. Bind every task credential to the one Space that produced the task.
-- Context-free tasks (currently direct Chat) keep NULL and therefore cannot
-- access Space-backed work through their task token.
ALTER TABLE task_token
    ADD COLUMN space_id UUID;

ALTER TABLE task_token
    ADD CONSTRAINT task_token_workspace_space_fk
    FOREIGN KEY (workspace_id, space_id)
    REFERENCES workspace_space(workspace_id, id)
    ON DELETE CASCADE;

CREATE INDEX idx_task_token_workspace_space
    ON task_token(workspace_id, space_id);

COMMENT ON COLUMN task_token.space_id IS
    'The single Space data boundary for this run. NULL means the task has no Space data access.';

COMMENT ON COLUMN agent.availability_mode IS
    'Space assignment. private = owner only in a concrete active Space they can use; selected_spaces = only rows in agent_available_space; workspace = every active Space. Each run is still bound to exactly one Space.';

COMMENT ON TABLE agent_available_space IS
    'Selected-Space assignments for agents with availability_mode=selected_spaces. A run may receive read/write access to its one bound Space only.';
