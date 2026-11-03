-- Initiatives, their stage timeline and their artifact checklists (spec 3.1).
-- Every query filters by workspace_id.

-- name: LockInitiativeWorkspace :exec
-- Serializes initiative creation within one workspace, so seq is unique under
-- concurrent creates (the unique index is the backstop). Released at commit.
SELECT pg_advisory_xact_lock(hashtext('initiative'), hashtext(sqlc.arg('workspace_id')::uuid::text));

-- name: NextInitiativeSeq :one
SELECT (COALESCE(MAX(seq), 0) + 1)::integer AS next_seq FROM initiative
WHERE workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: CreateInitiative :one
INSERT INTO initiative (
    id, workspace_id, pipeline_id, seq, title, thesis, stage_key, owner_squad_id,
    status, issue_id, source_proposal_id, sponsor_user_id
) VALUES (
    sqlc.arg('id')::uuid, sqlc.arg('workspace_id')::uuid, sqlc.arg('pipeline_id')::uuid,
    sqlc.arg('seq')::integer, sqlc.arg('title')::text, sqlc.arg('thesis')::text,
    sqlc.arg('stage_key')::text, sqlc.narg('owner_squad_id')::uuid,
    'active', sqlc.arg('issue_id')::uuid, sqlc.narg('source_proposal_id')::uuid,
    sqlc.narg('sponsor_user_id')::uuid
)
RETURNING *;

-- name: GetInitiativeInWorkspace :one
SELECT * FROM initiative
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: LockInitiativeInWorkspace :one
-- Row lock for a transition, so two concurrent moves cannot both read the same
-- stage.
SELECT * FROM initiative
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid
FOR UPDATE;

-- name: ListInitiatives :many
-- Newest first. Each filter is optional.
SELECT * FROM initiative
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND (sqlc.narg('pipeline_id')::uuid IS NULL OR pipeline_id = sqlc.narg('pipeline_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('owner_squad_id')::uuid IS NULL OR owner_squad_id = sqlc.narg('owner_squad_id')::uuid)
ORDER BY seq DESC
LIMIT 500;

-- name: ListOperatingInitiatives :many
-- The gate job reads every initiative in its operate stage, across workspaces.
SELECT * FROM initiative
WHERE status = 'operating' AND operating_since IS NOT NULL
ORDER BY workspace_id, seq
LIMIT sqlc.arg('row_limit')::integer;

-- name: UpdateInitiativeState :one
-- Writes the mutable state of an initiative. The service loads the row under
-- LockInitiativeInWorkspace, changes what it needs and writes every field back.
UPDATE initiative SET
    stage_key = sqlc.arg('stage_key')::text,
    owner_squad_id = sqlc.narg('owner_squad_id')::uuid,
    status = sqlc.arg('status')::text,
    stage_entered_at = sqlc.arg('stage_entered_at')::timestamptz,
    shelved_at = sqlc.narg('shelved_at')::timestamptz,
    operating_since = sqlc.narg('operating_since')::timestamptz,
    build_project_id = sqlc.narg('build_project_id')::uuid,
    closed_at = sqlc.narg('closed_at')::timestamptz,
    closed_reason = sqlc.arg('closed_reason')::text
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;

-- name: CreateInitiativeStageEvent :one
INSERT INTO initiative_stage_event (
    workspace_id, initiative_id, from_stage, to_stage, kind, actor_type, actor_id,
    gate_proposal_id, note
) VALUES (
    sqlc.arg('workspace_id')::uuid, sqlc.arg('initiative_id')::uuid,
    sqlc.narg('from_stage')::text, sqlc.narg('to_stage')::text, sqlc.arg('kind')::text,
    sqlc.arg('actor_type')::text, sqlc.narg('actor_id')::uuid,
    sqlc.narg('gate_proposal_id')::uuid, sqlc.arg('note')::text
)
RETURNING *;

-- name: ListInitiativeStageEvents :many
SELECT * FROM initiative_stage_event
WHERE workspace_id = sqlc.arg('workspace_id')::uuid AND initiative_id = sqlc.arg('initiative_id')::uuid
ORDER BY created_at, id;

-- name: LatestInitiativeEventOfKind :one
SELECT * FROM initiative_stage_event
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND initiative_id = sqlc.arg('initiative_id')::uuid
  AND kind = sqlc.arg('kind')::text
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: CreateInitiativeArtifact :exec
-- A checklist row per stage artifact. Re-running a stage entry keeps the rows
-- already there, including their waivers and links.
INSERT INTO initiative_artifact (workspace_id, initiative_id, stage_key, kind, required)
VALUES (
    sqlc.arg('workspace_id')::uuid, sqlc.arg('initiative_id')::uuid,
    sqlc.arg('stage_key')::text, sqlc.arg('kind')::text, sqlc.arg('required')::boolean
)
ON CONFLICT (initiative_id, stage_key, kind) DO NOTHING;

-- name: ListInitiativeArtifacts :many
SELECT * FROM initiative_artifact
WHERE workspace_id = sqlc.arg('workspace_id')::uuid AND initiative_id = sqlc.arg('initiative_id')::uuid
ORDER BY stage_key, kind;

-- name: GetInitiativeArtifact :one
SELECT * FROM initiative_artifact
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND initiative_id = sqlc.arg('initiative_id')::uuid
  AND stage_key = sqlc.arg('stage_key')::text
  AND kind = sqlc.arg('kind')::text;

-- name: WaiveInitiativeArtifact :one
UPDATE initiative_artifact SET waived_by = sqlc.arg('waived_by')::uuid, waived_note = sqlc.arg('waived_note')::text
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND initiative_id = sqlc.arg('initiative_id')::uuid
  AND stage_key = sqlc.arg('stage_key')::text
  AND kind = sqlc.arg('kind')::text
RETURNING *;

-- name: LinkInitiativeArtifact :one
-- A published document with this initiative and kind satisfies the row.
UPDATE initiative_artifact SET knowledge_link_id = sqlc.arg('knowledge_link_id')::uuid
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND initiative_id = sqlc.arg('initiative_id')::uuid
  AND stage_key = sqlc.arg('stage_key')::text
  AND kind = sqlc.arg('kind')::text
RETURNING *;

-- name: ListGateProposalsForInitiative :many
-- Every stage_gate proposal of one initiative, oldest first, with the backing
-- issue's title and number.
SELECT sqlc.embed(p), i.title AS issue_title, i.number AS issue_number, i.status AS issue_status
FROM proposal p
JOIN issue i ON i.id = p.issue_id AND i.workspace_id = p.workspace_id
WHERE p.workspace_id = sqlc.arg('workspace_id')::uuid
  AND p.kind = 'stage_gate'
  AND p.payload->>'initiative_id' = sqlc.arg('initiative_id')::text
ORDER BY p.created_at, p.id;

-- name: ListPendingGateProposalsForWorkspace :many
-- Pending stage_gate proposals of the workspace, with the initiative they name.
SELECT p.id AS proposal_id, (p.payload->>'initiative_id')::text AS initiative_id, p.created_at
FROM proposal p
WHERE p.workspace_id = sqlc.arg('workspace_id')::uuid
  AND p.kind = 'stage_gate'
  AND p.state = 'pending'
ORDER BY p.created_at DESC, p.id DESC;

-- name: DeleteWorkspaceInitiativeArtifacts :exec
DELETE FROM initiative_artifact WHERE workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: DeleteWorkspaceInitiativeStageEvents :exec
DELETE FROM initiative_stage_event WHERE workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: DeleteWorkspaceInitiatives :exec
DELETE FROM initiative WHERE workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: ListActiveInitiatives :many
-- The stage sweep reads every active initiative, across workspaces.
SELECT * FROM initiative
WHERE status = 'active'
ORDER BY workspace_id, seq
LIMIT sqlc.arg('row_limit')::integer;

-- name: MinIssuePositionInStatus :one
-- The top of a status column, for placing an issue the initiative service
-- creates (the issue service's NextTopPosition rule).
SELECT COALESCE(MIN(position), 0)::float8 FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')::uuid AND status = sqlc.arg('status')::text;

-- name: ListInitiativeArtifactsForWorkspace :many
-- The digest reads every checklist row of the workspace in one query.
SELECT * FROM initiative_artifact
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY initiative_id, stage_key, kind;

-- name: UpdateInitiativeGateDays :one
-- The day offsets of the operate review gates. The service checks the range.
UPDATE initiative SET gate_days = sqlc.arg('gate_days')::integer[]
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;

-- name: GetSquadFloorName :one
-- The name of the floor a squad sits on, empty when it sits on none. Opportunity
-- filings are limited to the Research and Product floors (spec 3.2).
SELECT COALESCE(f.name, '')::text AS floor_name
FROM squad s
LEFT JOIN office_floor f ON f.id = s.floor_id AND f.workspace_id = s.workspace_id
WHERE s.id = sqlc.arg('squad_id')::uuid AND s.workspace_id = sqlc.arg('workspace_id')::uuid;
