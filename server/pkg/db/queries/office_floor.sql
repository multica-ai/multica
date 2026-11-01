-- Floors of the digital company office. Every query filters by workspace_id;
-- there are no foreign keys, so squad seats are validated by the handlers.

-- name: LockOfficeLayout :exec
-- Serializes every change to a workspace's floors and seats for the rest of the
-- transaction: auto-seating on squad create, the atomic seat swap, floor
-- create/reorder/delete. Layout writes are rare, so one lock per workspace is
-- enough to make "lowest free slot", the floor cap and the empty-floor check
-- race-free without a table-wide lock.
SELECT pg_advisory_xact_lock(hashtextextended('office_layout:' || sqlc.arg('workspace_id')::uuid::text, 0));

-- name: ListOfficeFloors :many
SELECT * FROM office_floor
WHERE workspace_id = $1
ORDER BY position ASC, created_at ASC, id ASC;

-- name: GetOfficeFloorInWorkspace :one
SELECT * FROM office_floor WHERE id = $1 AND workspace_id = $2;

-- name: CountOfficeFloors :one
SELECT count(*) FROM office_floor WHERE workspace_id = $1;

-- name: CreateOfficeFloor :one
-- Appends the floor after the last one.
INSERT INTO office_floor (workspace_id, name, position)
VALUES (
    sqlc.arg('workspace_id'),
    sqlc.arg('name'),
    COALESCE((SELECT max(position) + 1 FROM office_floor WHERE workspace_id = sqlc.arg('workspace_id')), 0)
)
RETURNING *;

-- name: RenameOfficeFloor :one
UPDATE office_floor SET name = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: SetOfficeFloorPosition :exec
UPDATE office_floor SET position = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: DeleteOfficeFloor :execrows
DELETE FROM office_floor WHERE id = $1 AND workspace_id = $2;

-- name: CountLiveSquadsOnFloor :one
SELECT count(*) FROM squad
WHERE workspace_id = $1 AND floor_id = $2 AND archived_at IS NULL;

-- name: ListOfficeSeats :many
-- Occupied seats of a workspace, used to find the lowest free slot.
SELECT floor_id, office_slot FROM squad
WHERE workspace_id = $1 AND floor_id IS NOT NULL AND archived_at IS NULL;
