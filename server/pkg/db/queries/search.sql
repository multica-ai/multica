-- name: SearchIssues :many
SELECT
  id, workspace_id, number, title, status, priority,
  assignee_type, assignee_id, created_at, updated_at
FROM (
  SELECT id, workspace_id, number, title, status, priority,
         assignee_type, assignee_id, created_at, updated_at,
         ts_rank(search_vector, to_tsquery('simple', sqlc.arg('tsquery')::text)) AS rank,
         1 AS source
  FROM issue
  WHERE workspace_id = sqlc.arg('workspace_id')::uuid
    AND search_vector @@ to_tsquery('simple', sqlc.arg('tsquery')::text)

  UNION ALL

  SELECT id, workspace_id, number, title, status, priority,
         assignee_type, assignee_id, created_at, updated_at,
         0.0 AS rank,
         2 AS source
  FROM issue
  WHERE workspace_id = sqlc.arg('workspace_id')::uuid
    AND title ILIKE '%' || sqlc.arg('ilike_pattern')::text || '%'

  UNION ALL

  SELECT id, workspace_id, number, title, status, priority,
         assignee_type, assignee_id, created_at, updated_at,
         1000.0 AS rank,
         0 AS source
  FROM issue
  WHERE workspace_id = sqlc.arg('workspace_id')::uuid
    AND sqlc.narg('issue_number')::int IS NOT NULL AND number = sqlc.narg('issue_number')::int
) AS combined
GROUP BY id, workspace_id, number, title, status, priority,
         assignee_type, assignee_id, created_at, updated_at
ORDER BY MAX(rank) DESC, created_at DESC
LIMIT sqlc.arg('max_results')::int;
