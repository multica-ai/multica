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

-- --- Context lint (FIR-1775 Phase 3) ---

-- ListAgentSkillsForContextLint returns the skills currently bound to the
-- agent with their content, so the lint can detect rules duplicated between
-- the instructions and a bound skill (or between two bound skills).
-- name: ListAgentSkillsForContextLint :many
SELECT s.id, s.name, s.content
FROM agent_skill ask
JOIN skill s ON s.id = ask.skill_id
WHERE ask.agent_id = $1
ORDER BY s.name;

-- ListWorkspaceSkillsForContextLint returns every skill in the workspace so
-- skill-name references in instructions can be resolved (dead vs unbound).
-- name: ListWorkspaceSkillsForContextLint :many
SELECT id, name, content FROM skill
WHERE workspace_id = $1
ORDER BY name;

-- ListAgentsForContextLint returns the non-archived agents of a workspace for
-- the workspace-wide lint sweep and for the repo-file drift lint's harness set.
-- name: ListAgentsForContextLint :many
SELECT * FROM agent
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY name;

-- GetWorkspaceReposForContextLint returns the workspace-level repos JSONB
-- (array of {url, description}) used to resolve repo links in instructions.
-- name: GetWorkspaceReposForContextLint :one
SELECT repos FROM workspace
WHERE id = $1;

-- ListWorkspaceGithubRepoRefsForContextLint returns the project-bound
-- github_repo resource refs ({url: ...}) of a workspace; together with the
-- workspace repos these form the known-repo set for the stale-link check.
-- name: ListWorkspaceGithubRepoRefsForContextLint :many
SELECT resource_ref FROM project_resource
WHERE workspace_id = $1 AND resource_type = 'github_repo';

-- --- Context observability (FIR-1775 Phase 4) ---
-- Read-only aggregates that answer "how often does an agent change, who
-- approves, where does drift concentrate". Drift itself is computed in memory
-- by the lint (reused by the observability handler); these queries cover the
-- change-frequency and approver dimensions.

-- AgentContextVersionStats returns the change-frequency stats for one agent:
-- total versions, versions cut in the last 30 days, and when it last changed.
-- name: AgentContextVersionStats :one
SELECT
    COUNT(*)::bigint AS version_count,
    COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '30 days')::bigint AS versions_last_30d,
    MAX(created_at)::timestamptz AS last_changed_at
FROM agent_context_version
WHERE agent_id = $1;

-- ListAgentContextVersionStatsByWorkspace returns one row per non-archived
-- agent with its change-frequency stats, ordered so the most-churned agents
-- surface first in the workspace overview.
-- name: ListAgentContextVersionStatsByWorkspace :many
SELECT
    a.id AS agent_id,
    a.name AS agent_name,
    a.context_version,
    COUNT(v.id)::bigint AS version_count,
    COUNT(v.id) FILTER (WHERE v.created_at >= NOW() - INTERVAL '30 days')::bigint AS versions_last_30d,
    MAX(v.created_at)::timestamptz AS last_changed_at
FROM agent a
LEFT JOIN agent_context_version v ON v.agent_id = a.id
WHERE a.workspace_id = $1 AND a.archived_at IS NULL
GROUP BY a.id, a.name, a.context_version
ORDER BY versions_last_30d DESC, version_count DESC, a.name;

-- ListAgentChangeRequestStats returns one agent's change-request counts grouped
-- by status (pending/approved/rejected/merged).
-- name: ListAgentChangeRequestStats :many
SELECT status, COUNT(*)::bigint AS count
FROM agent_change_request
WHERE agent_id = $1
GROUP BY status;

-- ListAgentChangeRequestStatsByWorkspace returns change-request counts grouped
-- by agent and status across every non-archived agent in the workspace.
-- name: ListAgentChangeRequestStatsByWorkspace :many
SELECT cr.agent_id, cr.status, COUNT(*)::bigint AS count
FROM agent_change_request cr
JOIN agent a ON a.id = cr.agent_id
WHERE a.workspace_id = $1 AND a.archived_at IS NULL
GROUP BY cr.agent_id, cr.status;

-- ListAgentContextApproverStats returns who reviewed one agent's change
-- requests and how many they approved/merged/rejected. The reviewer name
-- resolves inline so the overview never shows a bare UUID.
-- name: ListAgentContextApproverStats :many
SELECT
    cr.reviewed_by AS user_id,
    COALESCE(u.name, '')::text AS name,
    cr.status,
    COUNT(*)::bigint AS count
FROM agent_change_request cr
LEFT JOIN "user" u ON u.id = cr.reviewed_by
WHERE cr.agent_id = $1
  AND cr.reviewed_by IS NOT NULL
  AND cr.status IN ('approved', 'rejected', 'merged')
GROUP BY cr.reviewed_by, u.name, cr.status;

-- ListAgentContextApproverStatsByWorkspace is the workspace-wide approver
-- leaderboard: who reviews agent-context change requests across all agents.
-- name: ListAgentContextApproverStatsByWorkspace :many
SELECT
    cr.reviewed_by AS user_id,
    COALESCE(u.name, '')::text AS name,
    cr.status,
    COUNT(*)::bigint AS count
FROM agent_change_request cr
JOIN agent a ON a.id = cr.agent_id
LEFT JOIN "user" u ON u.id = cr.reviewed_by
WHERE a.workspace_id = $1 AND a.archived_at IS NULL
  AND cr.reviewed_by IS NOT NULL
  AND cr.status IN ('approved', 'rejected', 'merged')
GROUP BY cr.reviewed_by, u.name, cr.status;
