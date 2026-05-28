-- name: CreateDAGEvent :one
INSERT INTO dag_event (
    workspace_id,
    record_ids,
    agent_id,
    dvt,
    operation,
    payload,
    reason
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: GetDAGEvent :one
SELECT * FROM dag_event
WHERE id = $1 AND workspace_id = $2;

-- name: ListDAGEventsForRecord :many
SELECT * FROM dag_event
WHERE workspace_id = $1
  AND sqlc.arg('record_id')::text = ANY(record_ids)
ORDER BY created_at ASC, id ASC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: UpsertDAGRecordProjection :one
INSERT INTO dag_record_projection (
    workspace_id,
    id,
    type,
    created_event_id,
    tombstoned_event_id
) VALUES (
    $1, $2, $3, $4, $5
) ON CONFLICT (workspace_id, id) DO UPDATE SET
    type = EXCLUDED.type,
    tombstoned_event_id = EXCLUDED.tombstoned_event_id,
    updated_at = now()
RETURNING *;

-- name: UpsertDAGLinkProjection :one
INSERT INTO dag_link_projection (
    workspace_id,
    from_id,
    to_id,
    type,
    event_id,
    active
) VALUES (
    $1, $2, $3, $4, $5, $6
) ON CONFLICT (workspace_id, from_id, to_id, type) DO UPDATE SET
    event_id = EXCLUDED.event_id,
    active = EXCLUDED.active,
    updated_at = now()
RETURNING *;

-- name: CreateDAGFactProjection :one
INSERT INTO dag_fact_projection (
    workspace_id,
    predicate,
    args,
    event_id,
    grounded_by,
    confidence
) VALUES (
    $1, $2, $3, $4, $5, $6
) RETURNING *;

-- name: ListDAGLinksFromRecord :many
SELECT * FROM dag_link_projection
WHERE workspace_id = $1 AND from_id = $2 AND active = TRUE
ORDER BY type ASC, to_id ASC;

-- name: ListDAGFactsByPredicate :many
SELECT * FROM dag_fact_projection
WHERE workspace_id = $1 AND predicate = $2
ORDER BY predicate ASC, args ASC, event_id ASC, id ASC;
