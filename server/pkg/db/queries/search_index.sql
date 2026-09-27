-- Local search index sync (MUL-7754). Web and Desktop copy the issue, comment,
-- and project text of a workspace, then catch up through search_index_change.
-- Snapshots are exchanged as pg_snapshot text; see migration 561.

-- name: GetSearchIndexManifest :one
-- Taken before any snapshot page, so every write the pages might miss is newer
-- than this snapshot and reaches the client through the change log. The byte
-- totals let the client reject a workspace too large for its memory budget
-- before downloading it; octet_length reads the stored length, so TOASTed
-- bodies are not decompressed.
WITH ws AS (SELECT @workspace_id::uuid AS id)
SELECT
    pg_current_snapshot()::text AS snapshot,
    (SELECT count(*) FROM issue i, ws WHERE i.workspace_id = ws.id)::bigint AS issue_count,
    (SELECT COALESCE(sum(octet_length(i.title) + COALESCE(octet_length(i.description), 0)), 0)
        FROM issue i, ws WHERE i.workspace_id = ws.id)::bigint AS issue_bytes,
    (SELECT count(*) FROM comment c, ws
        WHERE c.workspace_id = ws.id AND c.deleted_at IS NULL)::bigint AS comment_count,
    (SELECT COALESCE(sum(octet_length(c.content)), 0) FROM comment c, ws
        WHERE c.workspace_id = ws.id AND c.deleted_at IS NULL)::bigint AS comment_bytes,
    (SELECT count(*) FROM project p, ws WHERE p.workspace_id = ws.id)::bigint AS project_count,
    (SELECT COALESCE(sum(octet_length(p.title) + COALESCE(octet_length(p.description), 0)), 0)
        FROM project p, ws WHERE p.workspace_id = ws.id)::bigint AS project_bytes;

-- name: GetSearchIndexCurrentSnapshot :one
SELECT pg_current_snapshot()::text AS snapshot;

-- name: ListSearchIndexIssuesPage :many
-- Pages by the unique (workspace_id, number) index.
SELECT * FROM issue
WHERE workspace_id = @workspace_id AND number > @after_number
ORDER BY number
LIMIT @page_limit;

-- name: ListSearchIndexIssuesByIDs :many
SELECT * FROM issue
WHERE workspace_id = @workspace_id AND id = ANY(@ids::uuid[]);

-- name: ListSearchIndexCommentsByIssues :many
SELECT id, issue_id, content, created_at
FROM comment
WHERE workspace_id = @workspace_id
  AND issue_id = ANY(@issue_ids::uuid[])
  AND deleted_at IS NULL;

-- name: ListSearchIndexCommentsByIDs :many
-- Tombstoned comments are returned so the caller can report them as deleted.
SELECT id, issue_id, content, created_at, deleted_at
FROM comment
WHERE workspace_id = @workspace_id AND id = ANY(@ids::uuid[]);

-- name: ListSearchIndexProjects :many
SELECT * FROM project
WHERE workspace_id = @workspace_id
ORDER BY id;

-- name: ListSearchIndexProjectsByIDs :many
SELECT * FROM project
WHERE workspace_id = @workspace_id AND id = ANY(@ids::uuid[]);

-- name: SearchIndexSinceIsPruned :one
-- True when retention pruning may have removed a change the client has not
-- seen yet: a pruned xid at or above the snapshot's xmin could be one it has
-- not consumed.
SELECT EXISTS (
    SELECT 1 FROM search_index_prune_mark
    WHERE singleton
      AND pruned_through_xid >= pg_snapshot_xmin((@since_snapshot::text)::pg_snapshot)
) AS pruned;

-- name: ListSearchIndexChanges :many
-- Changes committed in the target snapshot but not in the since snapshot, in
-- keyset order. An entity rewritten after the target snapshot carries a newer
-- xid, drops out of this range, and is returned by the next catch-up instead.
SELECT entity_type, entity_id, change_xid::text AS change_xid
FROM search_index_change
WHERE workspace_id = @workspace_id
  AND change_xid >= pg_snapshot_xmin((@since_snapshot::text)::pg_snapshot)
  AND change_xid < pg_snapshot_xmax((@target_snapshot::text)::pg_snapshot)
  AND NOT pg_visible_in_snapshot(change_xid, (@since_snapshot::text)::pg_snapshot)
  AND pg_visible_in_snapshot(change_xid, (@target_snapshot::text)::pg_snapshot)
  AND (change_xid, entity_type, entity_id)
      > ((@after_xid::text)::xid8, @after_type::text, @after_id::uuid)
ORDER BY change_xid, entity_type, entity_id
LIMIT @page_limit;

-- name: PruneSearchIndexChanges :one
-- Deletes one bounded batch of rows older than the cutoff and raises the prune
-- mark to the highest xid removed, in the same statement.
WITH doomed AS (
    SELECT sic.entity_type, sic.entity_id
    FROM search_index_change sic
    WHERE sic.changed_at < @cutoff
    ORDER BY sic.changed_at
    LIMIT @batch_size
    FOR UPDATE SKIP LOCKED
), deleted AS (
    DELETE FROM search_index_change c
    USING doomed d
    WHERE c.entity_type = d.entity_type
      AND c.entity_id = d.entity_id
      AND c.changed_at < @cutoff
    RETURNING c.change_xid
), marked AS (
    INSERT INTO search_index_prune_mark (singleton, pruned_through_xid)
    SELECT TRUE, deleted.change_xid FROM deleted ORDER BY deleted.change_xid DESC LIMIT 1
    ON CONFLICT (singleton) DO UPDATE
        SET pruned_through_xid = GREATEST(search_index_prune_mark.pruned_through_xid, EXCLUDED.pruned_through_xid)
    RETURNING 1
)
SELECT count(*)::bigint AS deleted_count FROM deleted;

-- name: DeleteWorkspaceSearchIndexChanges :exec
DELETE FROM search_index_change WHERE workspace_id = $1;
