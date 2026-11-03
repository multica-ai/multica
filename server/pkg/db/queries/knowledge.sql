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

-- name: GetKnowledgeBaseByKey :one
SELECT * FROM knowledge_base WHERE workspace_id = $1 AND key = $2 AND archived_at IS NULL;

-- name: ClaimKnowledgeLink :execrows
-- The idempotency claim of a write: the row exists before WeKnora is asked,
-- in state 'failed' with the pending content, so a crash or an outage leaves
-- something a retry can finish. A repeated key is a no-op.
INSERT INTO knowledge_link (
    workspace_id, knowledge_base_id, weknora_knowledge_id, source_type, source_id,
    idempotency_key, state, created_by_type, created_by_id,
    title, source_url, pending_content, publish_requested, error,
    kind, initiative_id, stage_key, squad_id, confidence, valid_until, supersedes_link_id, tags
) VALUES (
    $1, $2, '', $3, $4, $5, 'failed', $6, $7, $8, $9, $10, $11, 'not sent yet',
    $12, $13, $14, $15, $16, $17, $18, $19
)
ON CONFLICT (workspace_id, idempotency_key) DO NOTHING;

-- name: GetKnowledgeLinkByKey :one
SELECT * FROM knowledge_link WHERE workspace_id = $1 AND idempotency_key = $2;

-- name: GetKnowledgeLink :one
SELECT * FROM knowledge_link WHERE id = $1 AND workspace_id = $2;

-- name: SettleKnowledgeLink :one
-- Records the outcome of a write or publish: the WeKnora document, the new
-- state, and the failure text (empty on success). Pending content is dropped
-- once the document exists.
UPDATE knowledge_link SET
    weknora_knowledge_id = sqlc.arg('weknora_knowledge_id')::text,
    state = sqlc.arg('state')::text,
    error = sqlc.arg('error')::text,
    pending_content = CASE WHEN sqlc.arg('state')::text = 'failed' THEN pending_content ELSE '' END,
    published_at = CASE WHEN sqlc.arg('state')::text = 'published' THEN COALESCE(published_at, now()) ELSE published_at END
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: ListKnowledgeLinks :many
-- Newest first. knowledge_base_id and state are optional filters.
SELECT * FROM knowledge_link
WHERE workspace_id = sqlc.arg('workspace_id')
  AND (sqlc.narg('knowledge_base_id')::uuid IS NULL OR knowledge_base_id = sqlc.narg('knowledge_base_id')::uuid)
  AND (sqlc.narg('state')::text IS NULL OR state = sqlc.narg('state')::text)
ORDER BY created_at DESC, id DESC
LIMIT 500;

-- name: ListRetryableKnowledgeLinks :many
-- Writes WeKnora has not accepted yet whose author is Multica itself.
SELECT * FROM knowledge_link
WHERE workspace_id = $1 AND state = 'failed' AND created_by_type = 'system'
ORDER BY created_at ASC
LIMIT 20;

-- name: DeleteKnowledgeLink :execrows
DELETE FROM knowledge_link WHERE id = $1 AND workspace_id = $2;

-- name: GetKnowledgeLinksByDocumentIDs :many
-- The taxonomy of the documents a search returned. Documents WeKnora holds
-- without a link come back as no row.
SELECT * FROM knowledge_link
WHERE workspace_id = @workspace_id
  AND weknora_knowledge_id = ANY(@document_ids::text[])
  AND state = 'published';

-- name: ListSupersededLinkIDs :many
-- Links some published document supersedes. Search hides them.
SELECT supersedes_link_id FROM knowledge_link
WHERE workspace_id = $1 AND state = 'published' AND supersedes_link_id IS NOT NULL;

-- name: ListPublishedKnowledgeLinks :many
-- Every published link of a workspace, for the freshness review and the
-- taxonomy audit. Bounded; a workspace past the bound is audited in part and
-- the job says so.
SELECT * FROM knowledge_link
WHERE workspace_id = $1 AND state = 'published'
ORDER BY created_at DESC, id DESC
LIMIT 5000;

-- name: SetKnowledgeLinkReview :one
-- Records a critic's verdict. It does not change the document's state; the
-- caller publishes it when the policy allows.
UPDATE knowledge_link SET
    review_verdict = sqlc.arg('review_verdict')::text,
    review_by_id = sqlc.narg('review_by_id')::uuid,
    review_squad_id = sqlc.narg('review_squad_id')::uuid,
    review_note = sqlc.arg('review_note')::text,
    reviewed_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: SetKnowledgeLinkReviewIssue :exec
-- Records the freshness review request the Librarian opened for a document.
UPDATE knowledge_link SET review_issue_id = sqlc.arg('review_issue_id')::uuid
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id');
