-- name: LockProposalWorkspace :exec
-- Serializes proposal creation within one workspace so the attention limits
-- (counted below) cannot be passed by two concurrent creates. Released at
-- commit. A hash collision across workspaces only costs extra serialization.
SELECT pg_advisory_xact_lock(hashtext('proposal'), hashtext(sqlc.arg('workspace_id')::uuid::text));

-- name: CreateProposal :one
-- expiry_window is the time a pending proposal waits for a decision; the
-- service passes ProposalExpiryAfter so the SQL and the Go constant cannot drift.
INSERT INTO proposal (
    id, workspace_id, squad_id, issue_id, handoff_squad_id, urgent, urgent_reason,
    created_by_type, created_by_id, expires_at, kind, payload
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, now() + sqlc.arg('expiry_window')::interval,
    sqlc.arg('kind')::text, sqlc.arg('payload')::jsonb
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
SET state = $3, decided_by = $4, decided_at = now(), decision_note = $5, snoozed_until = NULL
WHERE id = $1 AND workspace_id = $2 AND state = 'pending'
RETURNING *;

-- name: RequestProposalRevision :one
-- "Ask for changes": the proposal leaves the boss's queue until the squad
-- resubmits. The note is kept in decision_note while it is open.
UPDATE proposal
SET state = 'revising', decided_by = $3, decided_at = now(), decision_note = $4, snoozed_until = NULL
WHERE id = $1 AND workspace_id = $2 AND state = 'pending'
RETURNING *;

-- name: ResubmitProposal :one
-- The squad's revised body goes back to the boss: revision increments and the
-- decision fields clear. The decision window starts again, because the boss has
-- not yet seen this body. Creation time is untouched, so a revision does not
-- count as a new proposal in the weekly cap.
UPDATE proposal
SET state = 'pending', revision = revision + 1, decided_by = NULL, decided_at = NULL,
    decision_note = '', expires_at = now() + sqlc.arg('expiry_window')::interval
WHERE id = $1 AND workspace_id = $2 AND state = 'revising'
RETURNING *;

-- name: SnoozeProposal :one
-- "Decide later": the proposal stays pending and counts towards the caps. The
-- window does not run out while the boss has put the proposal aside: expires_at
-- moves to a full window after the snooze ends.
UPDATE proposal
SET snoozed_until = sqlc.arg('snoozed_until')::timestamptz,
    expires_at = GREATEST(expires_at, sqlc.arg('snoozed_until')::timestamptz + sqlc.arg('expiry_window')::interval)
WHERE id = $1 AND workspace_id = $2 AND state = 'pending'
RETURNING *;

-- name: ListDueProposals :many
-- Pending proposals whose decision window has passed, for the daily expiry job.
SELECT id, workspace_id FROM proposal
WHERE state = 'pending' AND expires_at <= now()
ORDER BY expires_at, id
LIMIT $1;

-- name: ExpireProposal :one
-- The predicate is repeated from ListDueProposals: a decision, snooze or
-- revision that took the row first leaves nothing to expire.
UPDATE proposal
SET state = 'expired', decided_at = now(), decision_note = '', snoozed_until = NULL
WHERE id = $1 AND workspace_id = $2 AND state = 'pending' AND expires_at <= now()
RETURNING *;

-- name: ListRecentProposalsForPriorArt :many
-- Proposals of the workspace filed since the given time, whatever their state,
-- with the title of each backing issue. The service scores titles in Go.
SELECT p.id, p.squad_id, p.state, p.created_at, i.title, i.number
FROM proposal p
JOIN issue i ON i.id = p.issue_id AND i.workspace_id = p.workspace_id
WHERE p.workspace_id = $1 AND p.created_at >= $2
ORDER BY p.created_at DESC, p.id DESC
LIMIT 500;

-- name: RevertProposalDecision :one
-- Compensation when the approval's kickoff issue could not be created after
-- the decision committed.
UPDATE proposal
SET state = 'pending', decided_by = NULL, decided_at = NULL, decision_note = ''
WHERE id = $1 AND workspace_id = $2 AND state <> 'pending'
RETURNING *;

-- name: HasActiveSquadAutopilot :one
-- Whether the squad is the assignee of an active autopilot. Squads that work
-- on a schedule are the ones whose leaders are briefed to propose to the boss.
SELECT EXISTS(
    SELECT 1 FROM autopilot
    WHERE workspace_id = $1 AND assignee_type = 'squad' AND assignee_id = $2 AND status = 'active'
) AS has_active;

-- name: GetOldestWorkspaceOwnerMember :one
-- The boss: the longest-standing owner of the workspace.
SELECT * FROM member
WHERE workspace_id = $1 AND role = 'owner'
ORDER BY created_at ASC, id ASC
LIMIT 1;

-- name: DeleteWorkspaceProposals :exec
-- Workspace teardown: proposals have no foreign keys to cascade.
DELETE FROM proposal WHERE proposal.workspace_id = $1;
