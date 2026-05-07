-- CEREBRO-PATCH(sqlc-budget): cerebro modification of upstream file
-- name: GetWorkspaceBudgetConfig :one
-- Returns NULL row (sql.ErrNoRows) when the workspace owner hasn't completed
-- the setup wizard yet. Callers treat that as "no caps configured".
SELECT * FROM workspace_budget_config
WHERE workspace_id = $1;

-- name: GetUserBudgetOverride :one
SELECT * FROM user_budget_override
WHERE workspace_id = $1 AND user_id = $2;

-- name: GetAgentBudgetOverride :one
SELECT * FROM agent_budget_override
WHERE workspace_id = $1 AND agent_id = $2;

-- name: GetBudgetState :one
SELECT * FROM budget_state
WHERE workspace_id = $1
  AND scope_type = $2
  AND scope_id = $3
  AND window_type = $4
  AND window_start = $5;

-- name: IncrementBudgetState :exec
-- Atomic upsert + increment. Inserts a fresh row at cents_spent = $6 if no
-- (workspace, scope, window) row exists, otherwise adds $6 to whatever was
-- already there. Safe to call concurrently — Postgres ON CONFLICT serializes
-- on the primary key.
INSERT INTO budget_state (workspace_id, scope_type, scope_id, window_type, window_start, cents_spent, last_updated)
VALUES ($1, $2, $3, $4, $5, $6, now())
ON CONFLICT (workspace_id, scope_type, scope_id, window_type, window_start)
DO UPDATE SET
    cents_spent = budget_state.cents_spent + EXCLUDED.cents_spent,
    last_updated = now();

-- name: ListBudgetStateForWindow :many
-- Used by the admin overview to render per-(user|agent) totals for a window.
SELECT * FROM budget_state
WHERE workspace_id = $1
  AND scope_type = $2
  AND window_type = $3
  AND window_start = $4
ORDER BY cents_spent DESC;
