-- name: UpsertTaskCodeChange :exec
-- One snapshot per task, written when the run ends. A re-report for the same
-- task replaces the previous one.
INSERT INTO task_code_change (task_id, workspace_id, issue_id, repos, repos_truncated)
VALUES (@task_id, @workspace_id, sqlc.narg(issue_id), @repos, @repos_truncated)
ON CONFLICT (task_id) DO UPDATE
SET repos = EXCLUDED.repos,
    repos_truncated = EXCLUDED.repos_truncated,
    updated_at = now();

-- name: GetTaskCodeChange :one
SELECT task_id, repos, repos_truncated, updated_at
FROM task_code_change
WHERE task_id = $1;

-- name: ListTaskCodeChangeFileCounts :many
-- Changed-file count per run for the execution log, one batch for the listed
-- tasks. Runs with no snapshot have no row.
SELECT c.task_id,
       COALESCE(SUM(jsonb_array_length(COALESCE(r.repo->'files', '[]'::jsonb))), 0)::int AS file_count
FROM task_code_change c
LEFT JOIN LATERAL jsonb_array_elements(c.repos) AS r(repo) ON true
WHERE c.workspace_id = @workspace_id
  AND c.task_id = ANY(@task_ids::uuid[])
GROUP BY c.task_id;
