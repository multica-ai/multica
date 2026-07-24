-- =====================
-- Jira Connection
-- =====================

-- name: ListJiraConnectionsByWorkspace :many
SELECT * FROM jira_connection
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetJiraConnectionByID :one
SELECT * FROM jira_connection
WHERE id = $1;

-- name: UpsertJiraConnection :one
-- Reconnecting the same site rotates the stored token/secret and identity in
-- place rather than creating a duplicate row (mirrors UpsertVCSConnection).
INSERT INTO jira_connection (
    workspace_id, base_url, account_email,
    api_token_encrypted, webhook_secret_encrypted, connected_by_id, jql
) VALUES (
    $1, $2, $3, $4, $5, sqlc.narg('connected_by_id'), sqlc.narg('jql')
)
ON CONFLICT (workspace_id, base_url) DO UPDATE SET
    account_email            = EXCLUDED.account_email,
    api_token_encrypted      = EXCLUDED.api_token_encrypted,
    webhook_secret_encrypted = EXCLUDED.webhook_secret_encrypted,
    connected_by_id          = EXCLUDED.connected_by_id,
    jql                      = EXCLUDED.jql,
    updated_at               = now()
RETURNING *;

-- name: DeleteJiraConnection :exec
-- These tables carry no FKs (project migration rules), so dependent cleanup
-- is done explicitly here, in one statement so it commits or rolls back
-- atomically with the connection row. The target CTE scopes the child delete
-- to a connection that actually belongs to the workspace, so a wrong
-- workspace_id is a no-op rather than deleting another tenant's link rows.
WITH target AS (
    SELECT jira_connection.id FROM jira_connection
    WHERE jira_connection.id = $1 AND jira_connection.workspace_id = $2
),
cleared_links AS (
    DELETE FROM jira_issue_link
    WHERE connection_id IN (SELECT target.id FROM target)
)
DELETE FROM jira_connection
WHERE jira_connection.id = $1 AND jira_connection.workspace_id = $2;

-- =====================
-- Jira issue link
-- =====================

-- name: GetJiraIssueLink :one
SELECT * FROM jira_issue_link
WHERE connection_id = $1 AND jira_issue_key = $2;

-- name: UpsertJiraIssueLink :one
-- One link per (connection, jira issue key). A webhook redelivery or repeat
-- event refreshes the sync bookkeeping in place; the multica_issue_id is
-- stable after the first insert (the Multica issue is created exactly once
-- per Jira issue).
INSERT INTO jira_issue_link (
    workspace_id, connection_id, jira_issue_key, jira_issue_id,
    multica_issue_id, sync_status, last_inbound_at
) VALUES (
    $1, $2, $3, $4, $5, $6, now()
)
ON CONFLICT (connection_id, jira_issue_key) DO UPDATE SET
    jira_issue_id   = EXCLUDED.jira_issue_id,
    sync_status     = EXCLUDED.sync_status,
    last_inbound_at = now(),
    updated_at      = now()
RETURNING *;

-- name: SyncIssueFromJira :one
-- Narrow inbound-sync write: only the fields Jira owns in PR 1 (title and
-- description). Deliberately NOT reusing UpdateIssue, whose non-COALESCE
-- sqlc.narg fields (assignee, dates, parent, project) would be nulled by a
-- partial update. workspace_id is the SQL-layer tenant guard.
UPDATE issue SET
    title       = $2,
    description = COALESCE(sqlc.narg('description'), description),
    updated_at  = now()
WHERE id = $1 AND workspace_id = $3
RETURNING *;
