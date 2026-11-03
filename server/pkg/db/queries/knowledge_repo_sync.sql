-- name: GetKnowledgeRepoSync :one
SELECT * FROM knowledge_repo_sync WHERE workspace_id = $1;

-- name: UpsertKnowledgeRepoSync :one
INSERT INTO knowledge_repo_sync (workspace_id, enabled, repo_url, branch, docs_path, kb_key, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
ON CONFLICT (workspace_id) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    repo_url = EXCLUDED.repo_url,
    branch = EXCLUDED.branch,
    docs_path = EXCLUDED.docs_path,
    kb_key = EXCLUDED.kb_key,
    updated_at = now()
RETURNING *;

-- name: SetKnowledgeRepoSyncRun :exec
UPDATE knowledge_repo_sync SET last_synced_at = now(), last_error = $2 WHERE workspace_id = $1;

-- name: ListKnowledgeRepoFiles :many
SELECT * FROM knowledge_repo_file WHERE workspace_id = $1;

-- name: UpsertKnowledgeRepoFile :exec
INSERT INTO knowledge_repo_file (workspace_id, path, knowledge_link_id, content_hash, archived_at, synced_at)
VALUES ($1, $2, $3, $4, NULL, now())
ON CONFLICT (workspace_id, path) DO UPDATE SET
    knowledge_link_id = EXCLUDED.knowledge_link_id,
    content_hash = EXCLUDED.content_hash,
    archived_at = NULL,
    synced_at = now();

-- name: ArchiveKnowledgeRepoFile :exec
UPDATE knowledge_repo_file SET archived_at = now(), knowledge_link_id = NULL
WHERE workspace_id = $1 AND path = $2;
