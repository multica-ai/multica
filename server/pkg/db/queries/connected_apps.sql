-- =====================
-- Connected Apps v2 (MUL-8516)
-- =====================

-- name: UpsertWorkspaceComposioConnection :one
-- Idempotent on (workspace_id, connected_account_id): a repeated callback for
-- the same account reactivates the row instead of inserting another.
INSERT INTO workspace_composio_connection (
    workspace_id, toolkit_slug, auth_config_id, connected_account_id, composio_user_id, connected_by, status
) VALUES (
    $1, $2, $3, $4, $5, $6, 'active'
)
ON CONFLICT (workspace_id, connected_account_id) DO UPDATE SET
    toolkit_slug   = EXCLUDED.toolkit_slug,
    auth_config_id = EXCLUDED.auth_config_id,
    connected_by   = EXCLUDED.connected_by,
    status         = 'active',
    updated_at     = now()
RETURNING *;

-- name: ListWorkspaceComposioConnections :many
-- Active and expired team accounts, newest first. Revoked rows are history.
SELECT * FROM workspace_composio_connection
WHERE workspace_id = $1 AND status <> 'revoked'
ORDER BY connected_at DESC;

-- name: GetWorkspaceComposioConnection :one
SELECT * FROM workspace_composio_connection
WHERE id = $1 AND workspace_id = $2;

-- name: SetWorkspaceComposioConnectionStatus :exec
UPDATE workspace_composio_connection
SET status = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: RevokeOtherWorkspaceComposioConnections :many
-- A workspace holds one team account per app: connecting again replaces the
-- previous account. Returns the replaced rows so their upstream grant can be
-- revoked.
UPDATE workspace_composio_connection
SET status = 'revoked', updated_at = now()
WHERE workspace_id = $1 AND toolkit_slug = $2 AND id <> $3 AND status <> 'revoked'
RETURNING *;

-- name: TouchWorkspaceComposioConnection :exec
UPDATE workspace_composio_connection SET last_used_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: TouchUserComposioConnection :exec
UPDATE user_composio_connection SET last_used_at = now()
WHERE id = $1 AND user_id = $2;

-- name: SetComposioConnectionStatusByAccount :exec
-- Webhook path: Composio names only the connected account. Applies to either
-- kind of row; revoked rows stay revoked.
WITH personal AS (
    UPDATE user_composio_connection
    SET status = sqlc.arg('status')::text, updated_at = now()
    WHERE connected_account_id = sqlc.arg('connected_account_id')::text AND status <> 'revoked'
)
UPDATE workspace_composio_connection
SET status = sqlc.arg('status')::text, updated_at = now()
WHERE connected_account_id = sqlc.arg('connected_account_id')::text AND status <> 'revoked';

-- name: ListUserComposioConnectionsAnyStatus :many
-- A member's personal accounts in one workspace for the app list: active and
-- expired.
SELECT * FROM user_composio_connection
WHERE user_id = $1 AND status <> 'revoked'
  AND (workspace_id = sqlc.arg('workspace_id')::uuid OR workspace_id IS NULL)
ORDER BY (workspace_id IS NULL), connected_at DESC;

-- name: ListAgentApps :many
SELECT * FROM agent_app
WHERE agent_id = $1
ORDER BY toolkit_slug;

-- name: ListAgentAppsInWorkspace :many
SELECT * FROM agent_app
WHERE workspace_id = $1
ORDER BY toolkit_slug, agent_id;

-- name: UpsertAgentApp :one
INSERT INTO agent_app (workspace_id, agent_id, toolkit_slug, account_scope, level, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (agent_id, toolkit_slug) DO UPDATE SET
    account_scope = EXCLUDED.account_scope,
    level         = EXCLUDED.level,
    updated_at    = now()
RETURNING *;

-- name: DeleteAgentAppsExcept :exec
DELETE FROM agent_app
WHERE agent_id = $1 AND NOT (toolkit_slug = ANY(sqlc.arg('keep')::text[]));

-- name: ListAppActionRules :many
-- Rules of one account: owner_user_id NULL is the team account.
SELECT * FROM app_action_rule
WHERE workspace_id = $1
  AND toolkit_slug = $2
  AND owner_user_id IS NOT DISTINCT FROM sqlc.narg('owner_user_id')::uuid;

-- name: ListAppActionRulesForRun :many
-- Every rule that can apply to a run: the team rules and the triggering
-- member's personal rules for the run's apps.
SELECT * FROM app_action_rule
WHERE workspace_id = $1
  AND toolkit_slug = ANY(sqlc.arg('toolkit_slugs')::text[])
  AND (owner_user_id IS NULL OR owner_user_id = sqlc.narg('owner_user_id')::uuid);

-- name: UpsertAppActionRule :one
INSERT INTO app_action_rule (workspace_id, owner_user_id, toolkit_slug, action_class, policy, updated_by)
VALUES ($1, sqlc.narg('owner_user_id')::uuid, $2, $3, $4, $5)
ON CONFLICT (workspace_id, toolkit_slug, action_class, COALESCE(owner_user_id, '00000000-0000-0000-0000-000000000000'::uuid))
DO UPDATE SET policy = EXCLUDED.policy, updated_by = EXCLUDED.updated_by, updated_at = now()
RETURNING *;

-- name: CreateAppActionRequest :one
INSERT INTO app_action_request (
    workspace_id, kind, task_id, agent_id, issue_id, chat_session_id, approver_user_id,
    account_scope, toolkit_slug, tool_slug, action_class, arguments, summary, expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
RETURNING *;

-- name: FindPendingAppActionRequest :one
-- The same run asking for the same call again (an agent retrying after a
-- "submitted" answer) reuses the open request instead of stacking another.
SELECT * FROM app_action_request
WHERE task_id = $1 AND kind = $2 AND toolkit_slug = $3 AND tool_slug = $4
  AND arguments = sqlc.arg('arguments')::jsonb AND status = 'pending'
ORDER BY created_at DESC
LIMIT 1;

-- name: GetAppActionRequest :one
SELECT * FROM app_action_request
WHERE id = $1 AND workspace_id = $2;

-- name: ListPendingAppActionRequestsForIssue :many
-- Every request of an issue still waiting for a decision. Expiry and the
-- per-minute call budget keep this short; the cap only bounds a runaway.
SELECT * FROM app_action_request
WHERE workspace_id = $1 AND issue_id = $2 AND status = 'pending'
ORDER BY created_at
LIMIT 500;

-- name: ListDecidedAppActionRequestsForIssue :many
-- One page of an issue's decided requests, newest first, before a cursor.
SELECT * FROM app_action_request
WHERE workspace_id = $1 AND issue_id = $2 AND status <> 'pending'
  AND (sqlc.narg('before')::timestamptz IS NULL OR created_at < sqlc.narg('before')::timestamptz)
ORDER BY created_at DESC
LIMIT sqlc.arg('page_size');

-- name: ListPendingAppActionRequestsForChatSession :many
SELECT * FROM app_action_request
WHERE workspace_id = $1 AND chat_session_id = $2 AND status = 'pending'
ORDER BY created_at
LIMIT 500;

-- name: ListDecidedAppActionRequestsForChatSession :many
SELECT * FROM app_action_request
WHERE workspace_id = $1 AND chat_session_id = $2 AND status <> 'pending'
  AND (sqlc.narg('before')::timestamptz IS NULL OR created_at < sqlc.narg('before')::timestamptz)
ORDER BY created_at DESC
LIMIT sqlc.arg('page_size');

-- name: CancelAppActionRequest :one
-- Withdraws one pending request whose approver may no longer act on it.
UPDATE app_action_request
SET status = 'cancelled', updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'pending'
RETURNING *;

-- name: CancelPendingAppActionRequestsForApprover :many
-- A member leaving the workspace withdraws everything waiting on them.
UPDATE app_action_request
SET status = 'cancelled', updated_at = now()
WHERE workspace_id = $1 AND approver_user_id = $2 AND status = 'pending'
RETURNING *;

-- name: ListPendingAppActionRequestsForApprover :many
-- Requests waiting on the person, and for an admin also every request for a
-- team account, which any admin answers.
SELECT * FROM app_action_request
WHERE workspace_id = $1 AND status = 'pending'
  AND (approver_user_id = $2 OR (sqlc.arg('as_admin')::bool AND kind = 'connect' AND account_scope = 'team'))
ORDER BY created_at
LIMIT 200;

-- name: DecideAppActionRequest :one
-- Compare-and-set from pending: a second click, another device, or the expiry
-- sweep racing the decision finds no pending row and returns no rows.
UPDATE app_action_request
SET status = $3,
    approved_arguments = sqlc.narg('approved_arguments')::jsonb,
    reason = sqlc.narg('reason')::text,
    decided_by = $4,
    decided_at = now(),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'pending'
  AND (approver_user_id = $4 OR (sqlc.arg('as_admin')::bool AND kind = 'connect' AND account_scope = 'team'))
RETURNING *;

-- name: SetAppActionRequestOutcome :one
-- Records what executing an approved request produced. Only moves an approved
-- request; status becomes 'failed' when execution failed.
UPDATE app_action_request
SET status = $3, result = sqlc.narg('result')::jsonb, error = sqlc.narg('error')::text, updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'approved'
RETURNING *;

-- name: ExpireDueAppActionRequests :many
UPDATE app_action_request
SET status = 'expired', updated_at = now()
WHERE id IN (
    SELECT r.id FROM app_action_request r
    WHERE r.status = 'pending' AND r.expires_at <= now()
    ORDER BY r.expires_at
    LIMIT 200
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: ClaimAppActionRequestReminders :many
-- Pending requests within two hours of expiry that have not been reminded.
UPDATE app_action_request
SET reminded_at = now(), updated_at = now()
WHERE id IN (
    SELECT r.id FROM app_action_request r
    WHERE r.status = 'pending' AND r.reminded_at IS NULL
      AND NOT (r.kind = 'connect' AND r.account_scope = 'team')
      AND r.expires_at <= now() + interval '2 hours'
      AND r.expires_at > now()
    ORDER BY r.expires_at
    LIMIT 200
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: CancelPendingTeamAppActionRequests :many
-- Disconnecting a team account cancels what was waiting to use it.
UPDATE app_action_request
SET status = 'cancelled', updated_at = now()
WHERE workspace_id = $1 AND toolkit_slug = $2 AND account_scope = 'team' AND status = 'pending'
RETURNING *;

-- name: CountPendingTeamAppActionRequests :one
SELECT count(*) FROM app_action_request
WHERE workspace_id = $1 AND toolkit_slug = $2 AND account_scope = 'team' AND status = 'pending';

-- name: CountActiveTasksForAgents :one
SELECT count(*) FROM agent_task_queue
WHERE agent_id = ANY(sqlc.arg('agent_ids')::uuid[])
  AND status IN ('queued', 'dispatched', 'running');

-- name: CountActiveAutopilotsForAgents :one
SELECT count(*) FROM autopilot
WHERE workspace_id = $1
  AND assignee_type = 'agent'
  AND assignee_id = ANY(sqlc.arg('agent_ids')::uuid[])
  AND status = 'active';

-- name: InsertAppToolCall :exec
INSERT INTO app_tool_call (
    workspace_id, task_id, agent_id, issue_id, chat_session_id, triggered_by,
    account_scope, account_owner_user_id, toolkit_slug, tool_slug, action_class,
    outcome, request_id, error, metered_period_start
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
);

-- name: ListTeamAppToolCalls :many
SELECT * FROM app_tool_call
WHERE workspace_id = $1 AND toolkit_slug = $2 AND account_scope = 'team'
ORDER BY created_at DESC
LIMIT $3;

-- name: ListPersonalAppToolCalls :many
-- A member's own account activity. Only the owner reads this.
SELECT * FROM app_tool_call
WHERE workspace_id = $1 AND toolkit_slug = $2 AND account_scope = 'personal'
  AND account_owner_user_id = $3
ORDER BY created_at DESC
LIMIT $4;

-- name: GetWorkspaceAppSetting :one
SELECT * FROM workspace_app_setting WHERE workspace_id = $1;

-- name: UpsertWorkspaceAppSetting :one
INSERT INTO workspace_app_setting (workspace_id, catalog_mode, allowed_toolkits, updated_by)
VALUES ($1, $2, sqlc.arg('allowed_toolkits')::text[], $3)
ON CONFLICT (workspace_id) DO UPDATE SET
    catalog_mode     = EXCLUDED.catalog_mode,
    allowed_toolkits = EXCLUDED.allowed_toolkits,
    updated_by       = EXCLUDED.updated_by,
    updated_at       = now()
RETURNING *;

-- name: ListAppToolClassOverrides :many
SELECT * FROM app_tool_class_override
WHERE workspace_id = $1 AND toolkit_slug = $2;

-- name: GetAppToolClassOverride :one
SELECT action_class FROM app_tool_class_override
WHERE workspace_id = $1 AND toolkit_slug = $2 AND tool_slug = $3;

-- name: UpsertAppToolClassOverride :exec
INSERT INTO app_tool_class_override (workspace_id, toolkit_slug, tool_slug, action_class, updated_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, toolkit_slug, tool_slug) DO UPDATE SET
    action_class = EXCLUDED.action_class,
    updated_by   = EXCLUDED.updated_by,
    updated_at   = now();

-- name: DeleteAppToolClassOverride :exec
DELETE FROM app_tool_class_override
WHERE workspace_id = $1 AND toolkit_slug = $2 AND tool_slug = $3;

-- name: CountRecentAppAccountCalls :one
-- Calls that reached the app through one account within the window; backs the
-- per-account rate limit. Blocked, pending and limited calls never ran.
SELECT count(*) FROM app_tool_call
WHERE workspace_id = $1 AND toolkit_slug = $2 AND account_scope = $3
  AND account_owner_user_id IS NOT DISTINCT FROM sqlc.narg('owner')::uuid
  AND outcome IN ('ok', 'error', 'approved')
  AND created_at > sqlc.arg('since')::timestamptz;

-- name: HasBlockedAppToolCall :one
-- Whether a run already hit a block on this tool, so the person is told once.
SELECT EXISTS (
    SELECT 1 FROM app_tool_call
    WHERE workspace_id = $1 AND toolkit_slug = $2 AND task_id = $3
      AND tool_slug = $4 AND outcome = 'blocked'
      AND created_at > sqlc.arg('since')::timestamptz
);

-- name: ListIssueAppToolCalls :many
-- The latest calls the runs of one issue made, newest first. Arguments are
-- never stored.
SELECT * FROM app_tool_call
WHERE workspace_id = $1 AND issue_id = $2
ORDER BY created_at DESC
LIMIT 500;

-- name: ListPendingConnectRequestsForUser :many
-- Connection requests a member is asked to answer for one app; resolved when
-- they finish connecting it.
SELECT * FROM app_action_request
WHERE workspace_id = $1 AND approver_user_id = $2 AND toolkit_slug = $3 AND kind = 'connect'
  AND status = 'pending' AND account_scope = 'personal';

-- name: ListPendingTeamConnectRequests :many
-- Everything waiting for the workspace's team account for one app: agents'
-- requests and members' asks. Connecting the account answers them all.
SELECT * FROM app_action_request
WHERE workspace_id = $1 AND toolkit_slug = $2 AND kind = 'connect'
  AND account_scope = 'team' AND status = 'pending'
ORDER BY created_at
LIMIT 500;

-- name: FindPendingTeamAsk :one
-- A member's own open ask for a team account, made outside any run.
SELECT * FROM app_action_request
WHERE workspace_id = $1 AND toolkit_slug = $2 AND approver_user_id = $3
  AND task_id IS NULL AND kind = 'connect' AND account_scope = 'team' AND status = 'pending'
LIMIT 1;

-- name: ArchiveAppRequestInbox :many
-- A withdrawn ask leaves the admins nothing to answer.
UPDATE inbox_item SET archived = true, snoozed_until = NULL, snoozed_at = NULL, returned_at = NULL
WHERE workspace_id = $1 AND type = 'app_connect_requested' AND archived = false
  AND details->>'request_id' = sqlc.arg('request_id')::text
RETURNING recipient_type, recipient_id;

-- name: ListTeamAccountRequestSummaries :many
-- One row per app with open requests for a team account: who is waiting and
-- which agents, oldest first. A person or agent waiting on several requests
-- repeats; callers drop the repeats.
SELECT toolkit_slug,
       array_agg(approver_user_id ORDER BY created_at)::uuid[] AS requested_by,
       COALESCE(array_agg(approver_user_id ORDER BY created_at) FILTER (WHERE task_id IS NULL), '{}')::uuid[] AS asked_by,
       COALESCE(array_agg(agent_id ORDER BY created_at) FILTER (WHERE agent_id IS NOT NULL), '{}')::uuid[] AS agent_ids,
       max(created_at)::timestamptz AS last_requested_at
FROM app_action_request
WHERE workspace_id = $1 AND kind = 'connect' AND account_scope = 'team' AND status = 'pending'
GROUP BY toolkit_slug
ORDER BY max(created_at) DESC
LIMIT 500;

-- name: AppendQueuedTaskHandoffNote :execrows
-- Folds a decision into a run that has not been claimed yet, so approving a
-- batch of requests starts one run instead of one per request.
UPDATE agent_task_queue
SET handoff_note = CASE
        WHEN handoff_note IS NULL OR handoff_note = '' THEN sqlc.arg('note')::text
        ELSE handoff_note || E'\n\n' || sqlc.arg('note')::text
    END
WHERE id = $1 AND status = 'queued';

-- name: SetAgentComposioToolkitAllowlist :exec
-- Keeps the legacy allowlist column in step with agent_app for older clients.
UPDATE agent SET composio_toolkit_allowlist = sqlc.arg('slugs')::text[], updated_at = now()
WHERE id = $1;

-- name: ListPersonalAppAgentsForUser :many
-- Agents in every workspace the user belongs to that act through the
-- personal account of whoever triggers them for one app. The caller still
-- filters by who may run each agent.
SELECT a.id, a.name, a.workspace_id, w.name AS workspace_name
FROM agent_app aa
JOIN agent a ON a.id = aa.agent_id AND a.workspace_id = aa.workspace_id
JOIN member m ON m.workspace_id = aa.workspace_id AND m.user_id = sqlc.arg('user_id')
JOIN workspace w ON w.id = aa.workspace_id
WHERE aa.toolkit_slug = sqlc.arg('toolkit_slug')
  AND aa.account_scope = 'personal'
  AND a.archived_at IS NULL
  AND a.kind = 'user'
ORDER BY w.name, a.name, a.id
LIMIT 200;

-- name: ListAgentsWithActiveTasksForOriginator :many
SELECT DISTINCT agent_id FROM agent_task_queue
WHERE agent_id = ANY(sqlc.arg('agent_ids')::uuid[])
  AND originator_user_id = sqlc.arg('originator_user_id')
  AND status IN ('queued', 'dispatched', 'running');
