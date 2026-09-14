-- Device-flow bind session state (MUL-7340). These rows are the status
-- projection the install dialog polls; the device_code itself never lands here
-- (see migration 471). Every read is workspace-scoped so a session id leaked
-- across workspaces cannot be resolved.

-- name: CreateLarkInstallSession :one
-- Opens a session at 'pending'. gc_after stays NULL until the session reaches a
-- terminal state, so the sweep cannot take a live row out from under the dialog.
INSERT INTO lark_install_session (
    id, workspace_id, agent_id, initiator_id, expires_at
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: GetLarkInstallSession :one
-- Workspace-scoped read. A miss (wrong workspace, swept, or never existed) is
-- pgx.ErrNoRows, which the service maps to ErrRegistrationSessionNotFound —
-- deliberately indistinguishable so a session id cannot be probed across
-- workspaces.
SELECT * FROM lark_install_session
WHERE id = $1 AND workspace_id = $2;

-- name: MarkLarkInstallSessionSuccess :one
-- Terminal success. The `status = 'pending'` guard makes this idempotent
-- against a racing expiry writer: whoever lands first wins and the loser's
-- RETURNING is empty, so a late success cannot overwrite a recorded error (and
-- vice versa) — the user already saw the first outcome.
UPDATE lark_install_session
SET status          = 'success',
    installation_id = $3,
    gc_after        = $4,
    updated_at      = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'pending'
RETURNING *;

-- name: MarkLarkInstallSessionError :one
-- Terminal failure, same first-writer-wins guard as the success path.
UPDATE lark_install_session
SET status        = 'error',
    error_reason  = $3,
    error_message = $4,
    gc_after      = $5,
    updated_at    = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'pending'
RETURNING *;

-- name: SweepLarkInstallSessions :exec
-- Drops rows nobody can still be waiting on: terminal ones past gc_after, and
-- pending ones whose device_code expired long enough ago that the dialog has
-- already been told the QR died. Pending rows are kept until expires_at so a
-- status read during the live window always finds the row.
DELETE FROM lark_install_session
WHERE (gc_after IS NOT NULL AND gc_after < $1)
   OR (status = 'pending' AND expires_at < $2);
