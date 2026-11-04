-- Agent instruction revisions (spec 4.6). Written only by the agent service's
-- revision hook, in the transaction that changes the instructions.

-- name: InsertAgentInstructionRevision :one
INSERT INTO agent_instruction_revision (
    id, workspace_id, agent_id, revision, instructions, source, proposal_id, actor_type, actor_id
) VALUES (
    @id, @workspace_id, @agent_id, @revision, @instructions, @source, sqlc.narg('proposal_id'), @actor_type, sqlc.narg('actor_id')
)
RETURNING *;

-- name: GetLatestAgentInstructionRevisionNumber :one
SELECT COALESCE(MAX(revision), 0)::int AS revision FROM agent_instruction_revision
WHERE workspace_id = @workspace_id AND agent_id = @agent_id;

-- name: ListAgentInstructionRevisions :many
SELECT * FROM agent_instruction_revision
WHERE workspace_id = @workspace_id AND agent_id = @agent_id
ORDER BY revision DESC
LIMIT @row_limit;

-- name: GetAgentInstructionRevision :one
SELECT * FROM agent_instruction_revision
WHERE workspace_id = @workspace_id AND agent_id = @agent_id AND revision = @revision;

-- name: DeleteWorkspaceAgentInstructionRevisions :exec
-- Workspace teardown: revisions have no foreign keys.
DELETE FROM agent_instruction_revision WHERE workspace_id = @workspace_id;
