-- Issue status catalog (MUL-6243). Each workspace holds the 7 built-in
-- statuses plus any custom ones. A category's value IS its canonical built-in
-- key, so resolving a custom status to its platform behavior is a plain column
-- read — no mapping table, no second concept.

-- name: SeedIssueStatusEntries :exec
-- Idempotent seed of the 7 built-ins. Safe to call concurrently from multiple
-- pods during a rolling deploy: the unique (workspace_id, key) index makes a
-- losing racer a no-op rather than an error. Positions are intra-category, and
-- each built-in is the only member of its category at seed time, so all 0.
INSERT INTO issue_status (workspace_id, key, name, description, category, color, is_system, position)
VALUES
    (sqlc.arg('workspace_id')::uuid, 'backlog', 'Backlog', 'Parked. Assigning an issue here never starts an agent run.', 'backlog', '#6b7280', TRUE, 0),
    (sqlc.arg('workspace_id')::uuid, 'todo', 'Todo', 'Queued for work. Moving an issue here starts the assigned agent.', 'todo', '#6b7280', TRUE, 0),
    (sqlc.arg('workspace_id')::uuid, 'in_progress', 'In Progress', 'Actively being worked on.', 'in_progress', '#f59e0b', TRUE, 0),
    (sqlc.arg('workspace_id')::uuid, 'in_review', 'In Review', 'Work delivered, waiting on human review. Finalizes the autopilot run.', 'in_review', '#22c55e', TRUE, 0),
    (sqlc.arg('workspace_id')::uuid, 'done', 'Done', 'Completed.', 'done', '#3b82f6', TRUE, 0),
    (sqlc.arg('workspace_id')::uuid, 'blocked', 'Blocked', 'Stalled on an external dependency.', 'blocked', '#ef4444', TRUE, 0),
    (sqlc.arg('workspace_id')::uuid, 'cancelled', 'Cancelled', 'Decided not to do.', 'cancelled', '#6b7280', TRUE, 0)
ON CONFLICT DO NOTHING;

-- name: ListIssueStatusEntries :many
-- Ordered by category rank (the historical STATUS_ORDER, so the default board
-- and picker stay pixel-identical for a workspace with no custom statuses),
-- then intra-category position, then key as a stable tiebreak.
SELECT * FROM issue_status
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND (sqlc.arg('include_archived')::bool OR archived_at IS NULL)
ORDER BY
    CASE category
        WHEN 'backlog' THEN 0
        WHEN 'todo' THEN 1
        WHEN 'in_progress' THEN 2
        WHEN 'in_review' THEN 3
        WHEN 'done' THEN 4
        WHEN 'blocked' THEN 5
        WHEN 'cancelled' THEN 6
        ELSE 7
    END,
    position,
    key;

-- name: GetIssueStatusEntryByKey :one
SELECT * FROM issue_status
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND key = sqlc.arg('key')::text;

-- name: GetIssueStatusEntryByID :one
SELECT * FROM issue_status
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: CreateIssueStatusEntry :one
-- Custom statuses only: is_system is never set here, so the canonical-key and
-- non-archivable CHECK constraints can only ever apply to seeded rows.
INSERT INTO issue_status (workspace_id, key, name, description, category, color, position)
VALUES (
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('key')::text,
    sqlc.arg('name')::text,
    sqlc.arg('description')::text,
    sqlc.arg('category')::text,
    sqlc.arg('color')::text,
    COALESCE(
        (SELECT MAX(position) + 1 FROM issue_status
         WHERE workspace_id = sqlc.arg('workspace_id')::uuid
           AND category = sqlc.arg('category')::text),
        0
    )
)
RETURNING *;

-- name: UpdateIssueStatusEntry :one
-- key and category are absent on purpose: both are immutable after create.
-- The is_system guard is what enforces "built-in name and color are locked" at
-- the storage layer, so a handler bug cannot rename a built-in.
UPDATE issue_status SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    color = COALESCE(sqlc.narg('color'), color),
    position = COALESCE(sqlc.narg('position'), position),
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND is_system = FALSE
  AND archived_at IS NULL
RETURNING *;

-- name: ArchiveIssueStatusEntry :one
-- Built-ins are excluded by is_system: archiving one would delete its
-- category's behavior definition.
--
-- Archiving retires a status from future use only. Issues already on it keep
-- it — Effective ignores archived_at, so their behavior is unchanged — while
-- Resolve rejects it, so nothing new can be assigned to it.
UPDATE issue_status SET
    archived_at = now(),
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND is_system = FALSE
  AND archived_at IS NULL
RETURNING *;

-- name: CountIssuesUsingStatusKey :one
-- Reported alongside a status so the UI can say how many issues still carry an
-- archived one. NOT an archive precondition: archiving never requires migrating
-- issues off the status.
SELECT COUNT(*)::bigint FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND status = sqlc.arg('key')::text;

-- name: DeleteIssueStatusEntriesForWorkspace :exec
-- No foreign keys by project rule, so workspace teardown cleans up here.
DELETE FROM issue_status WHERE workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: LockIssueStatusCatalog :exec
-- EXCLUSIVE transaction lock over a workspace's status catalog, held by archive
-- so no issue can be written onto a status between the in-use census and the
-- archive itself. pg_advisory_xact_lock (not pg_try_) so a concurrent archive
-- queues instead of failing.
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg('workspace_id')::uuid::text || ':issue_status', 0));

-- name: LockIssueStatusCatalogShared :exec
-- SHARED counterpart, taken by a write that puts an issue ON a custom status.
-- Shared holders do not block each other, so concurrent issue writes keep their
-- current throughput; they only block against archive's exclusive lock.
--
-- Built-in statuses deliberately do NOT take this lock: a built-in can never be
-- archived (enforced by issue_status_system_not_archivable), so there is nothing
-- to race with, and the overwhelmingly common write path stays lock-free.
SELECT pg_advisory_xact_lock_shared(hashtextextended(sqlc.arg('workspace_id')::uuid::text || ':issue_status', 0));

-- name: ListIssueStatusKeysByCategories :many
-- Expands a set of categories to the status keys that belong to them, so a
-- category filter can be applied as an INDEXED `status = ANY(...)` predicate
-- instead of wrapping the column in issue_effective_status(), which makes
-- (workspace_id, status) unusable and forces a full workspace scan.
--
-- ARCHIVED statuses are included on purpose: archiving retires a status from
-- future assignment but leaves existing issues on it, and those issues must
-- still appear in their category's column.
SELECT key FROM issue_status
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND category = ANY(sqlc.arg('categories')::text[]);
