-- name: UpsertWorkspaceSecret :one
-- value_encrypted carries the AES-256-GCM ciphertext (with the 12-byte
-- nonce prepended) produced by server/pkg/secrets. Plaintext never
-- touches this table.
INSERT INTO workspace_secret (workspace_id, name, value_encrypted)
VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, name) DO UPDATE SET
    value_encrypted = EXCLUDED.value_encrypted,
    updated_at      = now()
RETURNING *;

-- name: GetWorkspaceSecret :one
SELECT * FROM workspace_secret
WHERE workspace_id = $1 AND name = $2;

-- name: DeleteWorkspaceSecret :exec
DELETE FROM workspace_secret
WHERE workspace_id = $1 AND name = $2;

-- name: ListWorkspaceSecrets :many
-- Returns the names only — value_encrypted is excluded so callers that
-- want a "what's set" answer don't accidentally pull ciphertext into
-- API responses.
SELECT workspace_id, name, created_at, updated_at FROM workspace_secret
WHERE workspace_id = $1
ORDER BY name ASC;

-- name: ListWorkspacesNeedingSecretMigration :many
-- One-shot startup helper: returns workspaces that still hold a
-- ship_hub.github_token in workspace.settings AND don't yet have an
-- equivalent workspace_secret row. The migrator processes each row,
-- writes the encrypted secret, then clears the settings-side copy.
SELECT id, settings FROM workspace
WHERE settings ? 'ship_hub'
  AND settings -> 'ship_hub' ? 'github_token'
  AND NOT EXISTS (
      SELECT 1 FROM workspace_secret ws
      WHERE ws.workspace_id = workspace.id AND ws.name = 'github_token'
  );

-- name: ClearShipHubTokenInSettings :exec
-- Idempotent JSON surgery: removes ship_hub.github_token from
-- workspace.settings without touching siblings. Empty ship_hub object
-- is left in place; pruning it would risk concurrent updates trampling
-- a fresh write. Used by the secret migrator after the encrypted copy
-- lands.
UPDATE workspace SET
    settings = jsonb_set(
        settings,
        '{ship_hub}',
        (settings -> 'ship_hub') - 'github_token',
        false
    )
WHERE id = $1
  AND settings ? 'ship_hub'
  AND settings -> 'ship_hub' ? 'github_token';

-- name: SetWorkspaceWebhookSecretPlaintext :exec
-- Plaintext fallback for environments without MULTICA_SECRET_ENCRYPTION_KEY.
-- The encrypted path is preferred and tried first; this column gives
-- bootstrap parity with Phase 1's settings.github_token storage style.
UPDATE workspace SET
    ship_hub_webhook_secret = $2,
    updated_at              = now()
WHERE id = $1;

-- name: GetWorkspaceWebhookSecretPlaintext :one
-- Companion read for the plaintext fallback.
SELECT ship_hub_webhook_secret FROM workspace
WHERE id = $1;

-- name: ListWorkspacesWithWebhookSecret :many
-- Webhook receiver scans this set to find which workspace's secret
-- verifies a given inbound HMAC signature. Cheap because the workspaces-
-- with-Ship-Hub-on subset is tiny relative to the webhook QPS, and the
-- alternative (a workspace_id header) cannot be trusted from an
-- unauthenticated endpoint.
SELECT id, ship_hub_webhook_secret FROM workspace
WHERE ship_hub_enabled = TRUE
  AND ship_hub_webhook_secret IS NOT NULL
  AND ship_hub_webhook_secret <> '';

-- name: ListWorkspacesWithEncryptedWebhookSecret :many
-- Returns workspaces that have a webhook secret stored in the
-- encrypted-at-rest table. Joined with ship_hub_enabled on the
-- workspace side so we never bother trying secrets from disabled
-- workspaces.
SELECT w.id AS workspace_id, ws.value_encrypted
FROM workspace w
JOIN workspace_secret ws
  ON ws.workspace_id = w.id AND ws.name = 'github_webhook_secret'
WHERE w.ship_hub_enabled = TRUE;
