-- Cerebro-only queries that bind a runtime to the cerebro_account it is
-- currently authenticated with. The agent_runtime.current_account_id column
-- is added by migration 9019_cerebro_runtime_current_account; the table
-- itself is upstream so sqlc emits the column into both `db` and `cerebrodb`
-- AgentRuntime structs once the migration is applied.

-- name: SetAgentRuntimeCurrentAccount :one
UPDATE agent_runtime
SET current_account_id = $1,
    updated_at = now()
WHERE id = $2
  AND current_account_id IS DISTINCT FROM $1
RETURNING id, current_account_id;

-- name: GetAgentRuntimeCurrentAccountID :one
SELECT current_account_id
FROM agent_runtime
WHERE id = $1;
