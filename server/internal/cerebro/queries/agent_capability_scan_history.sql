-- CEREBRO-PATCH(agent-capability-scan-history): persistent per-agent history for
-- the nightly capability delta scanner.

-- name: GetAgentLatestCapabilityScan :one
SELECT *
FROM cerebro_agent_capability_scan
WHERE agent_id = sqlc.arg(agent_id)
ORDER BY window_end_at DESC
LIMIT 1;

-- name: ListAgentKnownCapabilityKeys :many
SELECT DISTINCT capability_key
FROM cerebro_agent_capability_scan_item
WHERE agent_id = sqlc.arg(agent_id)
  AND capability_type = sqlc.arg(capability_type)
ORDER BY capability_key;

-- name: CreateAgentCapabilityScan :one
INSERT INTO cerebro_agent_capability_scan (
    workspace_id,
    agent_id,
    window_start_at,
    window_end_at
) VALUES (
    sqlc.arg(workspace_id),
    sqlc.arg(agent_id),
    sqlc.arg(window_start_at),
    sqlc.arg(window_end_at)
)
RETURNING *;

-- name: UpdateAgentCapabilityScanCounts :one
UPDATE cerebro_agent_capability_scan
SET observed_capability_count = sqlc.arg(observed_capability_count),
    new_capability_count = sqlc.arg(new_capability_count),
    drift_capability_count = sqlc.arg(drift_capability_count),
    completed_at = sqlc.arg(completed_at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: InsertAgentCapabilityScanItem :one
INSERT INTO cerebro_agent_capability_scan_item (
    scan_id,
    workspace_id,
    agent_id,
    capability_type,
    capability_key,
    display_name,
    uses,
    first_seen_at,
    last_seen_at,
    permission,
    status,
    is_new
) VALUES (
    sqlc.arg(scan_id),
    sqlc.arg(workspace_id),
    sqlc.arg(agent_id),
    sqlc.arg(capability_type),
    sqlc.arg(capability_key),
    sqlc.arg(display_name),
    sqlc.arg(uses),
    sqlc.arg(first_seen_at),
    sqlc.arg(last_seen_at),
    sqlc.narg(permission),
    sqlc.arg(status),
    sqlc.arg(is_new)
)
RETURNING *;
