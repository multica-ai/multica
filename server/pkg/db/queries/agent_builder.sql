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

-- name: ListEmptyAgentBuilderSessions :many
-- Metadata only. Ordinary unfinished drafts intentionally exclude these rows.
-- Call again with session_id under session -> agent locks before discarding.
SELECT cs.id, a.runtime_id, ar.profile_id, cs.created_at,
       cs.updated_at AS session_updated_at, a.updated_at AS agent_updated_at,
       COALESCE(NULLIF(btrim(ar.custom_name), ''), ar.name)::text AS machine_label
FROM chat_session cs
JOIN agent a ON a.id = cs.agent_id AND a.workspace_id = cs.workspace_id
JOIN agent_runtime ar ON ar.id = a.runtime_id AND ar.workspace_id = cs.workspace_id
JOIN runtime_profile rp ON rp.id = ar.profile_id AND rp.workspace_id = cs.workspace_id
WHERE cs.workspace_id = @workspace_id AND cs.creator_id = @creator_id
  AND ar.profile_id = @profile_id
  AND cs.status = 'active' AND a.archived_at IS NULL
  AND a.kind = 'system' AND starts_with(a.system_key, 'agent_builder:')
  AND (sqlc.narg('session_id')::uuid IS NULL OR cs.id = sqlc.narg('session_id'))
  AND cs.id > @after_id::uuid
  AND NOT EXISTS (SELECT 1 FROM chat_message m WHERE m.chat_session_id = cs.id)
  AND NOT EXISTS (SELECT 1 FROM agent_builder_draft d WHERE d.chat_session_id = cs.id)
  AND NOT EXISTS (SELECT 1 FROM chat_draft_restore d WHERE d.chat_session_id = cs.id)
  AND NOT EXISTS (SELECT 1 FROM agent_task_queue t WHERE t.chat_session_id = cs.id OR t.agent_id = a.id)
  AND NOT EXISTS (SELECT 1 FROM chat_session other WHERE other.agent_id = a.id AND other.id <> cs.id)
ORDER BY cs.id
LIMIT @page_limit;
