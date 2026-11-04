-- name: CountDirectiveIssuesForMessage :one
-- Directives already filed from one boss chat message. The handler compares
-- this against the per-message cap before it creates another.
SELECT count(*)::bigint FROM issue
WHERE workspace_id = $1 AND origin_type = 'directive' AND origin_id = $2;
