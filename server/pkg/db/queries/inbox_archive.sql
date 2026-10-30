-- name: ListArchivedInboxPage :many
-- Select narrow group representatives before loading bodies or comment anchors.
WITH newest AS MATERIALIZED (
    SELECT DISTINCT ON (COALESCE(i.issue_id, i.id))
           i.id, i.issue_id, i.created_at, i.read,
           CASE WHEN i.actor_type = 'system' THEN 'system'
                ELSE i.actor_type || ':' || i.actor_id::text END AS actor
    FROM inbox_item i
    WHERE i.workspace_id = sqlc.arg(workspace_id)::uuid
      AND i.recipient_type = 'member'
      AND i.recipient_id = sqlc.arg(recipient_id)::uuid
      AND i.archived = true
      AND (sqlc.narg(group_id)::uuid IS NULL OR i.issue_id = sqlc.narg(group_id)::uuid
           OR (i.issue_id IS NULL AND i.id = sqlc.narg(group_id)::uuid))
      AND (i.issue_id IS NULL OR NOT EXISTS (
          SELECT 1 FROM inbox_item active
          WHERE active.workspace_id = i.workspace_id
            AND active.recipient_type = i.recipient_type
            AND active.recipient_id = i.recipient_id
            AND active.issue_id = i.issue_id AND active.archived = false
      ))
    ORDER BY COALESCE(i.issue_id, i.id), i.created_at DESC, i.id DESC
), projected AS (
    SELECT newest.*, iss.status AS issue_status, iss.priority AS issue_priority
    FROM newest
    LEFT JOIN issue iss ON iss.id = newest.issue_id
      AND iss.workspace_id = sqlc.arg(workspace_id)::uuid
), matched AS (
    SELECT projected.*,
           (cardinality(sqlc.arg(statuses)::text[]) = 0 OR issue_status = ANY(sqlc.arg(statuses)::text[])) AS status_match,
           (cardinality(sqlc.arg(priorities)::text[]) = 0 OR issue_priority = ANY(sqlc.arg(priorities)::text[])) AS priority_match,
           (cardinality(sqlc.arg(actors)::text[]) = 0 OR actor = ANY(sqlc.arg(actors)::text[])) AS actor_match,
           (NOT sqlc.arg(unread_only)::boolean OR NOT read) AS read_match
    FROM projected
)
, selected AS (
    SELECT * FROM matched
    WHERE status_match AND priority_match AND actor_match AND read_match
      AND (sqlc.narg(before_time)::timestamptz IS NULL OR
           (created_at, id) < (sqlc.narg(before_time)::timestamptz, sqlc.narg(before_id)::uuid))
    ORDER BY created_at DESC, id DESC
    LIMIT sqlc.arg(page_limit)::int
)
SELECT sqlc.embed(i), selected.issue_status, selected.issue_priority,
       COALESCE(anchor.comment_id, '')::text AS comment_id
FROM selected
JOIN inbox_item i ON i.id = selected.id
LEFT JOIN LATERAL (
    SELECT a.details->>'comment_id' AS comment_id
    FROM inbox_item a
    WHERE i.issue_id IS NOT NULL AND a.issue_id = i.issue_id
      AND a.workspace_id = i.workspace_id AND a.recipient_type = i.recipient_type
      AND a.recipient_id = i.recipient_id AND a.archived = true
      AND NULLIF(a.details->>'comment_id', '') IS NOT NULL
    ORDER BY a.created_at DESC, a.id DESC LIMIT 1
) anchor ON true
ORDER BY i.created_at DESC, i.id DESC;

-- name: ArchivedInboxFacets :many
-- Select narrow group representatives before loading bodies or comment anchors.
WITH newest AS MATERIALIZED (
    SELECT DISTINCT ON (COALESCE(i.issue_id, i.id))
           i.id, i.issue_id, i.created_at, i.read,
           CASE WHEN i.actor_type = 'system' THEN 'system'
                ELSE i.actor_type || ':' || i.actor_id::text END AS actor
    FROM inbox_item i
    WHERE i.workspace_id = sqlc.arg(workspace_id)::uuid
      AND i.recipient_type = 'member'
      AND i.recipient_id = sqlc.arg(recipient_id)::uuid
      AND i.archived = true
      AND (i.issue_id IS NULL OR NOT EXISTS (
          SELECT 1 FROM inbox_item active
          WHERE active.workspace_id = i.workspace_id
            AND active.recipient_type = i.recipient_type
            AND active.recipient_id = i.recipient_id
            AND active.issue_id = i.issue_id AND active.archived = false
      ))
    ORDER BY COALESCE(i.issue_id, i.id), i.created_at DESC, i.id DESC
), projected AS (
    SELECT newest.*, iss.status AS issue_status, iss.priority AS issue_priority
    FROM newest
    LEFT JOIN issue iss ON iss.id = newest.issue_id
      AND iss.workspace_id = sqlc.arg(workspace_id)::uuid
), matched AS (
    SELECT projected.*,
           (cardinality(sqlc.arg(statuses)::text[]) = 0 OR issue_status = ANY(sqlc.arg(statuses)::text[])) AS status_match,
           (cardinality(sqlc.arg(priorities)::text[]) = 0 OR issue_priority = ANY(sqlc.arg(priorities)::text[])) AS priority_match,
           (cardinality(sqlc.arg(actors)::text[]) = 0 OR actor = ANY(sqlc.arg(actors)::text[])) AS actor_match,
           (NOT sqlc.arg(unread_only)::boolean OR NOT read) AS read_match
    FROM projected
)
SELECT 'statuses'::text AS dimension, issue_status::text AS key,
       count(*) FILTER (WHERE priority_match AND actor_match AND read_match)::bigint AS count
FROM matched WHERE issue_status IS NOT NULL GROUP BY issue_status
UNION ALL
SELECT 'priorities'::text, issue_priority::text,
       count(*) FILTER (WHERE status_match AND actor_match AND read_match)::bigint
FROM matched WHERE issue_priority IS NOT NULL GROUP BY issue_priority
UNION ALL
SELECT 'actors'::text, actor::text,
       count(*) FILTER (WHERE status_match AND priority_match AND read_match)::bigint
FROM matched WHERE actor IS NOT NULL GROUP BY actor
UNION ALL
SELECT 'unread'::text, 'unread'::text,
       count(*) FILTER (WHERE status_match AND priority_match AND actor_match AND NOT read)::bigint
FROM matched;
