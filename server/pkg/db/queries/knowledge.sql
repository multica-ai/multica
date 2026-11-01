-- name: GetWorkspaceKnowledge :one
SELECT * FROM workspace_knowledge WHERE workspace_id = $1;

-- name: CreateWorkspaceKnowledge :execrows
-- First provisioning step done: the tenant exists and its key is sealed. A
-- concurrent or repeated insert is a no-op; the provisioning lock serializes
-- the callers that matter.
INSERT INTO workspace_knowledge (workspace_id, weknora_tenant_id, api_key_ciphertext, state)
VALUES ($1, $2, $3, 'provisioning')
ON CONFLICT (workspace_id) DO NOTHING;

-- name: SetWorkspaceKnowledgeState :execrows
UPDATE workspace_knowledge SET state = $2 WHERE workspace_id = $1;

-- name: ListKnowledgeBases :many
-- Live knowledge bases in display order.
SELECT * FROM knowledge_base
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY position ASC, created_at ASC;

-- name: CreateKnowledgeBase :execrows
INSERT INTO knowledge_base (workspace_id, weknora_kb_id, key, name, visibility, position)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, key) DO NOTHING;

-- name: GetWorkspaceMcpServerByName :one
SELECT * FROM workspace_mcp_server WHERE workspace_id = $1 AND name = $2;

-- name: AssignMcpServerToLiveAgents :execrows
-- Gives a library server to every live agent of the workspace. System agents
-- (builder carriers) are skipped, like in agent lists. Existing bindings,
-- including ones an owner switched off, are left alone.
INSERT INTO agent_mcp_server (agent_id, server_id)
SELECT a.id, @server_id::uuid
FROM agent a
WHERE a.workspace_id = @workspace_id AND a.archived_at IS NULL AND a.kind = 'user'
ON CONFLICT DO NOTHING;

-- name: AssignKnowledgeMcpToAgent :exec
-- Creation hook: a new agent receives the read-only company-read server when
-- the workspace has finished enabling knowledge. A no-op otherwise.
INSERT INTO agent_mcp_server (agent_id, server_id)
SELECT @agent_id::uuid, s.id
FROM workspace_mcp_server s
JOIN workspace_knowledge wk ON wk.workspace_id = s.workspace_id
WHERE s.workspace_id = @workspace_id AND s.name = 'company-read' AND wk.state = 'ready'
ON CONFLICT DO NOTHING;
