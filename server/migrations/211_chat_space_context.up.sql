-- Chat is a workspace-level personal surface, but every session runs with an
-- explicit Space context. NULL means "All spaces currently available to both
-- the initiating member and the Agent"; a concrete id narrows the session to
-- one immutable Space.
ALTER TABLE chat_session
    ADD COLUMN space_id UUID;

ALTER TABLE chat_session
    ADD CONSTRAINT chat_session_workspace_space_fk
    FOREIGN KEY (workspace_id, space_id)
    REFERENCES workspace_space(workspace_id, id)
    ON DELETE RESTRICT;

CREATE INDEX idx_chat_session_workspace_space
    ON chat_session(workspace_id, space_id);

COMMENT ON COLUMN chat_session.space_id IS
    'Chat context. NULL = all Spaces available to the Agent and current initiating member; non-NULL = one immutable Space.';

-- A task token may now authorize more than one Space for an All-spaces Chat.
-- The existing scalar space_id remains the canonical fast path for every
-- single-Space task. Multi-Space requests must still name a concrete Space at
-- the API boundary; handlers validate it against this authoritative array.
ALTER TABLE task_token
    ADD COLUMN space_ids UUID[] NOT NULL DEFAULT '{}';

UPDATE task_token
SET space_ids = ARRAY[space_id]
WHERE space_id IS NOT NULL;

COMMENT ON COLUMN task_token.space_ids IS
    'Authoritative active Space allow-list for this run. Empty means no Space data access.';
