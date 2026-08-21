-- name: UpsertAgentBuilderDraft :one
-- The current configuration of one creation conversation. Last write wins:
-- there is a single editor (the session's creator, on one screen at a time) and
-- the client autosaves the whole object, so a partial merge would only be able
-- to reconstruct a state the user never saw.
INSERT INTO agent_builder_draft (chat_session_id, workspace_id, draft)
VALUES (@chat_session_id, @workspace_id, @draft)
ON CONFLICT (chat_session_id) DO UPDATE
SET draft = EXCLUDED.draft,
    updated_at = now()
RETURNING *;

-- name: DeleteAgentBuilderDraft :exec
-- agent_builder_draft has no chat_session FK (repo rule), so every path that
-- removes a conversation has to say so explicitly.
DELETE FROM agent_builder_draft WHERE chat_session_id = $1;

-- name: DeleteAgentBuilderDraftsBySystemRuntimeAgents :exec
-- chat_session cascades from agent, so a runtime teardown hard-deleting its
-- system carriers (DeleteSystemAgentsByRuntime) silently drops their sessions.
-- Prune the drafts in the same tx and BEFORE the agent rows go — the join below
-- needs them. Mirrors DeleteChatDraftRestoresBySystemRuntimeAgents.
--
-- Only the system variant exists: a builder draft can only ever hang off a
-- `kind = 'system'` carrier, so the archived-agent prune has nothing to find.
DELETE FROM agent_builder_draft
WHERE chat_session_id IN (
    SELECT cs.id FROM chat_session cs
    JOIN agent a ON a.id = cs.agent_id
    WHERE a.runtime_id = $1 AND a.kind = 'system'
);
