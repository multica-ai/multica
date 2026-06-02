-- name: CreateAgentSession :one
INSERT INTO agent_sessions (
    id, issue_id, agent_id, run_number, state,
    conversation_summary, working_directory, branch,
    files_modified, is_active, expires_at, version
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8,
    $9, $10, $11, $12
)
RETURNING *;

-- name: GetActiveSession :one
-- Returns the currently active session for an issue+agent pair (with row-level lock).
SELECT * FROM agent_sessions
WHERE issue_id = $1 AND agent_id = $2 AND is_active = true
ORDER BY run_number DESC
LIMIT 1
FOR UPDATE;

-- name: GetActiveSessionNoLock :one
-- Read-only variant of GetActiveSession without FOR UPDATE.
SELECT * FROM agent_sessions
WHERE issue_id = $1 AND agent_id = $2 AND is_active = true
ORDER BY run_number DESC
LIMIT 1;

-- name: UpdateSessionState :exec
-- Atomic state update with optimistic concurrency (version check).
UPDATE agent_sessions
SET state = $2,
    conversation_summary = $3,
    files_modified = $4,
    working_directory = $5,
    branch = $6,
    last_active_at = now(),
    version = version + 1
WHERE id = $1 AND version = $7;

-- name: DeactivateSession :exec
UPDATE agent_sessions
SET is_active = false, last_active_at = now()
WHERE id = $1 AND is_active = true;

-- name: DeactivateSessionsByIssueAndAgent :exec
-- Deactivates all active sessions for a given issue+agent pair.
UPDATE agent_sessions
SET is_active = false, last_active_at = now()
WHERE issue_id = $1 AND agent_id = $2 AND is_active = true;

-- name: ExpireStaleSessions :many
-- Marks active sessions stale before cutoff as inactive.
UPDATE agent_sessions
SET is_active = false, expires_at = now()
WHERE is_active = true AND last_active_at < $1
RETURNING id, issue_id, agent_id;

-- name: GetLatestRunNumber :one
SELECT COALESCE(MAX(run_number), 0)::int AS run_number
FROM agent_sessions
WHERE issue_id = $1 AND agent_id = $2;
