-- WeCom scan-code install session queries (spec §3.3, §4, §7.1.1).
--
-- The session survives across replicas: the HTTP handler inserts a `creating`
-- row via LockWecomInstallBeginWorkspace + insert; the install worker on any
-- replica claims due `creating` / `pending` rows via ClaimDueWecomInstallSession
-- using an expiring lease so a crashed worker cannot indefinitely hold the row.
--
-- No FK / no CASCADE (repo hard rule). workspace_id-scoped rows are cleaned up
-- inside DeleteWorkspaceLeafData; the install worker also purges terminal rows
-- via PurgeTerminalWecomInstallSessions every minute.

-- name: LockWecomInstallBeginWorkspace :exec
-- Serializes the read-then-insert admission decision (request-hash dedupe,
-- pending-session recovery, active-installation guard, and 10-minute rate
-- window count) across replicas for one (workspace) key. Inline hashtext
-- avoids a table-locking pg_advisory_xact_lock namespace collision — the
-- 'wecom_install_begin' string keys this namespace apart from any other
-- workspace-scoped advisory lock we may add later.
SELECT pg_advisory_xact_lock(
    hashtext('wecom_install_begin'),
    hashtext((sqlc.arg(workspace_id)::uuid)::text)
);

-- name: GetWecomInstallSessionByRequestHash :one
-- Idempotent replay lookup for the Idempotency-Key header. Same
-- (workspace, initiator, request_key_hash) always returns the same session
-- (indexed by idx_wecom_install_session_request UNIQUE). Cross-agent replay
-- is caught by the caller comparing the session's agent_id.
SELECT * FROM wecom_install_session
WHERE workspace_id = sqlc.arg('workspace_id')
  AND initiator_user_id = sqlc.arg('initiator_user_id')
  AND request_key_hash = sqlc.arg('request_key_hash');

-- name: GetPendingWecomInstallSessionByAgent :one
-- Recovery lookup for the (workspace, agent) pending slot. At most one row
-- per (workspace, agent) has status IN ('creating', 'pending') (indexed by
-- idx_wecom_install_session_agent_pending UNIQUE); a different Idempotency-Key
-- from the same initiator or a workspace admin returns the existing session,
-- other callers get 409 install_in_progress at the handler layer.
SELECT * FROM wecom_install_session
WHERE workspace_id = sqlc.arg('workspace_id')
  AND agent_id = sqlc.arg('agent_id')
  AND status IN ('creating', 'pending');

-- name: CountWecomInstallSessionsInWindow :one
-- Rate-window count for the (workspace, initiator) 10-minute budget. Called
-- under the LockWecomInstallBeginWorkspace advisory lock so a burst of
-- concurrent begins from the same user cannot slip past the cap. Includes
-- every status — failed and terminated sessions still consumed a quota slot
-- via generate.
SELECT
    count(*)                                                                   AS total,
    count(*) FILTER (WHERE initiator_user_id = sqlc.arg('initiator_user_id'))  AS by_user
FROM wecom_install_session
WHERE workspace_id = sqlc.arg('workspace_id')
  AND created_at >= sqlc.arg('since')::timestamptz;

-- name: GetActiveWecomInstallationForAgent :one
-- Existing active WeCom installation guard for begin (spec §4). One row per
-- (workspace, agent, channel_type='wecom') can be active at a time; the
-- handler returns 409 installation_conflict here rather than starting an
-- install that would fail at finalize.
SELECT * FROM channel_installation
WHERE workspace_id = sqlc.arg('workspace_id')
  AND agent_id = sqlc.arg('agent_id')
  AND channel_type = 'wecom'
  AND status = 'active';

-- name: CreateWecomInstallSession :one
-- Inserts the fresh `creating` row after LockWecomInstallBeginWorkspace has
-- ruled out replay/pending/active/rate-limit. `poll_after=now()` explicitly
-- so ClaimDueWecomInstallSession picks it up on the very next sweep — the
-- claim predicate is `poll_after <= now()`, not "NULL means due", and this
-- avoids that ambiguity.
INSERT INTO wecom_install_session (
    workspace_id, agent_id, initiator_user_id, request_key_hash, status, poll_after
) VALUES (
    sqlc.arg('workspace_id'),
    sqlc.arg('agent_id'),
    sqlc.arg('initiator_user_id'),
    sqlc.arg('request_key_hash'),
    'creating',
    now()
)
RETURNING *;

-- name: GetWecomInstallSession :one
SELECT * FROM wecom_install_session WHERE id = sqlc.arg('id');

-- name: ClaimDueWecomInstallSession :one
-- Install worker's claim primitive. Picks the earliest due `creating` or
-- `pending` row whose lease is absent or expired, and rewrites the lease
-- atomically. Backed by idx_wecom_install_session_due (partial index on
-- status IN ('creating','pending') ordered by poll_after, created_at).
-- FOR UPDATE SKIP LOCKED lets multiple replicas share the workload without
-- blocking each other on hot rows.
UPDATE wecom_install_session
SET lease_token       = sqlc.arg('lease_token'),
    lease_expires_at  = sqlc.arg('lease_expires_at'),
    updated_at        = now()
WHERE id = (
    SELECT id FROM wecom_install_session
    WHERE status IN ('creating', 'pending')
      AND poll_after <= now()
      AND (lease_expires_at IS NULL OR lease_expires_at <= now())
    ORDER BY poll_after, created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: DeferClaimedWecomInstallSession :execrows
-- Releases the current lease and schedules the next poll. Used when the
-- upstream generate/query call yielded init/pending (not terminal) — worker
-- steps back so any replica can pick up when poll_after arrives.
-- Match-on-lease so a stale worker whose lease already expired cannot
-- clobber a new owner's claim.
UPDATE wecom_install_session
SET lease_token       = NULL,
    lease_expires_at  = NULL,
    poll_after        = sqlc.arg('poll_after'),
    status            = sqlc.arg('status'),
    scode_encrypted   = COALESCE(sqlc.narg('scode_encrypted'),
                                 wecom_install_session.scode_encrypted),
    qr_code_url_encrypted = COALESCE(sqlc.narg('qr_code_url_encrypted'),
                                     wecom_install_session.qr_code_url_encrypted),
    expires_at        = COALESCE(sqlc.narg('expires_at'), wecom_install_session.expires_at),
    updated_at        = now()
WHERE id = sqlc.arg('id')
  AND lease_token = sqlc.arg('lease_token')
  AND status IN ('creating', 'pending');

-- name: CompleteWecomInstallSession :execrows
-- Finalize-success terminator. Wipes short-lived ciphertext, records the
-- installation id, flips status to 'success'. Fenced on lease_token AND
-- status='pending' so a stale worker cannot override the owner's success.
UPDATE wecom_install_session
SET status                  = 'success',
    installation_id         = sqlc.arg('installation_id'),
    scode_encrypted         = NULL,
    qr_code_url_encrypted   = NULL,
    lease_token             = NULL,
    lease_expires_at        = NULL,
    updated_at              = now()
WHERE id = sqlc.arg('id')
  AND lease_token = sqlc.arg('lease_token')
  AND status = 'pending';

-- name: FailWecomInstallSession :execrows
-- Terminal failure. Idempotent: only mutates a still-active session. Wipes
-- short-lived ciphertext so a leaked DB dump cannot resurrect the QR after
-- the fact. Lease is dropped so the row is not re-claimed.
-- Match-on-lease like every other mutation here: without it a worker whose
-- lease had already expired could mark a session error out from under the new
-- owner that is mid-way through creating the bot.
UPDATE wecom_install_session
SET status                  = 'error',
    error_reason            = sqlc.arg('error_reason'),
    error_message           = sqlc.arg('error_message'),
    scode_encrypted         = NULL,
    qr_code_url_encrypted   = NULL,
    lease_token             = NULL,
    lease_expires_at        = NULL,
    updated_at              = now()
WHERE id = sqlc.arg('id')
  AND lease_token = sqlc.arg('lease_token')
  AND status IN ('creating', 'pending');

-- name: PurgeTerminalWecomInstallSessions :execrows
-- GC for terminated sessions older than the retention window (spec §3.3:
-- terminal sessions preserved for 30 minutes so the frontend can read
-- final status after the dialog closes). Also sweeps out `creating` rows
-- that failed to be claimed within the same window as a defence-in-depth
-- for the worker being fully offline.
DELETE FROM wecom_install_session
WHERE (status IN ('success', 'error')
       AND updated_at < sqlc.arg('terminal_cutoff')::timestamptz)
   OR (status = 'creating'
       AND created_at < sqlc.arg('creating_cutoff')::timestamptz);
