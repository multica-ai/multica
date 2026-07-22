-- name: UpsertClientUsageDaily :one
INSERT INTO client_usage_daily (
    user_id,
    client_type,
    install_id,
    activity_date,
    workspace_id,
    client_version,
    os,
    first_active_at,
    last_active_at,
    runtime_probed_at,
    probe_result,
    runtime_count,
    provider_summary,
    online_count,
    offline_count
) VALUES (
    sqlc.arg('user_id'),
    sqlc.arg('client_type'),
    sqlc.arg('install_id'),
    (CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date,
    sqlc.narg('workspace_id'),
    sqlc.arg('client_version'),
    sqlc.arg('os'),
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP,
    CASE WHEN sqlc.arg('has_runtime_probe')::boolean THEN CURRENT_TIMESTAMP ELSE NULL END,
    sqlc.narg('probe_result'),
    sqlc.narg('runtime_count'),
    sqlc.narg('provider_summary'),
    sqlc.narg('online_count'),
    sqlc.narg('offline_count')
)
ON CONFLICT (user_id, client_type, install_id, activity_date) DO UPDATE SET
    workspace_id = COALESCE(EXCLUDED.workspace_id, client_usage_daily.workspace_id),
    client_version = EXCLUDED.client_version,
    os = EXCLUDED.os,
    last_active_at = EXCLUDED.last_active_at,
    runtime_probed_at = CASE WHEN sqlc.arg('has_runtime_probe')::boolean THEN EXCLUDED.runtime_probed_at ELSE client_usage_daily.runtime_probed_at END,
    probe_result = CASE WHEN sqlc.arg('has_runtime_probe')::boolean THEN EXCLUDED.probe_result ELSE client_usage_daily.probe_result END,
    runtime_count = CASE WHEN sqlc.arg('has_runtime_probe')::boolean THEN EXCLUDED.runtime_count ELSE client_usage_daily.runtime_count END,
    provider_summary = CASE WHEN sqlc.arg('has_runtime_probe')::boolean THEN EXCLUDED.provider_summary ELSE client_usage_daily.provider_summary END,
    online_count = CASE WHEN sqlc.arg('has_runtime_probe')::boolean THEN EXCLUDED.online_count ELSE client_usage_daily.online_count END,
    offline_count = CASE WHEN sqlc.arg('has_runtime_probe')::boolean THEN EXCLUDED.offline_count ELSE client_usage_daily.offline_count END,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;
