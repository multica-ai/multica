-- Durable row-lockable automation state (MUL-4332): rising-edge latches (and,
-- later, stage frontier + rate buckets). Not an audit log; one row per key.

-- name: GetAutomationStateForUpdate :one
-- Row-lock a state key so concurrent matchers serialize their read-modify-write
-- of the same latch. Returns no rows the first time a key is used.
SELECT * FROM automation_state
WHERE workspace_id = $1 AND state_kind = $2 AND state_key = $3
FOR UPDATE;

-- name: UpsertAutomationState :one
INSERT INTO automation_state (workspace_id, state_kind, state_key, state, version)
VALUES ($1, $2, $3, $4, 0)
ON CONFLICT (workspace_id, state_kind, state_key)
DO UPDATE SET state = EXCLUDED.state, version = automation_state.version + 1, updated_at = now()
RETURNING *;
