-- Agent Office (FIR-1775): versioning + governance for an agent's full runtime
-- context, mirroring the skill-governance model (skill_version /
-- skill_change_request) but for the agent COMPOSITE: instructions, bound skills,
-- model, thinking_level, mcp_config, custom_args, runtime_config,
-- persona_sandbox, and the NAMES (never values) of custom_env keys.
--
-- Schema lives in 9100_cerebro_agent_context_versioning.{up,down}.sql.

-- --- Agent row reads (context governance columns + composite source) ---

-- name: GetAgentContext :one
SELECT * FROM agent
WHERE id = $1;

-- name: GetAgentContextInWorkspace :one
SELECT * FROM agent
WHERE id = $1 AND workspace_id = $2;

-- ListAgentSkillIDsForContext returns the skill ids currently bound to the
-- agent, in a stable order so two snapshots of the same binding set compare
-- equal byte-for-byte.
-- name: ListAgentSkillIDsForContext :many
SELECT skill_id FROM agent_skill
WHERE agent_id = $1
ORDER BY skill_id;

-- --- Ownership ---

-- name: UpdateAgentContextOwnership :one
UPDATE agent SET
    context_owner_id     = $2,
    context_approver_ids = $3,
    updated_at           = now()
WHERE id = $1
RETURNING *;

-- ApplyAgentContextSnapshot writes a composite snapshot back onto the live agent
-- row and bumps context_version. custom_env VALUES are intentionally untouched —
-- only key names are versioned, never secret values. Skill bindings are replaced
-- separately (DeleteAgentSkillsForContext + InsertAgentSkillForContext) inside
-- the same transaction.
-- name: ApplyAgentContextSnapshot :one
UPDATE agent SET
    instructions    = $2,
    description     = $3,
    model           = $4,
    thinking_level  = $5,
    persona_sandbox = $6,
    mcp_config      = $7,
    custom_args     = $8,
    runtime_config  = $9,
    context_version = $10,
    updated_at      = now()
WHERE id = $1
RETURNING *;

-- name: DeleteAgentSkillsForContext :exec
DELETE FROM agent_skill WHERE agent_id = $1;

-- name: InsertAgentSkillForContext :exec
INSERT INTO agent_skill (agent_id, skill_id)
VALUES ($1, $2)
ON CONFLICT (agent_id, skill_id) DO NOTHING;

-- --- Version snapshots (append-only) ---

-- name: ListAgentContextVersions :many
SELECT * FROM agent_context_version
WHERE agent_id = $1
ORDER BY created_at DESC;

-- name: GetAgentContextVersion :one
SELECT * FROM agent_context_version
WHERE agent_id = $1 AND version = $2;

-- name: CreateAgentContextVersion :one
INSERT INTO agent_context_version (
    agent_id, version, snapshot, description, created_by
)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- GetLatestAgentContextVersion returns the most recent snapshot row for the
-- agent. The direct-edit recorder compares against it to skip edits that did
-- not change any versioned field, and to keep the version pointer monotonic
-- if it ever drifted behind the history.
-- name: GetLatestAgentContextVersion :one
SELECT * FROM agent_context_version
WHERE agent_id = $1
ORDER BY created_at DESC, version DESC
LIMIT 1;

-- BumpAgentContextVersion advances only the version pointer. The direct-edit
-- path has already written the new field values through the generic agent
-- update; unlike ApplyAgentContextSnapshot nothing else may be rewritten here.
-- name: BumpAgentContextVersion :one
UPDATE agent SET
    context_version = $2,
    updated_at      = now()
WHERE id = $1
RETURNING *;

-- --- Change requests ---

-- name: ListAgentChangeRequestsByAgent :many
SELECT * FROM agent_change_request
WHERE agent_id = $1
ORDER BY created_at DESC;

-- name: ListPendingAgentChangeRequestsByWorkspace :many
SELECT cr.*
FROM agent_change_request cr
JOIN agent a ON a.id = cr.agent_id
WHERE a.workspace_id = $1 AND cr.status = 'pending'
ORDER BY cr.created_at DESC;

-- name: GetAgentChangeRequest :one
SELECT * FROM agent_change_request
WHERE id = $1;

-- GetAgentChangeRequestForUpdate locks the row so concurrent reviews on the same
-- change request can't race past the status check.
-- name: GetAgentChangeRequestForUpdate :one
SELECT * FROM agent_change_request
WHERE id = $1
FOR UPDATE;

-- name: CreateAgentChangeRequest :one
INSERT INTO agent_change_request (
    agent_id, title, description, base_version, proposed_version,
    proposed_snapshot, proposed_by, work_session_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ReviewAgentChangeRequest :one
UPDATE agent_change_request SET
    status         = $2,
    reviewed_by    = $3,
    reviewed_at    = now(),
    review_comment = $4,
    updated_at     = now()
WHERE id = $1
RETURNING *;
