-- The body history of a proposal (see migration 709). Every query filters by
-- workspace_id; there are no foreign keys.

-- name: CreateProposalRevision :exec
INSERT INTO proposal_revision (
    id, workspace_id, proposal_id, revision, body, created_by_type, created_by_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
);

-- name: ListProposalRevisions :many
-- Newest first. A proposal filed before the history existed has fewer rows
-- than revisions; nothing is backfilled.
SELECT * FROM proposal_revision
WHERE workspace_id = $1 AND proposal_id = $2
ORDER BY revision DESC;

-- name: DeleteWorkspaceProposalRevisions :exec
-- Workspace teardown: revisions have no foreign keys to cascade.
DELETE FROM proposal_revision WHERE workspace_id = $1;
