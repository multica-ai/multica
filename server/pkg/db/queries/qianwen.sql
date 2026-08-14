-- Qianwen Skill durable request ledger.
--
-- The ledger intentionally has no foreign keys. Its installation/request key
-- survives installation revocation and chat-session archive/deletion so an
-- external retry cannot turn into a second run merely because presentation
-- rows were retired. Installation lifecycle cleanup explicitly removes
-- orphaned ledger rows; public status reads require the exact installation to
-- exist and be active.

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
), authority AS MATERIALIZED (
    SELECT installation.id
    FROM owner_guard
    JOIN channel_installation AS installation ON owner_guard.ok
    JOIN member AS membership
      ON membership.workspace_id = installation.workspace_id
     AND membership.user_id = installation.installer_user_id
    WHERE installation.id = sqlc.arg('installation_id')
      AND installation.workspace_id = sqlc.arg('workspace_id')
      AND installation.agent_id = sqlc.arg('agent_id')
      AND installation.installer_user_id = sqlc.arg('installer_user_id')
      AND installation.channel_type = 'qianwen'
      AND installation.status = 'active'
      AND installation.config ->> 'mode' = 'personal_polling'
      AND installation.config ->> 'app_id' = sqlc.arg('connection_id')::text
      AND installation.config ->> 'access_token_hash' = sqlc.arg('access_token_hash')::text
    FOR SHARE OF installation, membership
)
INSERT INTO qianwen_skill_request (
    installation_id,
    request_id,
    query_sha256,
    claim_token,
    claim_expires_at
)
SELECT
    authority.id,
    sqlc.arg('request_id'),
    sqlc.arg('query_sha256'),
    gen_random_uuid(),
    now() + INTERVAL '5 seconds'
FROM authority
ON CONFLICT (installation_id, request_id) DO UPDATE
SET claim_token = gen_random_uuid(),
    claim_expires_at = now() + INTERVAL '5 seconds',
    updated_at = now()
WHERE qianwen_skill_request.query_sha256 = EXCLUDED.query_sha256
  AND qianwen_skill_request.task_id IS NULL
  AND (
      qianwen_skill_request.claim_token IS NULL
      OR qianwen_skill_request.claim_expires_at IS NULL
      OR qianwen_skill_request.claim_expires_at <= now()
  )
RETURNING qianwen_skill_request.*;

-- name: LockQianwenSubmitAuthority :one
-- CreateChatTask has already acquired lock_task_owner_rows for this agent in
-- the surrounding direct-send transaction. Re-lock the mutable installation
-- credential and membership before publishing the ledger task pointer. FOR
-- SHARE lets independent glasses submits proceed concurrently while forcing a
-- revoke, credential rotation, or member removal to serialize at commit.
SELECT installation.id
FROM channel_installation AS installation
JOIN member AS membership
  ON membership.workspace_id = installation.workspace_id
 AND membership.user_id = installation.installer_user_id
WHERE installation.id = sqlc.arg('installation_id')
  AND installation.workspace_id = sqlc.arg('workspace_id')
  AND installation.agent_id = sqlc.arg('agent_id')
  AND installation.installer_user_id = sqlc.arg('installer_user_id')
  AND installation.channel_type = 'qianwen'
  AND installation.status = 'active'
  AND installation.config ->> 'mode' = 'personal_polling'
  AND installation.config ->> 'app_id' = sqlc.arg('connection_id')::text
  AND installation.config ->> 'access_token_hash' = sqlc.arg('access_token_hash')::text
FOR SHARE OF installation, membership;

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
  AND claim_token = sqlc.arg('claim_token')
  AND task_id IS NULL;

-- name: GetQianwenRequestStatus :one
-- Public polling read. Authorization is scoped from the caller's active
-- installation through its workspace, agent, installer, isolated chat session,
-- task, retry descendants, and final assistant message. The ledger and retry
-- lineage deliberately have no foreign keys, so every hop must re-assert that
-- ownership tuple instead of treating a stored UUID as authorization.
-- agent_task_queue.result/error are intentionally not selected.
WITH RECURSIVE request AS (
    SELECT
        ledger.*,
        installation.workspace_id,
        installation.agent_id,
        installation.installer_user_id
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
     AND membership.user_id = installation.installer_user_id
    JOIN agent AS installed_agent
      ON installed_agent.id = installation.agent_id
     AND installed_agent.workspace_id = installation.workspace_id
    WHERE ledger.installation_id = sqlc.arg('installation_id')
      AND ledger.request_id = sqlc.arg('request_id')
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
      AND task.initiator_user_id = request.installer_user_id
      AND task.originator_user_id = request.installer_user_id
      AND (
          (
              session.id IS NOT NULL
              AND session.id = task.chat_session_id
              AND session.workspace_id = request.workspace_id
              AND session.agent_id = request.agent_id
              AND session.creator_id = request.installer_user_id
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
     AND child.originator_user_id = request.installer_user_id
     AND (
         (parent.session_alive AND child.chat_session_id = request.chat_session_id)
         OR (NOT parent.session_alive AND child.chat_session_id IS NULL)
     )
     AND (
         child.initiator_user_id IS NULL
         OR child.initiator_user_id = request.installer_user_id
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
