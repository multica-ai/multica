-- Structured issue relations (blocks / related). One canonical directed row per
-- edge; the "blocked by" view is derived by the reader from rows where the issue
-- is the target. See migration 196 for the table rationale.
--
-- The table has no foreign keys (repo rule): both issues are validated in the
-- workspace-guarded WHERE EXISTS clauses below, and rows are cleaned up in the
-- same transaction as issue deletion. Every query filters by workspace_id.

-- name: LockWorkspaceRelations :exec
-- Serialize relation mutations within a workspace so two concurrent inserts
-- cannot both pass their cycle check and then form a cycle (e.g. A->B and B->A
-- racing), and so an add cannot race an issue/workspace delete into an orphan.
-- Every path that mutates issue_relation (add, issue delete, workspace delete)
-- takes this first. Transaction-scoped: released automatically on COMMIT/ROLLBACK.
-- Uses the 64-bit hashtextextended form (matching LockIssueDuplicateKey) with an
-- 'issue_relation:' namespace prefix so it can't collide with other advisory locks.
SELECT pg_advisory_xact_lock(hashtextextended('issue_relation:' || sqlc.arg('workspace_id')::text, 0));

-- name: CreateIssueRelation :one
-- Workspace-guarded INSERT: the WHERE EXISTS clauses guarantee both issues
-- belong to the given workspace, so a caller that forgets handler-level checks
-- still cannot link issues across workspaces (mirrors AttachLabelToIssue). ON
-- CONFLICT DO NOTHING makes a duplicate insert a no-op that returns no row; the
-- handler pre-checks for duplicates and treats a no-row result as a lost race.
INSERT INTO issue_relation (
    workspace_id, source_issue_id, target_issue_id, type, created_by_type, created_by_id
)
SELECT
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('source_issue_id')::uuid,
    sqlc.arg('target_issue_id')::uuid,
    sqlc.arg('type')::text,
    sqlc.narg('created_by_type')::text,
    sqlc.narg('created_by_id')::uuid
WHERE EXISTS (
    SELECT 1 FROM issue i
    WHERE i.id = sqlc.arg('source_issue_id')::uuid
      AND i.workspace_id = sqlc.arg('workspace_id')::uuid
)
AND EXISTS (
    SELECT 1 FROM issue i
    WHERE i.id = sqlc.arg('target_issue_id')::uuid
      AND i.workspace_id = sqlc.arg('workspace_id')::uuid
)
ON CONFLICT (workspace_id, source_issue_id, type, target_issue_id) DO NOTHING
RETURNING *;

-- name: GetIssueRelationEdge :one
-- Duplicate pre-check for a specific directed edge. Returns the existing row so
-- the handler can 409 with its id instead of silently swallowing the request.
SELECT * FROM issue_relation
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND source_issue_id = sqlc.arg('source_issue_id')::uuid
  AND target_issue_id = sqlc.arg('target_issue_id')::uuid
  AND type = sqlc.arg('type')::text;

-- name: ListIssueRelations :many
-- Every edge touching one issue, in either direction. The handler derives the
-- per-issue "blocks" vs "blocked_by" label from source/target.
SELECT * FROM issue_relation
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND (source_issue_id = sqlc.arg('issue_id')::uuid
       OR target_issue_id = sqlc.arg('issue_id')::uuid)
ORDER BY created_at ASC, id ASC;

-- name: ListIssueRelationsForIssues :many
-- Bulk variant: edges touching any issue in the set, one round-trip, so list
-- endpoints can fold relations into rows without N+1 (mirrors ListLabelsForIssues).
SELECT * FROM issue_relation
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND (source_issue_id = ANY(sqlc.arg('issue_ids')::uuid[])
       OR target_issue_id = ANY(sqlc.arg('issue_ids')::uuid[]))
ORDER BY created_at ASC, id ASC;

-- name: IssueBlocksReachable :one
-- Reachability over 'blocks' edges within one workspace: is to_issue_id
-- reachable from from_issue_id by following source -> target edges? Adding
-- "A blocks B" (source=A, target=B) closes a cycle iff A is already reachable
-- from B, so the handler calls this with from_issue_id = B (the new target) and
-- to_issue_id = A (the new source). workspace_id filters both the anchor and the
-- recursive term so traversal can never cross workspaces.
WITH RECURSIVE reachable AS (
    SELECT target_issue_id AS issue_id
    FROM issue_relation
    WHERE workspace_id = sqlc.arg('workspace_id')::uuid
      AND type = 'blocks'
      AND source_issue_id = sqlc.arg('from_issue_id')::uuid
    UNION
    SELECT r.target_issue_id
    FROM issue_relation r
    JOIN reachable ON r.source_issue_id = reachable.issue_id
    WHERE r.workspace_id = sqlc.arg('workspace_id')::uuid
      AND r.type = 'blocks'
)
SELECT EXISTS (
    SELECT 1 FROM reachable WHERE issue_id = sqlc.arg('to_issue_id')::uuid
) AS reachable;

-- name: DeleteIssueRelation :one
-- Delete one relation by id, scoped to the workspace AND required to touch the
-- issue in the request path (source or target) so a mismatched issue/relation
-- pair 404s instead of removing an unrelated edge. RETURNING the endpoints lets
-- the handler broadcast to both issues; RETURNING at all distinguishes
-- pgx.ErrNoRows (-> 404) from infrastructure errors (-> 500) without a TOCTOU
-- precheck.
DELETE FROM issue_relation
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND (source_issue_id = sqlc.arg('issue_id')::uuid
       OR target_issue_id = sqlc.arg('issue_id')::uuid)
RETURNING id, source_issue_id, target_issue_id;

-- name: DeleteIssueRelationsForIssue :many
-- Cleanup for issue deletion. The table has no FK cascade, so callers run this
-- in the same transaction as DeleteIssue. Matches both directions and RETURNs
-- the removed edges so the caller can notify the surviving counterpart issues
-- (their relation caches would otherwise go stale).
DELETE FROM issue_relation
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND (source_issue_id = sqlc.arg('issue_id')::uuid
       OR target_issue_id = sqlc.arg('issue_id')::uuid)
RETURNING source_issue_id, target_issue_id;
