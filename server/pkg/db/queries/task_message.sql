-- name: CreateTaskMessage :one
INSERT INTO task_message (task_id, seq, type, tool, content, input, output)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListTaskMessages :many
SELECT * FROM task_message
WHERE task_id = $1
ORDER BY seq ASC;

-- name: ListTaskMessagesSince :many
SELECT * FROM task_message
WHERE task_id = $1 AND seq > $2
ORDER BY seq ASC;

-- name: DeleteTaskMessages :exec
DELETE FROM task_message
WHERE task_id = $1;

-- Execution Log pagination (MUL-5122). The stable order is (seq, id): seq is
-- the daemon's intended order but is not unique across a fresh-session retry, so
-- id breaks ties to give a deterministic total order. The full-Run OR filter:
-- a row matches when its type is a selected type OR its tool is a selected tool;
-- empty selections match everything. Callers pass non-null (possibly empty)
-- text[] so cardinality(...) = 0 is the "no filter" test.

-- name: ListTaskMessagesPage :many
-- Newest-first window. With no before-cursor it opens at the most recent events;
-- a before-cursor loads older history. Fetch limit+1 to detect more history.
SELECT * FROM task_message
WHERE task_id = sqlc.arg('task_id')
  AND (
    sqlc.narg('before_seq')::int IS NULL
    OR (seq, id) < (sqlc.narg('before_seq')::int, sqlc.narg('before_id')::uuid)
  )
  AND (
    (cardinality(sqlc.arg('types')::text[]) = 0 AND cardinality(sqlc.arg('tools')::text[]) = 0)
    OR type = ANY(sqlc.arg('types')::text[])
    OR (tool IS NOT NULL AND tool = ANY(sqlc.arg('tools')::text[]))
  )
ORDER BY seq DESC, id DESC
LIMIT sqlc.arg('lim');

-- name: ListTaskMessagesAfter :many
-- Bounded terminal catch-up: events strictly after the client's newest known
-- (seq, id). Ascending, so the page is already chronological.
SELECT * FROM task_message
WHERE task_id = sqlc.arg('task_id')
  AND (seq, id) > (sqlc.arg('after_seq')::int, sqlc.arg('after_id')::uuid)
  AND (
    (cardinality(sqlc.arg('types')::text[]) = 0 AND cardinality(sqlc.arg('tools')::text[]) = 0)
    OR type = ANY(sqlc.arg('types')::text[])
    OR (tool IS NOT NULL AND tool = ANY(sqlc.arg('tools')::text[]))
  )
ORDER BY seq ASC, id ASC
LIMIT sqlc.arg('lim');

-- name: CountTaskMessages :one
-- Raw total: every persisted event for the Run, ignoring filters.
SELECT COUNT(*) FROM task_message WHERE task_id = $1;

-- name: CountTaskMessagesMatched :one
-- Matched total: events satisfying the full-Run OR filter (equals the raw total
-- when no filter is selected).
SELECT COUNT(*) FROM task_message
WHERE task_id = sqlc.arg('task_id')
  AND (
    (cardinality(sqlc.arg('types')::text[]) = 0 AND cardinality(sqlc.arg('tools')::text[]) = 0)
    OR type = ANY(sqlc.arg('types')::text[])
    OR (tool IS NOT NULL AND tool = ANY(sqlc.arg('tools')::text[]))
  );

-- name: TaskMessageTypeFacets :many
-- Full-Run type facets so a type present only in unloaded history is still
-- discoverable and selectable.
SELECT type, COUNT(*) AS count FROM task_message
WHERE task_id = $1
GROUP BY type
ORDER BY count DESC, type ASC;

-- name: TaskMessageToolFacets :many
-- Full-Run tool facets. A tool key covers both its tool_use and tool_result
-- rows because both carry the same tool value.
SELECT tool, COUNT(*) AS count FROM task_message
WHERE task_id = $1 AND tool IS NOT NULL AND tool <> ''
GROUP BY tool
ORDER BY count DESC, tool ASC;
