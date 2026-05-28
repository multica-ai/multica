-- name: CreateAgentAuditLog :one
INSERT INTO multica_agent_audit_logs (agent_id, action, target_type, target_id, status_code, error_msg)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListAgentAuditLogs :many
SELECT * FROM multica_agent_audit_logs
WHERE agent_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: CountRecentAgentCalls :one
SELECT COUNT(*) FROM multica_agent_audit_logs
WHERE agent_id = $1
AND created_at > now() - interval '1 minute';

-- name: PruneOldAuditLogs :exec
DELETE FROM multica_agent_audit_logs
WHERE created_at < now() - interval '30 days';
