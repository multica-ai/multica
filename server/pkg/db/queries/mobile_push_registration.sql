-- name: UpsertMobilePushRegistration :one
INSERT INTO mobile_push_registration (
    user_id,
    installation_id,
    platform,
    provider,
    provider_client_id,
    app_version,
    enabled,
    last_seen_at
) VALUES ($1, $2, $3, $4, $5, $6, true, now())
ON CONFLICT (user_id, installation_id, provider)
DO UPDATE SET
    platform = EXCLUDED.platform,
    provider_client_id = EXCLUDED.provider_client_id,
    app_version = EXCLUDED.app_version,
    enabled = true,
    last_seen_at = now(),
    updated_at = now()
RETURNING *;

-- name: DisableMobilePushRegistration :exec
UPDATE mobile_push_registration
SET enabled = false,
    updated_at = now()
WHERE user_id = $1
  AND installation_id = $2
  AND provider = $3;

-- name: GetMobilePushRegistrationByInstallation :one
SELECT *
FROM mobile_push_registration
WHERE user_id = $1
  AND installation_id = $2
  AND provider = $3;
