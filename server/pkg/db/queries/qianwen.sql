-- Qianwen Skill durable request ledger.
--
-- The ledger intentionally has no foreign keys. Its installation/request key
-- survives installation revocation and chat-session archive/deletion so an
-- external retry cannot turn into a second run merely because presentation
-- rows were retired. Installation lifecycle cleanup explicitly removes
-- orphaned ledger rows; public status reads require the exact installation to
-- exist and be active.

-- name: InstallQianwenPersonal :one
-- A live private Skill credential is immutable from the install surface so a
-- caller cannot silently invalidate an already configured Tool. An active
-- conflict returns no row rather than replacing qwc_/qws_; provider-side
-- credential replacement remains an account-level acceptance gate. A revoked
-- row may be reactivated after RevokeQianwenInstallation has cleared its pairing
-- authority.
-- Installation shares the member-removal advisory lock, then locks workspace,
-- agent, and active membership in the repository-wide lifecycle order. This
-- fences every no-FK parent sweep: either install commits first and teardown
-- sees it, or teardown commits first and authority yields no row.
-- The conflict predicate is evaluated atomically after any concurrent inserter
-- commits, so at most one caller receives a usable one-time credential.
WITH member_write_guard AS MATERIALIZED (
    SELECT pg_advisory_xact_lock(
        hashtext((sqlc.arg('workspace_id')::uuid)::text),
        hashtext((sqlc.arg('installer_user_id')::uuid)::text)
    ) AS ok
), workspace_guard AS MATERIALIZED (
    SELECT workspace.id
    FROM workspace
    JOIN member_write_guard ON true
    WHERE workspace.id = sqlc.arg('workspace_id')
    FOR KEY SHARE OF workspace
), agent_guard AS MATERIALIZED (
    SELECT agent.id, agent.workspace_id
    FROM agent
    JOIN workspace_guard ON workspace_guard.id = agent.workspace_id
    WHERE agent.id = sqlc.arg('agent_id')
      AND agent.kind = 'user'
      AND agent.archived_at IS NULL
    FOR SHARE OF agent
), authority AS MATERIALIZED (
    SELECT agent_guard.id AS agent_id,
           agent_guard.workspace_id,
           membership.user_id AS installer_user_id
    FROM agent_guard
    JOIN member AS membership
      ON membership.workspace_id = agent_guard.workspace_id
     AND membership.user_id = sqlc.arg('installer_user_id')
    FOR SHARE OF membership
)
INSERT INTO channel_installation (
    workspace_id,
    agent_id,
    channel_type,
    config,
    installer_user_id
) SELECT
    authority.workspace_id,
    authority.agent_id,
    'qianwen',
    sqlc.arg('config'),
    authority.installer_user_id
FROM authority
ON CONFLICT (workspace_id, agent_id, channel_type) DO UPDATE SET
    config = EXCLUDED.config,
    installer_user_id = EXCLUDED.installer_user_id,
    status = 'active',
    installed_at = now(),
    updated_at = now()
WHERE channel_installation.status = 'revoked'
RETURNING *;

-- name: UpsertQianwenPairingCode :one
-- Minting is authorized and persisted in one statement. The shared locks make
-- a concurrent installation revoke or membership removal serialize with this
-- write. The unique installation/user key replaces the previous digest, so a
-- repeated mint invalidates the older plaintext immediately. PostgreSQL's
-- transaction clock owns the ten-minute TTL returned to the caller.
WITH member_write_guard AS MATERIALIZED (
    SELECT pg_advisory_xact_lock(
        hashtext((sqlc.arg('workspace_id')::uuid)::text),
        hashtext((sqlc.arg('multica_user_id')::uuid)::text)
    ) AS ok
), workspace_guard AS MATERIALIZED (
    SELECT workspace.id
    FROM workspace
    JOIN member_write_guard ON true
    WHERE workspace.id = sqlc.arg('workspace_id')
    FOR KEY SHARE OF workspace
), installation_hint AS MATERIALIZED (
    SELECT installation.agent_id
    FROM channel_installation AS installation
    JOIN workspace_guard ON workspace_guard.id = installation.workspace_id
    WHERE installation.id = sqlc.arg('installation_id')
      AND installation.workspace_id = sqlc.arg('workspace_id')
      AND installation.channel_type = 'qianwen'
), agent_guard AS MATERIALIZED (
    SELECT agent.id, agent.workspace_id
    FROM agent
    JOIN installation_hint ON installation_hint.agent_id = agent.id
    WHERE agent.workspace_id = sqlc.arg('workspace_id')
      AND agent.kind = 'user'
      AND agent.archived_at IS NULL
      AND (
            agent.owner_id = sqlc.arg('multica_user_id')
         OR (
                agent.permission_mode = 'public_to'
            AND EXISTS (
                SELECT 1
                FROM agent_invocation_target target
                WHERE target.agent_id = agent.id
                  AND (
                        (target.target_type = 'workspace' AND target.target_id = agent.workspace_id)
                     OR (target.target_type = 'member' AND target.target_id = sqlc.arg('multica_user_id'))
                  )
            )
         )
      )
    FOR SHARE OF agent
), authority AS MATERIALIZED (
    SELECT installation.id, installation.workspace_id, membership.user_id
    FROM agent_guard
    JOIN channel_installation AS installation
      ON installation.agent_id = agent_guard.id
     AND installation.workspace_id = agent_guard.workspace_id
    JOIN member AS membership
      ON membership.workspace_id = installation.workspace_id
     AND membership.user_id = sqlc.arg('multica_user_id')
    WHERE installation.id = sqlc.arg('installation_id')
      AND installation.workspace_id = sqlc.arg('workspace_id')
      AND installation.channel_type = 'qianwen'
      AND installation.status = 'active'
      AND installation.config ->> 'mode' = 'personal_polling'
    FOR SHARE OF installation, membership
)
INSERT INTO qianwen_pairing_code (
    installation_id,
    workspace_id,
    multica_user_id,
    code_digest,
    expires_at
)
SELECT
    authority.id,
    authority.workspace_id,
    authority.user_id,
    sqlc.arg('code_digest'),
    now() + INTERVAL '10 minutes'
FROM authority
ON CONFLICT (installation_id, multica_user_id) DO UPDATE
SET workspace_id = EXCLUDED.workspace_id,
    code_digest = EXCLUDED.code_digest,
    expires_at = now() + INTERVAL '10 minutes',
    created_at = now()
RETURNING qianwen_pairing_code.*;

-- name: RevokeQianwenInstallation :one
-- Revocation is the authority boundary for the private Skill credential. The
-- installation row is updated first (the same parent-before-child lock order
-- used by redeem), then every short-lived pairing capability and established
-- Qianwen identity binding is removed in the same transaction. Durable task
-- request rows intentionally survive revocation so an external retry can never
-- turn an already accepted request id into a second run.
WITH revoked AS MATERIALIZED (
    UPDATE channel_installation
    SET status = 'revoked', updated_at = now()
    WHERE channel_installation.id = sqlc.arg('installation_id')
      AND channel_installation.workspace_id = sqlc.arg('workspace_id')
      AND channel_installation.channel_type = 'qianwen'
    RETURNING channel_installation.id
), cleared_codes AS (
    DELETE FROM qianwen_pairing_code
    WHERE installation_id IN (SELECT id FROM revoked)
), cleared_attempts AS (
    DELETE FROM qianwen_pairing_attempt
    WHERE installation_id IN (SELECT id FROM revoked)
), cleared_nonces AS (
    DELETE FROM qianwen_invocation_nonce
    WHERE installation_id IN (SELECT id FROM revoked)
), cleared_bindings AS (
    DELETE FROM channel_user_binding
    WHERE installation_id IN (SELECT id FROM revoked)
)
SELECT count(*)::bigint FROM revoked;

-- name: RevokeQianwenInstallationsByInstaller :exec
-- Member removal owns the same (workspace,user) advisory lock as pairing.
-- Revoke every personal Qianwen credential installed by the departing member
-- and clear all identities/codes/replay state for those installations before
-- the member row disappears. Otherwise a later re-invite would silently
-- reactivate the old qws_ credential and every binding it used to authorize.
WITH revoked AS MATERIALIZED (
    UPDATE channel_installation
    SET status = 'revoked', updated_at = now()
    WHERE channel_installation.workspace_id = sqlc.arg('workspace_id')
      AND channel_installation.installer_user_id = sqlc.arg('multica_user_id')
      AND channel_installation.channel_type = 'qianwen'
    RETURNING channel_installation.id
), cleared_codes AS (
    DELETE FROM qianwen_pairing_code
    WHERE installation_id IN (SELECT id FROM revoked)
), cleared_attempts AS (
    DELETE FROM qianwen_pairing_attempt
    WHERE installation_id IN (SELECT id FROM revoked)
), cleared_nonces AS (
    DELETE FROM qianwen_invocation_nonce
    WHERE installation_id IN (SELECT id FROM revoked)
), cleared_bindings AS (
    DELETE FROM channel_user_binding
    WHERE installation_id IN (SELECT id FROM revoked)
)
SELECT count(*) FROM revoked;

-- name: DeleteQianwenPairingStateByWorkspaceMember :exec
-- A departing non-installer may still own a pending spoken code or a short-
-- lived successful replay outcome on somebody else's installation. Remove
-- those user-addressable rows in the member-revoke transaction; anonymous
-- failure digests expire within ten minutes and cannot be reversed to a user.
WITH workspace_installations AS MATERIALIZED (
    SELECT channel_installation.id FROM channel_installation
    WHERE channel_installation.workspace_id = sqlc.arg('workspace_id')
      AND channel_installation.channel_type = 'qianwen'
), cleared_codes AS (
    DELETE FROM qianwen_pairing_code
    WHERE qianwen_pairing_code.workspace_id = sqlc.arg('workspace_id')
      AND qianwen_pairing_code.multica_user_id = sqlc.arg('multica_user_id')
), cleared_nonces AS (
    DELETE FROM qianwen_invocation_nonce
    WHERE installation_id IN (SELECT id FROM workspace_installations)
      AND multica_user_id = sqlc.arg('multica_user_id')
)
SELECT count(*) FROM workspace_installations;

-- name: GetLiveQianwenPairingCode :one
-- This non-locking pre-read identifies the target member whose lifecycle
-- advisory lock must be acquired before the transaction touches workspace,
-- agent, installation, membership, or pairing rows. The row is re-read under
-- FOR UPDATE after every authority fence is held; this result is never trusted
-- for consumption by itself.
SELECT * FROM qianwen_pairing_code
WHERE installation_id = sqlc.arg('installation_id')
  AND code_digest = sqlc.arg('code_digest')
  AND expires_at > now();

-- name: LockQianwenInstallationForPairing :one
-- Rechecks every mutable installation authority field under an exclusive row
-- lock. Serializing redemptions per installation makes the rolling DB budget,
-- nonce outcome ledger, and one-time code consume a single state machine while
-- forcing revoke/reconnect to choose a side of the transaction.
SELECT installation.*
FROM channel_installation AS installation
WHERE installation.id = sqlc.arg('installation_id')
  AND installation.workspace_id = sqlc.arg('workspace_id')
  AND installation.agent_id = sqlc.arg('agent_id')
  AND installation.installer_user_id = sqlc.arg('installer_user_id')
  AND installation.channel_type = 'qianwen'
  AND installation.status = 'active'
  AND installation.config ->> 'mode' = 'personal_polling'
  AND installation.config ->> 'app_id' = sqlc.arg('connection_id')::text
  AND installation.config ->> 'access_token_hash' = sqlc.arg('access_token_hash')::text
FOR UPDATE OF installation;

-- name: DeleteExpiredQianwenInvocationNonces :exec
DELETE FROM qianwen_invocation_nonce
WHERE installation_id = sqlc.arg('installation_id')
  AND expires_at <= now();

-- name: GetLiveQianwenInvocationByNonceForUpdate :one
SELECT * FROM qianwen_invocation_nonce
WHERE installation_id = sqlc.arg('installation_id')
  AND nonce_digest = sqlc.arg('nonce_digest')
  AND expires_at > now()
FOR UPDATE;

-- name: FindCompletedQianwenInvocationByRequestDigest :one
-- A provider retry may generate a fresh timestamp/nonce after losing the first
-- HTTP response. The semantic digest deliberately excludes transport replay
-- fields so the prior stable outcome can be returned without consuming a
-- second code or recording a second failure.
SELECT * FROM qianwen_invocation_nonce
WHERE installation_id = sqlc.arg('installation_id')
  AND request_digest = sqlc.arg('request_digest')
  AND outcome IS NOT NULL
  AND expires_at > now()
ORDER BY created_at DESC
LIMIT 1;

-- name: InsertQianwenInvocationNonce :one
INSERT INTO qianwen_invocation_nonce (
    installation_id,
    nonce_digest,
    request_digest,
    expires_at
) VALUES (
    sqlc.arg('installation_id'),
    sqlc.arg('nonce_digest'),
    sqlc.arg('request_digest'),
    now() + INTERVAL '5 minutes'
)
RETURNING *;

-- name: CompleteQianwenInvocationNonce :one
UPDATE qianwen_invocation_nonce
SET outcome = sqlc.arg('outcome'),
    multica_user_id = sqlc.narg('multica_user_id')::uuid
WHERE installation_id = sqlc.arg('installation_id')
  AND nonce_digest = sqlc.arg('nonce_digest')
  AND request_digest = sqlc.arg('request_digest')
  AND outcome IS NULL
  AND expires_at > now()
RETURNING *;

-- name: DeleteExpiredQianwenPairingAttempts :exec
DELETE FROM qianwen_pairing_attempt
WHERE installation_id = sqlc.arg('installation_id')
  AND attempted_at <= now() - INTERVAL '10 minutes';

-- name: GetQianwenPairingAttemptCounts :one
SELECT
    count(*)::bigint AS installation_failures,
    count(*) FILTER (
        WHERE identity_digest = sqlc.arg('identity_digest')::bytea
    )::bigint AS identity_failures
FROM qianwen_pairing_attempt
WHERE installation_id = sqlc.arg('installation_id')
  AND attempted_at > now() - INTERVAL '10 minutes';

-- name: InsertQianwenPairingFailure :one
INSERT INTO qianwen_pairing_attempt (
    installation_id,
    identity_digest
) VALUES (
    sqlc.arg('installation_id'),
    sqlc.arg('identity_digest')
)
RETURNING *;

-- name: GetLiveQianwenPairingCodeForUpdate :one
SELECT * FROM qianwen_pairing_code
WHERE installation_id = sqlc.arg('installation_id')
  AND code_digest = sqlc.arg('code_digest')
  AND expires_at > now()
FOR UPDATE;

-- name: CanQianwenPairingUserInvokeAgent :one
-- Mirrors the member branch of Handler.canInvokeAgent inside the redemption
-- transaction: private agents are owner-only; public_to agents require either
-- a workspace-wide or exact-member target. Membership and archive state are
-- rechecked after the code was minted.
SELECT EXISTS (
    SELECT 1
    FROM channel_installation installation
    JOIN agent
      ON agent.id = installation.agent_id
     AND agent.workspace_id = installation.workspace_id
     AND agent.archived_at IS NULL
    JOIN member membership
      ON membership.workspace_id = installation.workspace_id
     AND membership.user_id = sqlc.arg('multica_user_id')
    WHERE installation.id = sqlc.arg('installation_id')
      AND installation.channel_type = 'qianwen'
      AND installation.status = 'active'
      AND (
            agent.owner_id = sqlc.arg('multica_user_id')
         OR (
                agent.permission_mode = 'public_to'
            AND EXISTS (
                SELECT 1
                FROM agent_invocation_target target
                WHERE target.agent_id = agent.id
                  AND (
                        (target.target_type = 'workspace' AND target.target_id = installation.workspace_id)
                     OR (target.target_type = 'member' AND target.target_id = sqlc.arg('multica_user_id'))
                  )
            )
         )
      )
);

-- name: DeleteQianwenPairingCode :execrows
DELETE FROM qianwen_pairing_code
WHERE installation_id = sqlc.arg('installation_id')
  AND multica_user_id = sqlc.arg('multica_user_id')
  AND code_digest = sqlc.arg('code_digest');

-- name: ListQianwenBoundInstallationIDsForUser :many
-- Management-facing, caller-relative binding state. Opaque Qianwen identity
-- values never leave the server; a row only counts when it has the exact
-- skill-scoped shape used by inbound authorization.
SELECT DISTINCT binding.installation_id
FROM channel_user_binding AS binding
JOIN channel_installation AS installation
  ON installation.id = binding.installation_id
 AND installation.workspace_id = binding.workspace_id
 AND installation.channel_type = binding.channel_type
WHERE binding.workspace_id = sqlc.arg('workspace_id')
  AND binding.multica_user_id = sqlc.arg('multica_user_id')
  AND binding.channel_type = 'qianwen'
  AND binding.channel_user_id <> ''
  AND COALESCE(binding.config ->> 'open_uuid', '') <> ''
  AND binding.config ->> 'identity_scope' = 'skill';

-- name: LockQianwenInstallationForUnbind :one
-- Called after the member advisory, workspace, and agent locks. The exclusive
-- installation lock serializes with redeem; it deliberately accepts revoked
-- rows so repeated self-unbind remains idempotent while the installation
-- record still exists.
SELECT installation.*
FROM channel_installation AS installation
WHERE installation.id = sqlc.arg('installation_id')
  AND installation.workspace_id = sqlc.arg('workspace_id')
  AND installation.agent_id = sqlc.arg('agent_id')
  AND installation.channel_type = 'qianwen'
FOR UPDATE OF installation;

-- name: DeleteQianwenCurrentUserState :exec
-- This must run as a separate statement after LockSubscriberWrites has
-- returned. Under READ COMMITTED the new statement receives a fresh snapshot,
-- so a redeem that committed while unbind waited cannot escape cleanup.
WITH cleared_codes AS (
    DELETE FROM qianwen_pairing_code
    WHERE qianwen_pairing_code.installation_id = sqlc.arg('installation_id')
      AND qianwen_pairing_code.multica_user_id = sqlc.arg('multica_user_id')
), cleared_nonces AS (
    DELETE FROM qianwen_invocation_nonce
    WHERE qianwen_invocation_nonce.installation_id = sqlc.arg('installation_id')
      AND qianwen_invocation_nonce.multica_user_id = sqlc.arg('multica_user_id')
), cleared_bindings AS (
    DELETE FROM channel_user_binding
    WHERE channel_user_binding.installation_id = sqlc.arg('installation_id')
      AND channel_user_binding.workspace_id = sqlc.arg('workspace_id')
      AND channel_user_binding.multica_user_id = sqlc.arg('multica_user_id')
      AND channel_user_binding.channel_type = 'qianwen'
    RETURNING 1
)
SELECT count(*) FROM cleared_bindings;

-- name: GetActiveQianwenInvocationUser :one
-- Resolve the signed, opaque Qianwen identity to the Multica member that will
-- own the request. qws_ authenticates only the installation; it never supplies
-- the task actor. This read is repeated under locks by ClaimQianwenRequest and
-- LockQianwenSubmitAuthority before either ledger or task state is published.
SELECT binding.multica_user_id
FROM channel_installation AS installation
JOIN channel_user_binding AS binding
  ON binding.installation_id = installation.id
 AND binding.workspace_id = installation.workspace_id
 AND binding.channel_type = 'qianwen'
JOIN member AS membership
  ON membership.workspace_id = installation.workspace_id
 AND membership.user_id = binding.multica_user_id
JOIN agent
  ON agent.id = installation.agent_id
 AND agent.workspace_id = installation.workspace_id
 AND agent.kind = 'user'
 AND agent.archived_at IS NULL
WHERE installation.id = sqlc.arg('installation_id')
  AND installation.channel_type = 'qianwen'
  AND installation.status = 'active'
  AND installation.config ->> 'mode' = 'personal_polling'
  AND installation.config ->> 'app_id' = sqlc.arg('connection_id')::text
  AND installation.config ->> 'access_token_hash' = sqlc.arg('access_token_hash')::text
  AND binding.channel_user_id = sqlc.arg('open_user_id')::text
  AND binding.config ->> 'open_uuid' = sqlc.arg('open_uuid')::text
  AND binding.config ->> 'identity_scope' = 'skill'
  AND (
        agent.owner_id = binding.multica_user_id
     OR (
            agent.permission_mode = 'public_to'
        AND EXISTS (
            SELECT 1
            FROM agent_invocation_target AS target
            WHERE target.agent_id = agent.id
              AND (
                    (target.target_type = 'workspace' AND target.target_id = installation.workspace_id)
                 OR (target.target_type = 'member' AND target.target_id = binding.multica_user_id)
              )
        )
     )
  );

-- name: ClaimQianwenRequest :one
-- Acquire the first owner, or reclaim an unfinished request after its DB-clock
-- lease expires. The owner fence locks the workspace and agent before the
-- ledger write, so workspace/runtime teardown either sees and removes this row
-- or finishes first and makes the insert return no row. The authority CTE then
-- locks and revalidates the current installation credential and membership,
-- preventing an already-rotated/revoked token from publishing a new claim.
-- A replay with a different normalized-query digest, a request that already
-- owns a task, or a request with an active lease deliberately returns no row
-- (pgx.ErrNoRows). Every successful reclaim replaces the token, fencing every
-- write attempted by the previous owner.
WITH owner_guard AS MATERIALIZED (
    SELECT lock_task_owner_rows(
        sqlc.arg('agent_id')::uuid,
        NULL::uuid,
        NULL::uuid
    ) AS ok
), agent_guard AS MATERIALIZED (
    SELECT agent.id, agent.workspace_id, agent.owner_id, agent.permission_mode
    FROM owner_guard
    JOIN agent ON owner_guard.ok
    WHERE agent.id = sqlc.arg('agent_id')
      AND agent.workspace_id = sqlc.arg('workspace_id')
      AND agent.kind = 'user'
      AND agent.archived_at IS NULL
    FOR SHARE OF agent
), target_guard AS MATERIALIZED (
    SELECT target.id
    FROM agent_invocation_target AS target
    JOIN agent_guard ON target.agent_id = agent_guard.id
    WHERE agent_guard.permission_mode = 'public_to'
      AND (
            (target.target_type = 'workspace' AND target.target_id = agent_guard.workspace_id)
         OR (target.target_type = 'member' AND target.target_id = sqlc.arg('multica_user_id'))
      )
    FOR SHARE OF target
), authority AS MATERIALIZED (
    SELECT installation.id, binding.multica_user_id
    FROM agent_guard
    JOIN channel_installation AS installation
      ON installation.agent_id = agent_guard.id
     AND installation.workspace_id = agent_guard.workspace_id
    JOIN channel_user_binding AS binding
      ON binding.installation_id = installation.id
     AND binding.workspace_id = installation.workspace_id
     AND binding.channel_type = 'qianwen'
     AND binding.multica_user_id = sqlc.arg('multica_user_id')
    JOIN member AS membership
      ON membership.workspace_id = installation.workspace_id
     AND membership.user_id = binding.multica_user_id
    WHERE installation.id = sqlc.arg('installation_id')
      AND installation.workspace_id = sqlc.arg('workspace_id')
      AND installation.agent_id = sqlc.arg('agent_id')
      AND installation.channel_type = 'qianwen'
      AND installation.status = 'active'
      AND installation.config ->> 'mode' = 'personal_polling'
      AND installation.config ->> 'app_id' = sqlc.arg('connection_id')::text
      AND installation.config ->> 'access_token_hash' = sqlc.arg('access_token_hash')::text
      AND binding.channel_user_id = sqlc.arg('open_user_id')::text
      AND binding.config ->> 'open_uuid' = sqlc.arg('open_uuid')::text
      AND binding.config ->> 'identity_scope' = 'skill'
      AND (
            agent_guard.owner_id = binding.multica_user_id
         OR EXISTS (SELECT 1 FROM target_guard)
      )
    FOR SHARE OF installation, binding, membership
)
INSERT INTO qianwen_skill_request (
    installation_id,
    request_id,
    multica_user_id,
    query_sha256,
    claim_token,
    claim_expires_at
)
SELECT
    authority.id,
    sqlc.arg('request_id'),
    authority.multica_user_id,
    sqlc.arg('query_sha256'),
    gen_random_uuid(),
    now() + INTERVAL '5 seconds'
FROM authority
ON CONFLICT (installation_id, request_id) DO UPDATE
SET claim_token = gen_random_uuid(),
    claim_expires_at = now() + INTERVAL '5 seconds',
    updated_at = now()
WHERE qianwen_skill_request.query_sha256 = EXCLUDED.query_sha256
  AND qianwen_skill_request.multica_user_id = EXCLUDED.multica_user_id
  AND qianwen_skill_request.task_id IS NULL
  AND (
      qianwen_skill_request.claim_token IS NULL
      OR qianwen_skill_request.claim_expires_at IS NULL
      OR qianwen_skill_request.claim_expires_at <= now()
  )
RETURNING qianwen_skill_request.*;

-- name: LockQianwenSubmitAuthority :one
-- CreateChatTask has already acquired lock_task_owner_rows for this agent in
-- the surrounding direct-send transaction. Re-lock the exact opaque identity,
-- bound member, current invocation grant, and mutable installation credential
-- before publishing the ledger task pointer.
WITH agent_guard AS MATERIALIZED (
    SELECT agent.id, agent.workspace_id, agent.owner_id, agent.permission_mode
    FROM agent
    WHERE agent.id = sqlc.arg('agent_id')
      AND agent.workspace_id = sqlc.arg('workspace_id')
      AND agent.kind = 'user'
      AND agent.archived_at IS NULL
    FOR SHARE OF agent
), target_guard AS MATERIALIZED (
    SELECT target.id
    FROM agent_invocation_target AS target
    JOIN agent_guard ON target.agent_id = agent_guard.id
    WHERE agent_guard.permission_mode = 'public_to'
      AND (
            (target.target_type = 'workspace' AND target.target_id = agent_guard.workspace_id)
         OR (target.target_type = 'member' AND target.target_id = sqlc.arg('multica_user_id'))
      )
    FOR SHARE OF target
)
SELECT installation.id
FROM agent_guard
JOIN channel_installation AS installation
  ON installation.agent_id = agent_guard.id
 AND installation.workspace_id = agent_guard.workspace_id
JOIN channel_user_binding AS binding
  ON binding.installation_id = installation.id
 AND binding.workspace_id = installation.workspace_id
 AND binding.channel_type = 'qianwen'
 AND binding.multica_user_id = sqlc.arg('multica_user_id')
JOIN member AS membership
  ON membership.workspace_id = installation.workspace_id
 AND membership.user_id = binding.multica_user_id
WHERE installation.id = sqlc.arg('installation_id')
  AND installation.workspace_id = sqlc.arg('workspace_id')
  AND installation.agent_id = sqlc.arg('agent_id')
  AND installation.channel_type = 'qianwen'
  AND installation.status = 'active'
  AND installation.config ->> 'mode' = 'personal_polling'
  AND installation.config ->> 'app_id' = sqlc.arg('connection_id')::text
  AND installation.config ->> 'access_token_hash' = sqlc.arg('access_token_hash')::text
  AND binding.channel_user_id = sqlc.arg('open_user_id')::text
  AND binding.config ->> 'open_uuid' = sqlc.arg('open_uuid')::text
  AND binding.config ->> 'identity_scope' = 'skill'
  AND (
        agent_guard.owner_id = binding.multica_user_id
     OR EXISTS (SELECT 1 FROM target_guard)
  )
FOR SHARE OF installation, binding, membership;

-- name: GetQianwenRequest :one
-- Internal idempotency/recovery read. Authentication is performed before this
-- lookup; unlike the public status query it must also see a retained ledger row
-- after an installation is revoked so the request key is never silently reused.
SELECT *
FROM qianwen_skill_request
WHERE installation_id = sqlc.arg('installation_id')
  AND request_id = sqlc.arg('request_id');

-- name: SetQianwenRequestSession :execrows
-- Persist the isolated chat session while this owner still holds the fencing
-- token. A reclaimed owner receives a new token, so a late predecessor updates
-- zero rows. Replacement is intentional: if the recorded session was archived
-- or deleted before any task was sealed, recovery creates a fresh isolated
-- session and advances the ledger to it under the current claim.
UPDATE qianwen_skill_request
SET chat_session_id = sqlc.arg('chat_session_id'),
    updated_at = now()
WHERE installation_id = sqlc.arg('installation_id')
  AND request_id = sqlc.arg('request_id')
  AND multica_user_id = sqlc.arg('multica_user_id')
  AND claim_token = sqlc.arg('claim_token')
  AND task_id IS NULL;

-- name: CompleteQianwenRequest :execrows
-- Publish the durable task handle and release the lease in one fenced update.
-- The task's raw result/error is never copied into this ledger or selected by
-- the public status query.
UPDATE qianwen_skill_request
SET task_id = sqlc.arg('task_id'),
    claim_token = NULL,
    claim_expires_at = NULL,
    updated_at = now()
WHERE installation_id = sqlc.arg('installation_id')
  AND request_id = sqlc.arg('request_id')
  AND multica_user_id = sqlc.arg('multica_user_id')
  AND claim_token = sqlc.arg('claim_token')
  AND task_id IS NULL;

-- name: ReleaseQianwenRequestClaim :execrows
-- An owner that failed before publishing a task releases only its own token.
-- The row remains durable, including when a session was already recorded, so
-- the next claimant can recover that session instead of duplicating it.
UPDATE qianwen_skill_request
SET claim_token = NULL,
    claim_expires_at = NULL,
    updated_at = now()
WHERE installation_id = sqlc.arg('installation_id')
  AND request_id = sqlc.arg('request_id')
  AND multica_user_id = sqlc.arg('multica_user_id')
  AND claim_token = sqlc.arg('claim_token')
  AND task_id IS NULL;

-- name: GetQianwenRequestStatus :one
-- Public polling read. Authorization is scoped from the caller's active
-- installation through its exact bound identity, workspace, agent, isolated chat session,
-- task, retry descendants, and final assistant message. The ledger and retry
-- lineage deliberately have no foreign keys, so every hop must re-assert that
-- ownership tuple instead of treating a stored UUID as authorization.
-- agent_task_queue.result/error are intentionally not selected.
WITH RECURSIVE request AS (
    SELECT
        ledger.*,
        installation.workspace_id,
        installation.agent_id
    FROM qianwen_skill_request AS ledger
    JOIN channel_installation AS installation
      ON installation.id = ledger.installation_id
     AND installation.channel_type = 'qianwen'
     AND installation.status = 'active'
     AND installation.config ->> 'mode' = 'personal_polling'
     AND installation.config ->> 'app_id' = sqlc.arg('connection_id')::text
     AND installation.config ->> 'access_token_hash' = sqlc.arg('access_token_hash')::text
    JOIN member AS membership
      ON membership.workspace_id = installation.workspace_id
     AND membership.user_id = ledger.multica_user_id
    JOIN channel_user_binding AS binding
      ON binding.installation_id = installation.id
     AND binding.workspace_id = installation.workspace_id
     AND binding.channel_type = 'qianwen'
     AND binding.multica_user_id = ledger.multica_user_id
     AND binding.channel_user_id = sqlc.arg('open_user_id')::text
     AND binding.config ->> 'open_uuid' = sqlc.arg('open_uuid')::text
     AND binding.config ->> 'identity_scope' = 'skill'
    JOIN agent AS installed_agent
      ON installed_agent.id = installation.agent_id
     AND installed_agent.workspace_id = installation.workspace_id
     AND installed_agent.kind = 'user'
     AND installed_agent.archived_at IS NULL
    WHERE ledger.installation_id = sqlc.arg('installation_id')
      AND ledger.request_id = sqlc.arg('request_id')
      AND ledger.multica_user_id = sqlc.arg('multica_user_id')
      AND (
            installed_agent.owner_id = ledger.multica_user_id
         OR (
                installed_agent.permission_mode = 'public_to'
            AND EXISTS (
                SELECT 1
                FROM agent_invocation_target AS target
                WHERE target.agent_id = installed_agent.id
                  AND (
                        (target.target_type = 'workspace' AND target.target_id = installation.workspace_id)
                     OR (target.target_type = 'member' AND target.target_id = ledger.multica_user_id)
                  )
            )
         )
      )
), retry_chain AS (
    SELECT
        task.id,
        task.status,
        task.attempt,
        task.created_at,
        task.completed_at,
        (session.id IS NOT NULL)::boolean AS session_alive,
        ARRAY[task.id]::uuid[] AS path
    FROM agent_task_queue AS task
    JOIN request ON request.task_id = task.id
    LEFT JOIN chat_session AS session ON session.id = request.chat_session_id
    WHERE task.regenerate_quick_actions_for IS NULL
      AND task.agent_id = request.agent_id
      AND task.initiator_user_id = request.multica_user_id
      AND task.originator_user_id = request.multica_user_id
      AND task.accountable_user_id = request.multica_user_id
      AND (
          (
              session.id IS NOT NULL
              AND session.id = task.chat_session_id
              AND session.workspace_id = request.workspace_id
              AND session.agent_id = request.agent_id
              AND session.creator_id = request.multica_user_id
          )
          OR (
              session.id IS NULL
              AND task.chat_session_id IS NULL
          )
      )

    UNION ALL

    SELECT
        child.id,
        child.status,
        child.attempt,
        child.created_at,
        child.completed_at,
        parent.session_alive,
        parent.path || ARRAY[child.id]::uuid[]
    FROM agent_task_queue AS child
    JOIN retry_chain AS parent ON child.retry_of_task_id = parent.id
    JOIN request
      ON child.agent_id = request.agent_id
     AND child.originator_user_id = request.multica_user_id
     AND child.accountable_user_id = request.multica_user_id
     AND (
         (parent.session_alive AND child.chat_session_id = request.chat_session_id)
         OR (NOT parent.session_alive AND child.chat_session_id IS NULL)
     )
     AND (
         child.initiator_user_id IS NULL
         OR child.initiator_user_id = request.multica_user_id
     )
    WHERE child.regenerate_quick_actions_for IS NULL
      AND NOT (child.id = ANY(parent.path))
), task_head AS (
    SELECT id, status, created_at, completed_at
    FROM retry_chain
    ORDER BY attempt DESC, created_at DESC, id DESC
    LIMIT 1
)
SELECT
    request.chat_session_id,
    request.created_at AS request_created_at,
    CASE
        WHEN request.chat_session_id IS NOT NULL THEN TRUE
        ELSE FALSE
    END::boolean AS ingested,
    CASE
        WHEN request.task_id IS NULL
         AND request.claim_token IS NOT NULL
         AND request.claim_expires_at > now() THEN TRUE
        ELSE FALSE
    END::boolean AS claim_active,
    task_head.id AS task_id,
    COALESCE(task_head.status, '')::text AS task_status,
    task_head.created_at AS task_created_at,
    task_head.completed_at AS task_completed_at,
    COALESCE(reply.content, '')::text AS output,
    COALESCE(reply.message_kind, '')::text AS output_kind
FROM request
LEFT JOIN task_head ON TRUE
LEFT JOIN LATERAL (
    SELECT message.content, message.message_kind
    FROM chat_message AS message
    WHERE message.task_id = task_head.id
      AND message.chat_session_id = request.chat_session_id
      AND message.role = 'assistant'
    ORDER BY message.created_at DESC, message.id DESC
    LIMIT 1
) AS reply ON TRUE;
