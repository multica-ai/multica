-- name: InsertIssueLinkRow :exec
--
-- Inserts ONE direction of a link. Callers must invoke this twice within a
-- single transaction (outgoing on source, incoming on target) sharing the
-- same pair_id so the pair can be deleted with a single statement.
--
-- Caller must pre-validate:
--   * source != target (CHECK does this anyway)
--   * caller is a member of source_workspace_id
--   * for link_type = 'blocks', no transitive cycle (use BlocksReachable)
--   * existing duplicate (use GetIssueLinkByTuple) to surface a 409 instead
--     of bubbling up the UNIQUE violation.
INSERT INTO issue_link (
    pair_id,
    source_issue_id, source_workspace_id,
    target_issue_id, target_workspace_id,
    link_type, direction,
    creator_type, creator_id
) VALUES (
    $1,
    $2, $3,
    $4, $5,
    $6, $7,
    $8, $9
);

-- name: GetIssueLinkByTuple :one
SELECT *
FROM issue_link
WHERE source_issue_id = $1
  AND target_issue_id = $2
  AND link_type = $3
  AND direction = 'outgoing'
LIMIT 1;

-- name: GetIssueLinkByID :one
SELECT *
FROM issue_link
WHERE id = $1;

-- name: DeleteIssueLinkByPair :exec
DELETE FROM issue_link
WHERE pair_id = $1;

-- name: ListLinksForIssue :many
SELECT
    l.id,
    l.pair_id,
    l.link_type,
    l.direction,
    l.creator_type,
    l.creator_id,
    l.created_at,
    l.target_issue_id,
    l.target_workspace_id,
    -- Embedded target snapshot for response payload (avoids N+1 lookups).
    -- identifier is derived as <issue_prefix>-<number>; computed here so the
    -- handler can pass the row straight through without a second lookup.
    (w.issue_prefix || '-' || i.number::text) AS target_identifier,
    i.title       AS target_title,
    i.status      AS target_status,
    i.number      AS target_number,
    w.name        AS target_workspace_name,
    w.slug        AS target_workspace_slug
FROM issue_link l
JOIN issue i     ON i.id = l.target_issue_id
JOIN workspace w ON w.id = l.target_workspace_id
WHERE l.source_issue_id = $1
ORDER BY l.link_type, l.direction, i.number;

-- name: ListLinksForIssues :many
--
-- Bulk variant for DTO enrichment over a list of issues. Returns the same
-- columns as ListLinksForIssue plus the source_issue_id so the handler can
-- group rows back to their owning issue.
SELECT
    l.source_issue_id  AS source_issue_id,
    l.id,
    l.pair_id,
    l.link_type,
    l.direction,
    l.creator_type,
    l.creator_id,
    l.created_at,
    l.target_issue_id,
    l.target_workspace_id,
    (w.issue_prefix || '-' || i.number::text) AS target_identifier,
    i.title       AS target_title,
    i.status      AS target_status,
    i.number      AS target_number,
    w.name        AS target_workspace_name,
    w.slug        AS target_workspace_slug
FROM issue_link l
JOIN issue i     ON i.id = l.target_issue_id
JOIN workspace w ON w.id = l.target_workspace_id
WHERE l.source_issue_id = ANY($1::uuid[])
ORDER BY l.source_issue_id, l.link_type, l.direction, i.number;

-- name: ListBlockersForIssue :many
--
-- "Issues blocking me." From the perspective of issue X, blockers are
-- incoming 'blocks' links: another issue B has an outgoing blocks→X, which
-- mirrors as X having an incoming blocks→B. We pull the target snapshot
-- (which is the blocker) and only return blockers that are not yet closed.
SELECT
    l.target_issue_id        AS blocker_issue_id,
    l.target_workspace_id    AS blocker_workspace_id,
    (w.issue_prefix || '-' || i.number::text) AS blocker_identifier,
    i.title                  AS blocker_title,
    i.status                 AS blocker_status,
    i.number                 AS blocker_number,
    w.name                   AS blocker_workspace_name,
    w.slug                   AS blocker_workspace_slug
FROM issue_link l
JOIN issue i     ON i.id = l.target_issue_id
JOIN workspace w ON w.id = l.target_workspace_id
WHERE l.source_issue_id = $1
  AND l.link_type       = 'blocks'
  AND l.direction       = 'incoming'
  AND i.status NOT IN ('done', 'cancelled', 'duplicate')
ORDER BY i.number;

-- name: BlocksReachable :one
--
-- Cycle check for 'blocks' links. Given a candidate (source -> target,
-- link_type='blocks'), we must reject if the target already transitively
-- blocks the source. We walk outgoing 'blocks' edges starting at the target
-- and look for source.
--
-- Returns 1 if a path exists (i.e. cycle would be created), 0 otherwise.
WITH RECURSIVE walk(issue_id, depth) AS (
    SELECT $1::uuid, 0  -- start at target
  UNION
    SELECT l.target_issue_id, w.depth + 1
    FROM issue_link l
    JOIN walk w ON w.issue_id = l.source_issue_id
    WHERE l.link_type = 'blocks'
      AND l.direction = 'outgoing'
      AND w.depth < 32  -- safety cap
)
SELECT CASE WHEN EXISTS (
    SELECT 1 FROM walk WHERE issue_id = $2::uuid
) THEN 1 ELSE 0 END AS hit;

-- name: CountLinksForIssue :one
SELECT COUNT(*) AS count
FROM issue_link
WHERE source_issue_id = $1;
