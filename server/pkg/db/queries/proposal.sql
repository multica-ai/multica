-- name: LockProposalWorkspace :exec
-- Serializes proposal creation within one workspace so the attention limits
-- (counted below) cannot be passed by two concurrent creates. Released at
-- commit. A hash collision across workspaces only costs extra serialization.
SELECT pg_advisory_xact_lock(hashtext('proposal'), hashtext(sqlc.arg('workspace_id')::uuid::text));

-- name: CreateProposal :one
INSERT INTO proposal (
    id, workspace_id, squad_id, issue_id, handoff_squad_id, urgent, urgent_reason,
    created_by_type, created_by_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: GetProposalInWorkspace :one
SELECT * FROM proposal WHERE id = $1 AND workspace_id = $2;

-- name: GetProposalWithIssue :one
SELECT sqlc.embed(p), i.title AS issue_title, i.number AS issue_number, i.status AS issue_status
FROM proposal p
JOIN issue i ON i.id = p.issue_id AND i.workspace_id = p.workspace_id
WHERE p.id = $1 AND p.workspace_id = $2;

-- name: LockProposalInWorkspace :one
-- Row lock for a decision, so two concurrent decisions cannot both see
-- state='pending'.
SELECT * FROM proposal WHERE id = $1 AND workspace_id = $2 FOR UPDATE;

-- name: ListProposals :many
-- Newest first. state and squad_id are optional filters.
SELECT sqlc.embed(p), i.title AS issue_title, i.number AS issue_number, i.status AS issue_status
FROM proposal p
JOIN issue i ON i.id = p.issue_id AND i.workspace_id = p.workspace_id
WHERE p.workspace_id = sqlc.arg('workspace_id')
  AND (sqlc.narg('state')::text IS NULL OR p.state = sqlc.narg('state')::text)
  AND (sqlc.narg('squad_id')::uuid IS NULL OR p.squad_id = sqlc.narg('squad_id')::uuid)
ORDER BY p.created_at DESC, p.id DESC
LIMIT 200;

-- name: CountPendingProposalsBySquad :one
SELECT count(*) FROM proposal
WHERE workspace_id = $1 AND squad_id = $2 AND state = 'pending';

-- name: CountPendingProposalsByWorkspace :one
SELECT count(*) FROM proposal
WHERE workspace_id = $1 AND state = 'pending';

-- name: CountRecentProposalsBySquad :one
-- Proposals created by the squad over the rolling seven days, whatever their
-- state: a decided proposal still used the boss's attention.
SELECT count(*) FROM proposal
WHERE workspace_id = $1 AND squad_id = $2
  AND created_at > now() - interval '7 days';

-- name: CountRecentUrgentProposalsBySquad :one
SELECT count(*) FROM proposal
WHERE workspace_id = $1 AND squad_id = $2 AND urgent
  AND created_at > now() - interval '7 days';

-- name: DecideProposal :one
UPDATE proposal
SET state = $3, decided_by = $4, decided_at = now(), decision_note = $5
WHERE id = $1 AND workspace_id = $2 AND state = 'pending'
RETURNING *;

-- name: RevertProposalDecision :one
-- Compensation when the approval's kickoff issue could not be created after
-- the decision committed.
UPDATE proposal
SET state = 'pending', decided_by = NULL, decided_at = NULL, decision_note = ''
WHERE id = $1 AND workspace_id = $2 AND state <> 'pending'
RETURNING *;

-- name: GetOldestWorkspaceOwnerMember :one
-- The boss: the longest-standing owner of the workspace.
SELECT * FROM member
WHERE workspace_id = $1 AND role = 'owner'
ORDER BY created_at ASC, id ASC
LIMIT 1;
