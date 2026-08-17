-- DingTalk-specific installation identity operations. The underlying channel_*
-- tables are shared, but these replacement semantics belong to DingTalk's BYO
-- AppKey model and deliberately stay out of the shared channel query surface.

-- name: DiscoverDingTalkGroupRoute :one
-- Persist a group only after the shared inbound router has accepted the @bot
-- message and validated the sender's identity/workspace membership. The INSERT
-- default is the installation's agent; a later admin reassignment survives
-- subsequent inbound events because the conflict update refreshes only the
-- human-readable title. The returned active bit revalidates the effective route
-- target at runtime while preserving the route across archive/restore. The
-- workspace/installation locks serialize discovery with teardown and revoke:
-- revoke-first rechecks status after the wait and returns no row; discovery-
-- first completes before revoke, matching the existing in-flight semantics.
WITH eligible_workspace_ids AS MATERIALIZED (
    SELECT w.id
    FROM workspace w
    JOIN dingtalk_workspace_grant g ON g.workspace_id = w.id
    JOIN member m ON m.workspace_id = w.id
                 AND m.user_id = sqlc.arg(multica_user_id)
    WHERE g.connector_id = sqlc.arg(installation_id)
      AND g.status = 'active'
), workspace_guards AS MATERIALIZED (
    SELECT w.id
    FROM workspace w
    JOIN eligible_workspace_ids candidate ON candidate.id = w.id
    ORDER BY w.id
    FOR KEY SHARE OF w
), connector AS MATERIALIZED (
    SELECT c.id
    FROM dingtalk_connector c
    WHERE c.id = sqlc.arg(installation_id)
      AND c.status = 'active'
      AND EXISTS (SELECT 1 FROM workspace_guards)
    FOR SHARE OF c
), eligible_grants AS MATERIALIZED (
    SELECT g.workspace_id, g.default_agent_id, g.installer_user_id
    FROM dingtalk_workspace_grant g
    JOIN workspace_guards w ON w.id = g.workspace_id
    JOIN connector c ON c.id = g.connector_id
    JOIN member m ON m.workspace_id = g.workspace_id
                 AND m.user_id = sqlc.arg(multica_user_id)
    WHERE g.connector_id = sqlc.arg(installation_id)
      AND g.status = 'active'
    FOR SHARE OF g, m
), existing_target AS MATERIALIZED (
    SELECT r.workspace_id, r.agent_id, g.installer_user_id
    FROM dingtalk_group_route r
    JOIN eligible_grants g ON g.workspace_id = r.workspace_id
    WHERE r.installation_id = sqlc.arg(installation_id)
      AND r.conversation_id = sqlc.arg(conversation_id)::text
      AND EXISTS (SELECT 1 FROM connector)
), candidate AS MATERIALIZED (
    SELECT workspace_id, agent_id, installer_user_id
    FROM existing_target
    UNION ALL
    SELECT workspace_id, default_agent_id, installer_user_id
    FROM eligible_grants
    WHERE NOT EXISTS (SELECT 1 FROM existing_target)
      AND (SELECT count(*) FROM eligible_grants) = 1
      AND EXISTS (SELECT 1 FROM connector)
), group_route AS (
    INSERT INTO dingtalk_group_route (
        workspace_id, installation_id, conversation_id,
        conversation_title, agent_id
    )
    SELECT
        candidate.workspace_id, sqlc.arg(installation_id),
        sqlc.arg(conversation_id)::text,
        sqlc.arg(conversation_title)::text, candidate.agent_id
    FROM candidate
    ON CONFLICT (installation_id, conversation_id) DO UPDATE SET
        conversation_title = CASE
            WHEN EXCLUDED.conversation_title = '' THEN dingtalk_group_route.conversation_title
            ELSE EXCLUDED.conversation_title
        END,
        updated_at = CASE
            WHEN EXCLUDED.conversation_title <> ''
             AND EXCLUDED.conversation_title IS DISTINCT FROM dingtalk_group_route.conversation_title
                THEN now()
            ELSE dingtalk_group_route.updated_at
        END
    RETURNING agent_id, workspace_id, revision
)
SELECT r.agent_id,
       r.workspace_id,
       r.revision,
       g.installer_user_id,
       EXISTS (
           SELECT 1 FROM agent a
           WHERE a.id = r.agent_id
             AND a.workspace_id = r.workspace_id
             AND a.kind = 'user'
             AND a.archived_at IS NULL
       ) AS agent_active
FROM group_route r
JOIN eligible_grants g ON g.workspace_id = r.workspace_id;

-- name: ListDingTalkGroupRoutesByWorkspace :many
SELECT r.*
FROM dingtalk_group_route r
JOIN dingtalk_connector c ON c.id = r.installation_id
JOIN dingtalk_workspace_grant g
  ON g.connector_id = c.id AND g.workspace_id = r.workspace_id
WHERE r.workspace_id = sqlc.arg(workspace_id)
  AND c.status = 'active'
  AND g.status = 'active'
ORDER BY r.discovered_at ASC;

-- name: GetDingTalkGroupRouteInWorkspace :one
-- Handler diagnosis deliberately uses the same active-installation boundary as
-- reassignment. Retained routes stay available for reconnect internally, but a
-- revoked installation's route is not a PATCH-visible resource.
SELECT r.*
FROM dingtalk_group_route r
JOIN dingtalk_connector c ON c.id = r.installation_id
JOIN dingtalk_workspace_grant g
  ON g.connector_id = c.id AND g.workspace_id = r.workspace_id
WHERE r.id = sqlc.arg(id)
  AND r.workspace_id = sqlc.arg(workspace_id)
  AND c.status = 'active'
  AND g.status = 'active';

-- name: ReassignDingTalkGroupRoute :one
-- Reassigning a group must also sever its existing chat-session binding. The
-- next message creates a fresh session for the new agent, so transcripts and
-- pending outbound updates cannot cross agent boundaries. The old chat session
-- remains as history; only its DingTalk route and outbound card state are
-- removed. The target agent existence/workspace/archive check is repeated here
-- so a concurrent archive or delete cannot create a dangling route. The active
-- workspace, active installation, and route locks preserve the teardown order;
-- the installation is share-locked before the route is update-locked. Revoke
-- takes an exclusive lock on that same installation row, defining the race:
-- PATCH-first may finish before revoke, while revoke-first makes PATCH wait,
-- re-check status, and return no row without touching the route or binding.
WITH workspace_guard AS MATERIALIZED (
    SELECT w.id
    FROM workspace w
    WHERE w.id = sqlc.arg(workspace_id)
    FOR KEY SHARE
), connector_guard AS MATERIALIZED (
    SELECT c.id
    FROM dingtalk_connector c
    JOIN dingtalk_group_route r ON r.installation_id = c.id
    WHERE r.id = sqlc.arg(id)
      AND r.workspace_id = sqlc.arg(workspace_id)
      AND c.status = 'active'
      AND EXISTS (SELECT 1 FROM workspace_guard)
    FOR SHARE OF c
), target_agent AS MATERIALIZED (
    SELECT a.id
    FROM agent a
    JOIN workspace_guard w ON w.id = a.workspace_id
    JOIN connector_guard c ON true
    WHERE a.id = sqlc.arg(agent_id)
      AND a.workspace_id = sqlc.arg(workspace_id)
      AND a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE
), grant_guard AS MATERIALIZED (
    SELECT g.connector_id
    FROM dingtalk_workspace_grant g
    JOIN connector_guard c ON c.id = g.connector_id
    WHERE g.workspace_id = sqlc.arg(workspace_id)
      AND g.status = 'active'
      AND EXISTS (SELECT 1 FROM target_agent)
    FOR SHARE OF g
), target AS (
    SELECT r.*, r.agent_id AS previous_agent_id
    FROM dingtalk_group_route r
    JOIN grant_guard g ON g.connector_id = r.installation_id
    WHERE r.id = sqlc.arg(id)
      AND r.workspace_id = sqlc.arg(workspace_id)
    FOR UPDATE OF r
), updated AS (
    UPDATE dingtalk_group_route r
    SET agent_id = sqlc.arg(agent_id),
        revision = r.revision + 1,
        updated_at = now()
    FROM target t
    WHERE r.id = t.id
    RETURNING r.*, t.previous_agent_id
), cleared_chat_sessions AS (
    DELETE FROM channel_chat_session_binding b
    USING updated u
    WHERE u.previous_agent_id IS DISTINCT FROM u.agent_id
      AND b.installation_id = u.installation_id
      AND b.channel_chat_id = u.conversation_id
    RETURNING b.chat_session_id
), cleared_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared_chat_sessions)
)
SELECT id, workspace_id, installation_id, conversation_id,
       conversation_title, agent_id, revision, discovered_at, updated_at
FROM updated;

-- name: DingTalkGroupRouteMatchesAgent :one
SELECT EXISTS (
    SELECT 1
    FROM dingtalk_group_route r
    JOIN agent a ON a.id = r.agent_id
                AND a.workspace_id = r.workspace_id
    JOIN dingtalk_connector c ON c.id = r.installation_id
    JOIN dingtalk_workspace_grant g
      ON g.connector_id = c.id AND g.workspace_id = r.workspace_id
    WHERE r.installation_id = sqlc.arg(installation_id)
      AND r.conversation_id = sqlc.arg(conversation_id)::text
      AND r.agent_id = sqlc.arg(agent_id)
      AND r.revision = sqlc.arg(route_revision)
      AND a.kind = 'user'
      AND a.archived_at IS NULL
      AND c.status = 'active'
      AND g.status = 'active'
) AS matches;

-- name: DingTalkDirectRouteMatchesAgent :one
SELECT EXISTS (
    SELECT 1
    FROM dingtalk_direct_route r
    JOIN dingtalk_connector c ON c.id = r.connector_id
    JOIN dingtalk_workspace_grant g
      ON g.connector_id = r.connector_id AND g.workspace_id = r.workspace_id
    JOIN agent a ON a.id = r.agent_id AND a.workspace_id = r.workspace_id
    WHERE r.connector_id = sqlc.arg(connector_id)
      AND r.channel_user_id = sqlc.arg(channel_user_id)
      AND r.workspace_id = sqlc.arg(workspace_id)
      AND r.agent_id = sqlc.arg(agent_id)
      AND r.revision = sqlc.arg(route_revision)
      AND c.status = 'active'
      AND g.status = 'active'
      AND a.kind = 'user'
      AND a.archived_at IS NULL
) AS matches;

-- name: LockDingTalkGroupRouteForAppend :one
-- This is the durable cutover fence. The active target agent and route are
-- locked in the same order as ReassignDingTalkGroupRoute, and the lock remains
-- held by AppendUserMessage's transaction until the message and dedup mark
-- commit. A PATCH that wins first changes revision and produces no row; an
-- inbound append that wins first makes PATCH wait until that append commits.
SELECT r.revision
FROM dingtalk_group_route r
JOIN dingtalk_connector c ON c.id = r.installation_id
JOIN dingtalk_workspace_grant g
  ON g.connector_id = c.id AND g.workspace_id = r.workspace_id
JOIN agent a ON a.id = r.agent_id AND a.workspace_id = r.workspace_id
JOIN member m ON m.workspace_id = r.workspace_id
             AND m.user_id = sqlc.arg(multica_user_id)
WHERE r.installation_id = sqlc.arg(installation_id)
  AND r.conversation_id = sqlc.arg(conversation_id)::text
  AND r.workspace_id = sqlc.arg(workspace_id)
  AND r.agent_id = sqlc.arg(agent_id)
  AND r.revision = sqlc.arg(route_revision)
  AND c.status = 'active'
  AND g.status = 'active'
  AND a.kind = 'user'
  AND a.archived_at IS NULL
FOR SHARE OF r;

-- name: LockDingTalkDirectRouteForAppend :one
SELECT r.revision
FROM dingtalk_direct_route r
JOIN dingtalk_connector c ON c.id = r.connector_id
JOIN dingtalk_workspace_grant g
  ON g.connector_id = r.connector_id AND g.workspace_id = r.workspace_id
JOIN agent a ON a.id = r.agent_id AND a.workspace_id = r.workspace_id
JOIN member m ON m.workspace_id = r.workspace_id
             AND m.user_id = sqlc.arg(multica_user_id)
WHERE r.connector_id = sqlc.arg(connector_id)
  AND r.channel_user_id = sqlc.arg(channel_user_id)
  AND r.workspace_id = sqlc.arg(workspace_id)
  AND r.agent_id = sqlc.arg(agent_id)
  AND r.revision = sqlc.arg(route_revision)
  AND c.status = 'active'
  AND g.status = 'active'
  AND a.kind = 'user'
  AND a.archived_at IS NULL
FOR SHARE OF r;

-- name: LockDingTalkGroupOutboundRoute :one
-- Hold this route through the DingTalk network send. A concurrent workspace or
-- agent switch must either complete first (and make this return no row) or wait
-- until the old route's reply has been delivered.
SELECT r.revision
FROM dingtalk_group_route r
JOIN dingtalk_connector c ON c.id = r.installation_id
JOIN dingtalk_workspace_grant g
  ON g.connector_id = r.installation_id AND g.workspace_id = r.workspace_id
JOIN agent a ON a.id = r.agent_id AND a.workspace_id = r.workspace_id
JOIN member m ON m.workspace_id = r.workspace_id
             AND m.user_id = sqlc.arg(multica_user_id)
WHERE r.installation_id = sqlc.arg(installation_id)
  AND r.conversation_id = sqlc.arg(conversation_id)::text
  AND r.workspace_id = sqlc.arg(workspace_id)
  AND r.agent_id = sqlc.arg(agent_id)
  AND c.status = 'active'
  AND g.status = 'active'
  AND a.kind = 'user'
  AND a.archived_at IS NULL
FOR SHARE OF r;

-- name: LockDingTalkDirectOutboundRoute :one
SELECT r.revision
FROM dingtalk_direct_route r
JOIN dingtalk_connector c ON c.id = r.connector_id
JOIN dingtalk_workspace_grant g
  ON g.connector_id = r.connector_id AND g.workspace_id = r.workspace_id
JOIN agent a ON a.id = r.agent_id AND a.workspace_id = r.workspace_id
JOIN member m ON m.workspace_id = r.workspace_id
             AND m.user_id = sqlc.arg(multica_user_id)
WHERE r.connector_id = sqlc.arg(connector_id)
  AND r.channel_user_id = sqlc.arg(channel_user_id)
  AND r.workspace_id = sqlc.arg(workspace_id)
  AND r.agent_id = sqlc.arg(agent_id)
  AND c.status = 'active'
  AND g.status = 'active'
  AND a.kind = 'user'
  AND a.archived_at IS NULL
FOR SHARE OF r;

-- name: LockDingTalkGroupReplyRoute :one
-- Immediate product replies (/issue confirmation, status notices, processing
-- ack) are fenced by the exact inbound route revision. The agent may now be
-- archived; the notice still belongs to this route, so only identity,
-- membership, connector, and grant validity are required here.
SELECT r.revision
FROM dingtalk_group_route r
JOIN dingtalk_connector c ON c.id = r.installation_id
JOIN dingtalk_workspace_grant g
  ON g.connector_id = r.installation_id AND g.workspace_id = r.workspace_id
JOIN member m ON m.workspace_id = r.workspace_id
             AND m.user_id = sqlc.arg(multica_user_id)
WHERE r.installation_id = sqlc.arg(installation_id)
  AND r.conversation_id = sqlc.arg(conversation_id)::text
  AND r.workspace_id = sqlc.arg(workspace_id)
  AND r.agent_id = sqlc.arg(agent_id)
  AND r.revision = sqlc.arg(route_revision)
  AND c.status = 'active'
  AND g.status = 'active'
FOR SHARE OF r;

-- name: LockDingTalkDirectReplyRoute :one
SELECT r.revision
FROM dingtalk_direct_route r
JOIN dingtalk_connector c ON c.id = r.connector_id
JOIN dingtalk_workspace_grant g
  ON g.connector_id = r.connector_id AND g.workspace_id = r.workspace_id
JOIN member m ON m.workspace_id = r.workspace_id
             AND m.user_id = sqlc.arg(multica_user_id)
WHERE r.connector_id = sqlc.arg(connector_id)
  AND r.channel_user_id = sqlc.arg(channel_user_id)
  AND r.workspace_id = sqlc.arg(workspace_id)
  AND r.agent_id = sqlc.arg(agent_id)
  AND r.revision = sqlc.arg(route_revision)
  AND c.status = 'active'
  AND g.status = 'active'
FOR SHARE OF r;

-- name: LockActiveDingTalkConnectorGrantForReply :one
WITH connector_guard AS MATERIALIZED (
    SELECT c.id
    FROM dingtalk_connector c
    WHERE c.id = sqlc.arg(connector_id)
      AND c.status = 'active'
    FOR SHARE OF c
), grant_guard AS MATERIALIZED (
    SELECT g.connector_id
    FROM dingtalk_workspace_grant g
    JOIN connector_guard c ON c.id = g.connector_id
    WHERE g.workspace_id = sqlc.arg(workspace_id)
      AND g.status = 'active'
    FOR SHARE OF g
)
SELECT c.id
FROM connector_guard c
JOIN grant_guard g ON g.connector_id = c.id;

-- name: LockActiveDingTalkConnectorForReply :one
SELECT c.id
FROM dingtalk_connector c
WHERE c.id = sqlc.arg(connector_id)
  AND c.status = 'active'
  AND EXISTS (
      SELECT 1 FROM dingtalk_workspace_grant g
      WHERE g.connector_id = c.id AND g.status = 'active'
  )
FOR SHARE OF c;

-- name: LockDingTalkActiveAgentMemberForRoute :one
-- Call after locking the connector and before locking the workspace grant.
-- Agent-before-member matches member revocation (archive agent, delete member).
WITH agent_guard AS MATERIALIZED (
    SELECT a.id, a.workspace_id
    FROM agent a
    WHERE a.id = sqlc.arg(agent_id)
      AND a.workspace_id = sqlc.arg(workspace_id)
      AND a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE OF a
), member_guard AS MATERIALIZED (
    SELECT m.workspace_id
    FROM member m
    JOIN agent_guard a ON a.workspace_id = m.workspace_id
    WHERE m.user_id = sqlc.arg(multica_user_id)
    FOR SHARE OF m
)
SELECT a.id
FROM agent_guard a
JOIN member_guard m ON m.workspace_id = a.workspace_id;

-- name: LockDingTalkMemberForRoute :one
SELECT m.workspace_id
FROM member m
WHERE m.workspace_id = sqlc.arg(workspace_id)
  AND m.user_id = sqlc.arg(multica_user_id)
FOR SHARE OF m;

-- name: LockActiveDingTalkGrantForRoute :one
SELECT g.connector_id
FROM dingtalk_workspace_grant g
WHERE g.connector_id = sqlc.arg(connector_id)
  AND g.workspace_id = sqlc.arg(workspace_id)
  AND g.status = 'active'
FOR SHARE OF g;

-- name: DeleteDingTalkStaleGroupChatBinding :one
-- A route reassignment normally removes the group's binding in the same query.
-- An inbound message that resolved immediately before the reassignment can,
-- however, finish creating its old-agent binding immediately afterwards. Every
-- subsequent group message runs this guard before EnsureSession. Legacy
-- bindings have no agent stamp; preserve them when the effective route is still
-- the installation's original/default agent, but retire them after a genuine
-- reassignment so the new route can never inherit the old agent's transcript.
WITH cleared AS (
    DELETE FROM channel_chat_session_binding b
    WHERE b.installation_id = sqlc.arg(installation_id)
      AND b.channel_type = 'dingtalk'
      AND b.channel_chat_id = sqlc.arg(conversation_id)::text
      AND COALESCE(
          NULLIF(b.config ->> 'agent_id', ''),
          (
              SELECT g.default_agent_id::text
              FROM dingtalk_group_route r
              JOIN dingtalk_workspace_grant g
                ON g.connector_id = r.installation_id
               AND g.workspace_id = r.workspace_id
              WHERE r.installation_id = b.installation_id
                AND r.conversation_id = b.channel_chat_id
          ),
          ''
      ) <> sqlc.arg(agent_id)::uuid::text
    RETURNING b.chat_session_id
), cleared_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared)
)
SELECT count(*)::bigint AS cleared_count
FROM cleared;

-- name: DeleteDingTalkStaleDirectChatBinding :one
WITH cleared AS (
    DELETE FROM channel_chat_session_binding b
    WHERE b.installation_id = sqlc.arg(connector_id)
      AND b.channel_type = 'dingtalk'
      AND b.channel_chat_id = sqlc.arg(channel_chat_id)::text
      AND COALESCE(NULLIF(b.config ->> 'agent_id', ''), '')
          <> sqlc.arg(agent_id)::uuid::text
    RETURNING b.chat_session_id
), cleared_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared)
)
SELECT count(*)::bigint AS cleared_count
FROM cleared;

-- name: LockDingTalkConnectorAppID :exec
-- One DingTalk AppKey is one global connector. Serialize connector creation and
-- credential rotation before attaching or reactivating a workspace grant.
SELECT pg_advisory_xact_lock(
    hashtextextended(
        'dingtalk:' || sqlc.arg(app_id)::text,
        0
    )
);

-- name: LockDingTalkInstallTarget :one
-- The caller first locks workspace then connector. Agent-before-member matches
-- member revocation, which archives owned agents before deleting membership.
WITH agent_guard AS MATERIALIZED (
    SELECT a.id, a.workspace_id
    FROM agent a
    WHERE a.id = sqlc.arg(agent_id)
      AND a.workspace_id = sqlc.arg(workspace_id)
      AND a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE OF a
), member_guard AS MATERIALIZED (
    SELECT m.workspace_id
    FROM member m
    JOIN agent_guard a ON a.workspace_id = m.workspace_id
    WHERE m.workspace_id = sqlc.arg(workspace_id)
      AND m.user_id = sqlc.arg(installer_user_id)
      AND m.role IN ('owner', 'admin')
    FOR SHARE OF m
)
SELECT a.id
FROM agent_guard a
JOIN member_guard m ON m.workspace_id = a.workspace_id;

-- name: GetDingTalkConnectorByAppIDForUpdate :one
SELECT *
FROM dingtalk_connector
WHERE app_id = sqlc.arg(app_id)
FOR UPDATE;

-- name: CreateDingTalkConnector :one
INSERT INTO dingtalk_connector (
    app_id, config, installer_user_id
) VALUES (
    sqlc.arg(app_id), sqlc.arg(config), sqlc.arg(installer_user_id)
)
RETURNING *;

-- name: UpdateDingTalkConnectorCredentials :one
UPDATE dingtalk_connector
SET config = sqlc.arg(config),
    status = 'active',
    installer_user_id = sqlc.arg(installer_user_id),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpsertDingTalkWorkspaceGrant :one
INSERT INTO dingtalk_workspace_grant (
    connector_id, workspace_id, default_agent_id, installer_user_id
) VALUES (
    sqlc.arg(connector_id), sqlc.arg(workspace_id),
    sqlc.arg(default_agent_id), sqlc.arg(installer_user_id)
)
ON CONFLICT (connector_id, workspace_id) DO UPDATE SET
    default_agent_id = EXCLUDED.default_agent_id,
    installer_user_id = EXCLUDED.installer_user_id,
    status = 'active',
    updated_at = now()
RETURNING *;

-- name: ListDingTalkInstallationsByWorkspace :many
SELECT c.id, g.workspace_id, g.default_agent_id AS agent_id,
       'dingtalk'::text AS channel_type, c.config,
       CASE WHEN c.status = 'active' AND g.status = 'active'
            THEN 'active' ELSE 'revoked' END::text AS status,
       c.ws_lease_token, c.ws_lease_expires_at, g.installer_user_id,
       g.installed_at, g.created_at,
       GREATEST(c.updated_at, g.updated_at)::timestamptz AS updated_at
FROM dingtalk_workspace_grant g
JOIN dingtalk_connector c ON c.id = g.connector_id
WHERE g.workspace_id = sqlc.arg(workspace_id)
ORDER BY g.installed_at ASC;

-- name: GetDingTalkInstallationInWorkspace :one
SELECT c.id, g.workspace_id, g.default_agent_id AS agent_id,
       'dingtalk'::text AS channel_type, c.config,
       CASE WHEN c.status = 'active' AND g.status = 'active'
            THEN 'active' ELSE 'revoked' END::text AS status,
       c.ws_lease_token, c.ws_lease_expires_at, g.installer_user_id,
       g.installed_at, g.created_at,
       GREATEST(c.updated_at, g.updated_at)::timestamptz AS updated_at
FROM dingtalk_workspace_grant g
JOIN dingtalk_connector c ON c.id = g.connector_id
WHERE c.id = sqlc.arg(connector_id)
  AND g.workspace_id = sqlc.arg(workspace_id);

-- name: LockDingTalkConnectorForUpdate :one
SELECT *
FROM dingtalk_connector
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: RevokeDingTalkWorkspaceGrantOnly :one
UPDATE dingtalk_workspace_grant
SET status = 'revoked', updated_at = now()
WHERE connector_id = sqlc.arg(connector_id)
  AND workspace_id = sqlc.arg(workspace_id)
RETURNING connector_id;

-- name: CountActiveDingTalkWorkspaceGrants :one
SELECT count(*)::bigint
FROM dingtalk_workspace_grant
WHERE connector_id = sqlc.arg(connector_id)
  AND status = 'active';

-- name: RevokeDingTalkConnector :exec
UPDATE dingtalk_connector
SET status = 'revoked',
    config = config - 'app_secret_encrypted',
    ws_lease_token = NULL,
    ws_lease_expires_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg(id);

-- name: PurgeDingTalkConnectorUnscopedAudit :exec
-- Once the last workspace grant is gone there is no tenant that can own
-- connector-level drops recorded before routing resolved a workspace.
DELETE FROM channel_inbound_audit
WHERE installation_id = sqlc.arg(connector_id)
  AND workspace_id IS NULL;

-- name: ListAllActiveDingTalkConnectors :many
SELECT c.*
FROM dingtalk_connector c
WHERE c.status = 'active'
  AND EXISTS (
      SELECT 1 FROM dingtalk_workspace_grant g
      WHERE g.connector_id = c.id AND g.status = 'active'
  )
ORDER BY c.created_at ASC;

-- name: AcquireDingTalkConnectorWSLease :one
UPDATE dingtalk_connector
SET ws_lease_token = sqlc.arg(new_token),
    ws_lease_expires_at = sqlc.arg(new_expires_at),
    updated_at = now()
WHERE dingtalk_connector.id = sqlc.arg(id)
  AND status = 'active'
  AND EXISTS (
      SELECT 1 FROM dingtalk_workspace_grant g
      WHERE g.connector_id = dingtalk_connector.id AND g.status = 'active'
  )
  AND (
      ws_lease_token IS NULL OR ws_lease_expires_at IS NULL OR
      ws_lease_expires_at < now() OR ws_lease_token = sqlc.arg(new_token)
  )
RETURNING dingtalk_connector.id;

-- name: ReleaseDingTalkConnectorWSLease :exec
UPDATE dingtalk_connector
SET ws_lease_token = NULL, ws_lease_expires_at = NULL, updated_at = now()
WHERE id = sqlc.arg(id)
  AND ws_lease_token = sqlc.arg(current_token);

-- name: GetDingTalkConnectorByAppID :one
SELECT *
FROM dingtalk_connector
WHERE app_id = sqlc.arg(app_id);

-- name: GetDingTalkConnector :one
SELECT *
FROM dingtalk_connector
WHERE id = sqlc.arg(id);

-- name: GetActiveDingTalkConnectorInWorkspace :one
SELECT c.*
FROM dingtalk_connector c
JOIN dingtalk_workspace_grant g ON g.connector_id = c.id
WHERE c.id = sqlc.arg(connector_id)
  AND g.workspace_id = sqlc.arg(workspace_id)
  AND c.status = 'active'
  AND g.status = 'active';

-- name: ResolveDingTalkBindingWorkspace :one
WITH candidates AS MATERIALIZED (
    SELECT g.workspace_id, g.installer_user_id
    FROM dingtalk_workspace_grant g
    JOIN dingtalk_connector c ON c.id = g.connector_id
    JOIN workspace w ON w.id = g.workspace_id
    WHERE g.connector_id = sqlc.arg(connector_id)
      AND g.status = 'active'
      AND c.status = 'active'
      AND (
          sqlc.arg(workspace_slug)::text = '' OR
          w.slug = sqlc.arg(workspace_slug)::text
      )
)
SELECT workspace_id, installer_user_id
FROM candidates
WHERE sqlc.arg(workspace_slug)::text <> ''
   OR (SELECT count(*) FROM candidates) = 1
LIMIT 1;

-- name: CountDingTalkEligibleWorkspaceGrants :one
SELECT count(*)::bigint
FROM dingtalk_workspace_grant g
JOIN dingtalk_connector c ON c.id = g.connector_id
JOIN member m ON m.workspace_id = g.workspace_id
             AND m.user_id = sqlc.arg(multica_user_id)
WHERE g.connector_id = sqlc.arg(connector_id)
  AND g.status = 'active'
  AND c.status = 'active';

-- name: GetDingTalkDirectRoute :one
SELECT r.workspace_id, r.agent_id, r.revision, g.installer_user_id,
       EXISTS (
           SELECT 1 FROM agent a
           WHERE a.id = r.agent_id
             AND a.workspace_id = r.workspace_id
             AND a.kind = 'user'
             AND a.archived_at IS NULL
       ) AS agent_active
FROM dingtalk_direct_route r
JOIN dingtalk_connector c ON c.id = r.connector_id
JOIN dingtalk_workspace_grant g
  ON g.connector_id = r.connector_id AND g.workspace_id = r.workspace_id
JOIN member m ON m.workspace_id = r.workspace_id
             AND m.user_id = sqlc.arg(multica_user_id)
WHERE r.connector_id = sqlc.arg(connector_id)
  AND r.channel_user_id = sqlc.arg(channel_user_id)
  AND c.status = 'active'
  AND g.status = 'active';

-- name: SelectDingTalkDirectWorkspaceRoute :one
WITH workspace_guard AS MATERIALIZED (
    SELECT w.id
    FROM workspace w
    WHERE w.slug = sqlc.arg(workspace_slug)::text
      AND EXISTS (
          SELECT 1
          FROM dingtalk_workspace_grant g
          JOIN member m ON m.workspace_id = g.workspace_id
                       AND m.user_id = sqlc.arg(multica_user_id)
          WHERE g.connector_id = sqlc.arg(connector_id)
            AND g.workspace_id = w.id
            AND g.status = 'active'
      )
    FOR KEY SHARE OF w
), connector_guard AS MATERIALIZED (
    SELECT c.id
    FROM dingtalk_connector c
    WHERE c.id = sqlc.arg(connector_id)
      AND c.status = 'active'
      AND EXISTS (SELECT 1 FROM workspace_guard)
    FOR SHARE OF c
), candidate_grant AS MATERIALIZED (
    SELECT g.workspace_id, g.default_agent_id, g.installer_user_id
    FROM dingtalk_workspace_grant g
    JOIN workspace_guard w ON w.id = g.workspace_id
    JOIN connector_guard c ON c.id = g.connector_id
    WHERE g.connector_id = sqlc.arg(connector_id)
      AND g.status = 'active'
), agent_guard AS MATERIALIZED (
    SELECT a.id, a.workspace_id
    FROM agent a
    JOIN candidate_grant g ON g.default_agent_id = a.id
                          AND g.workspace_id = a.workspace_id
    WHERE a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE OF a
), member_guard AS MATERIALIZED (
    SELECT m.workspace_id
    FROM member m
    JOIN agent_guard a ON a.workspace_id = m.workspace_id
    WHERE m.user_id = sqlc.arg(multica_user_id)
    FOR SHARE OF m
), authorization_guard AS MATERIALIZED (
    SELECT g.workspace_id, g.default_agent_id, g.installer_user_id
    FROM dingtalk_workspace_grant g
    JOIN candidate_grant candidate
      ON candidate.workspace_id = g.workspace_id
     AND candidate.default_agent_id = g.default_agent_id
    JOIN agent_guard a ON a.id = g.default_agent_id
    JOIN member_guard m ON m.workspace_id = g.workspace_id
    WHERE g.connector_id = sqlc.arg(connector_id)
      AND g.status = 'active'
    FOR SHARE OF g
), target AS MATERIALIZED (
    SELECT workspace_id, default_agent_id, installer_user_id
    FROM authorization_guard
), previous AS MATERIALIZED (
    SELECT * FROM dingtalk_direct_route r
    WHERE r.connector_id = sqlc.arg(connector_id)
      AND r.channel_user_id = sqlc.arg(channel_user_id)
), selected AS (
    INSERT INTO dingtalk_direct_route (
        connector_id, channel_user_id, channel_chat_id, workspace_id, agent_id
    )
    SELECT sqlc.arg(connector_id), sqlc.arg(channel_user_id)::text,
           sqlc.arg(channel_chat_id)::text, workspace_id, default_agent_id
    FROM target
    ON CONFLICT (connector_id, channel_user_id) DO UPDATE SET
        channel_chat_id = EXCLUDED.channel_chat_id,
        workspace_id = EXCLUDED.workspace_id,
        agent_id = EXCLUDED.agent_id,
        revision = dingtalk_direct_route.revision + 1,
        updated_at = now()
    RETURNING *
), cleared_chat_sessions AS (
    DELETE FROM channel_chat_session_binding b
    USING previous p, selected s
    WHERE (p.workspace_id, p.agent_id) IS DISTINCT FROM (s.workspace_id, s.agent_id)
      AND b.installation_id = s.connector_id
      AND b.channel_type = 'dingtalk'
      AND b.channel_chat_id IN (p.channel_chat_id, s.channel_chat_id)
    RETURNING b.chat_session_id
), cleared_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared_chat_sessions)
)
SELECT s.workspace_id, s.agent_id, s.revision, t.installer_user_id,
       EXISTS (
           SELECT 1 FROM agent a
           WHERE a.id = s.agent_id
             AND a.workspace_id = s.workspace_id
             AND a.kind = 'user'
             AND a.archived_at IS NULL
       ) AS agent_active
FROM selected s
JOIN target t ON t.workspace_id = s.workspace_id;

-- name: SelectDingTalkGroupWorkspaceRoute :one
WITH workspace_guard AS MATERIALIZED (
    SELECT w.id
    FROM workspace w
    WHERE w.slug = sqlc.arg(workspace_slug)::text
      AND EXISTS (
          SELECT 1
          FROM dingtalk_workspace_grant g
          JOIN member m ON m.workspace_id = g.workspace_id
                       AND m.user_id = sqlc.arg(multica_user_id)
          WHERE g.connector_id = sqlc.arg(connector_id)
            AND g.workspace_id = w.id
            AND g.status = 'active'
            AND m.role IN ('owner', 'admin')
      )
    FOR KEY SHARE OF w
), connector_guard AS MATERIALIZED (
    SELECT c.id
    FROM dingtalk_connector c
    WHERE c.id = sqlc.arg(connector_id)
      AND c.status = 'active'
      AND EXISTS (SELECT 1 FROM workspace_guard)
    FOR SHARE OF c
), candidate_grant AS MATERIALIZED (
    SELECT g.workspace_id, g.default_agent_id, g.installer_user_id
    FROM dingtalk_workspace_grant g
    JOIN workspace_guard w ON w.id = g.workspace_id
    JOIN connector_guard c ON c.id = g.connector_id
    WHERE g.connector_id = sqlc.arg(connector_id)
      AND g.status = 'active'
), agent_guard AS MATERIALIZED (
    SELECT a.id, a.workspace_id
    FROM agent a
    JOIN candidate_grant g ON g.default_agent_id = a.id
                          AND g.workspace_id = a.workspace_id
    WHERE a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE OF a
), member_guard AS MATERIALIZED (
    SELECT m.workspace_id
    FROM member m
    JOIN agent_guard a ON a.workspace_id = m.workspace_id
    WHERE m.user_id = sqlc.arg(multica_user_id)
      AND m.role IN ('owner', 'admin')
    FOR SHARE OF m
), authorization_guard AS MATERIALIZED (
    SELECT g.workspace_id, g.default_agent_id, g.installer_user_id
    FROM dingtalk_workspace_grant g
    JOIN candidate_grant candidate
      ON candidate.workspace_id = g.workspace_id
     AND candidate.default_agent_id = g.default_agent_id
    JOIN agent_guard a ON a.id = g.default_agent_id
    JOIN member_guard m ON m.workspace_id = g.workspace_id
    WHERE g.connector_id = sqlc.arg(connector_id)
      AND g.status = 'active'
    FOR SHARE OF g
), target AS MATERIALIZED (
    SELECT workspace_id, default_agent_id, installer_user_id
    FROM authorization_guard
), previous AS MATERIALIZED (
    SELECT * FROM dingtalk_group_route
    WHERE installation_id = sqlc.arg(connector_id)
      AND conversation_id = sqlc.arg(conversation_id)::text
), selected AS (
    INSERT INTO dingtalk_group_route (
        workspace_id, installation_id, conversation_id,
        conversation_title, agent_id
    )
    SELECT workspace_id, sqlc.arg(connector_id),
           sqlc.arg(conversation_id)::text,
           sqlc.arg(conversation_title)::text, default_agent_id
    FROM target
    ON CONFLICT (installation_id, conversation_id) DO UPDATE SET
        workspace_id = EXCLUDED.workspace_id,
        conversation_title = CASE
            WHEN EXCLUDED.conversation_title = '' THEN dingtalk_group_route.conversation_title
            ELSE EXCLUDED.conversation_title
        END,
        agent_id = EXCLUDED.agent_id,
        revision = dingtalk_group_route.revision + 1,
        updated_at = now()
    RETURNING *
), cleared_chat_sessions AS (
    DELETE FROM channel_chat_session_binding b
    USING previous p, selected s
    WHERE (p.workspace_id, p.agent_id) IS DISTINCT FROM (s.workspace_id, s.agent_id)
      AND b.installation_id = s.installation_id
      AND b.channel_type = 'dingtalk'
      AND b.channel_chat_id = s.conversation_id
    RETURNING b.chat_session_id
), cleared_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared_chat_sessions)
)
SELECT s.workspace_id, s.agent_id, s.revision, t.installer_user_id,
       EXISTS (
           SELECT 1 FROM agent a
           WHERE a.id = s.agent_id
             AND a.workspace_id = s.workspace_id
             AND a.kind = 'user'
             AND a.archived_at IS NULL
       ) AS agent_active
FROM selected s
JOIN target t ON t.workspace_id = s.workspace_id;
