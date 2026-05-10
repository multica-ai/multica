-- name: ListWorkspaces :many
SELECT w.* FROM workspace w
JOIN member m ON m.workspace_id = w.id
WHERE m.user_id = $1
ORDER BY w.created_at ASC;

-- name: GetWorkspace :one
SELECT * FROM workspace
WHERE id = $1;

-- name: GetWorkspaceBySlug :one
SELECT * FROM workspace
WHERE slug = $1;

-- name: CreateWorkspace :one
INSERT INTO workspace (name, slug, description, context, issue_prefix)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateWorkspace :one
-- Multiple paired-bool fields use the same "leave alone" / "explicitly write" pattern:
--
-- - channels_enabled / channels_enabled_set + channel_retention_days /
--   channel_retention_days_set: a missing JSON field would otherwise coerce
--   to false/NULL and a single name-update PATCH would silently disable
--   channels or wipe the retention override.
--
-- - ship_hub_enabled / ship_hub_enabled_set: same gate semantics as channels —
--   a name-only PATCH must not silently disable Ship Hub.
--
-- - orchestrator_agent_id / orchestrator_agent_id_set: distinguishes "don't
--   change" from "explicitly clear to NULL". Same narg+bool pattern.
--
-- - ship_hub_approval_{low,medium,high,critical}: per-risk-tier approval
--   rule (Phase 7d follow-up — configurable approvals). String enum
--   validated by the SQL CHECK constraint; the handler does pre-PATCH
--   validation so a typo never reaches the DB. Same paired-bool gate
--   semantics so a name-only PATCH leaves the rule untouched.
--
-- - ship_hub_approver_can_be_author: boolean. Same paired-bool gate.
UPDATE workspace SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    context = COALESCE(sqlc.narg('context'), context),
    settings = COALESCE(sqlc.narg('settings'), settings),
    repos = COALESCE(sqlc.narg('repos'), repos),
    issue_prefix = COALESCE(sqlc.narg('issue_prefix'), issue_prefix),
    channels_enabled = CASE
        WHEN sqlc.arg('channels_enabled_set')::bool THEN COALESCE(sqlc.narg('channels_enabled'), channels_enabled)
        ELSE channels_enabled
    END,
    channel_retention_days = CASE
        WHEN sqlc.arg('channel_retention_days_set')::bool THEN sqlc.narg('channel_retention_days')
        ELSE channel_retention_days
    END,
    ship_hub_enabled = CASE
        WHEN sqlc.arg('ship_hub_enabled_set')::bool THEN COALESCE(sqlc.narg('ship_hub_enabled'), ship_hub_enabled)
        ELSE ship_hub_enabled
    END,
    orchestrator_agent_id = CASE
        WHEN sqlc.arg('orchestrator_agent_id_set')::boolean
        THEN sqlc.narg('orchestrator_agent_id')::uuid
        ELSE orchestrator_agent_id
    END,
    ship_hub_approval_low = CASE
        WHEN sqlc.arg('ship_hub_approval_low_set')::bool THEN COALESCE(sqlc.narg('ship_hub_approval_low'), ship_hub_approval_low)
        ELSE ship_hub_approval_low
    END,
    ship_hub_approval_medium = CASE
        WHEN sqlc.arg('ship_hub_approval_medium_set')::bool THEN COALESCE(sqlc.narg('ship_hub_approval_medium'), ship_hub_approval_medium)
        ELSE ship_hub_approval_medium
    END,
    ship_hub_approval_high = CASE
        WHEN sqlc.arg('ship_hub_approval_high_set')::bool THEN COALESCE(sqlc.narg('ship_hub_approval_high'), ship_hub_approval_high)
        ELSE ship_hub_approval_high
    END,
    ship_hub_approval_critical = CASE
        WHEN sqlc.arg('ship_hub_approval_critical_set')::bool THEN COALESCE(sqlc.narg('ship_hub_approval_critical'), ship_hub_approval_critical)
        ELSE ship_hub_approval_critical
    END,
    ship_hub_approver_can_be_author = CASE
        WHEN sqlc.arg('ship_hub_approver_can_be_author_set')::bool THEN COALESCE(sqlc.narg('ship_hub_approver_can_be_author'), ship_hub_approver_can_be_author)
        ELSE ship_hub_approver_can_be_author
    END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListWorkspacesWithShipHubEnabled :many
-- Used by the periodic Ship Hub reconciler to find workspaces it should
-- sync. Ordered by id for stable iteration.
SELECT * FROM workspace
WHERE ship_hub_enabled = TRUE
ORDER BY id ASC;

-- name: IncrementIssueCounter :one
UPDATE workspace SET issue_counter = issue_counter + 1
WHERE id = $1
RETURNING issue_counter;

-- name: DeleteWorkspace :exec
DELETE FROM workspace WHERE id = $1;
