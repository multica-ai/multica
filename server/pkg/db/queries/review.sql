-- Reviews (spec 4.6). A review is written once per subject and period start.

-- name: CreateReview :one
INSERT INTO review (
    id, workspace_id, subject_type, subject_id, period_start, period_end,
    scorecard_id, rating, narrative, recommendations, reviewer_agent_id
) VALUES (
    @id, @workspace_id, @subject_type, @subject_id, @period_start, @period_end,
    @scorecard_id, @rating, @narrative, @recommendations, @reviewer_agent_id
)
RETURNING *;

-- name: GetReviewInWorkspace :one
SELECT * FROM review WHERE id = @id AND workspace_id = @workspace_id;

-- name: DeleteWorkspaceReviews :exec
-- Workspace teardown: reviews have no foreign keys.
DELETE FROM review WHERE workspace_id = @workspace_id;

-- name: GetReviewForSubjectPeriod :one
SELECT * FROM review
WHERE workspace_id = @workspace_id
  AND subject_type = @subject_type
  AND subject_id = @subject_id
  AND period_start = @period_start;

-- name: ListReviews :many
-- Reviews of a workspace, newest first. subject_ids narrows the list for callers
-- who see only some subjects; since limits it to reviews published after it.
SELECT * FROM review
WHERE workspace_id = @workspace_id
  AND (sqlc.narg('subject_type')::text IS NULL OR subject_type = sqlc.narg('subject_type')::text)
  AND (sqlc.narg('subject_id')::uuid IS NULL OR subject_id = sqlc.narg('subject_id')::uuid)
  AND (sqlc.narg('subject_ids')::uuid[] IS NULL OR subject_id = ANY(sqlc.narg('subject_ids')::uuid[]))
  AND (sqlc.narg('since')::timestamptz IS NULL OR created_at >= sqlc.narg('since')::timestamptz)
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: GetReviewerAgent :one
-- The reviewer agent of the workspace: the live agent that a live squad lists as
-- its reviewer. The squad card gives the People & Performance squad one.
SELECT a.* FROM squad_member sm
JOIN squad s ON s.id = sm.squad_id
JOIN agent a ON a.id = sm.member_id
WHERE s.workspace_id = @workspace_id
  AND s.archived_at IS NULL
  AND sm.member_type = 'agent'
  AND sm.role = 'reviewer'
  AND a.archived_at IS NULL
ORDER BY sm.created_at ASC
LIMIT 1;

-- name: GetReviewerSquad :one
SELECT s.* FROM squad_member sm
JOIN squad s ON s.id = sm.squad_id
WHERE s.workspace_id = @workspace_id
  AND s.archived_at IS NULL
  AND sm.member_type = 'agent'
  AND sm.role = 'reviewer'
ORDER BY sm.created_at ASC
LIMIT 1;

-- name: GetSquadLeaderOfAgentHome :one
-- The live leader of an agent's home squad, for the review visibility rule.
SELECT s.leader_id FROM agent a
JOIN squad s ON s.id = a.home_squad_id
WHERE a.id = @agent_id AND a.workspace_id = @workspace_id AND s.archived_at IS NULL;

-- name: ListSquadsLedByAgent :many
SELECT id FROM squad
WHERE workspace_id = @workspace_id AND leader_id = @agent_id AND archived_at IS NULL;

-- name: ListAgentsInSquads :many
-- Agents whose home squad is one of the given squads.
SELECT id FROM agent
WHERE workspace_id = @workspace_id AND home_squad_id = ANY(@squad_ids::uuid[]) AND archived_at IS NULL;
