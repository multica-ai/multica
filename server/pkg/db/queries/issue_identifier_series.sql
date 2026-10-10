-- name: ReserveIssueIdentifierSeries :one
INSERT INTO issue_identifier_series (workspace_id, prefix, project_id)
VALUES (sqlc.arg('workspace_id'), sqlc.arg('prefix'), sqlc.narg('project_id'))
ON CONFLICT (workspace_id, prefix) DO UPDATE SET project_id = sqlc.narg('project_id')
WHERE issue_identifier_series.next_number = 0
  AND issue_identifier_series.project_id IS NOT DISTINCT FROM sqlc.narg('project_id')
RETURNING *;

-- name: ReleaseUnusedIssueIdentifierSeries :exec
DELETE FROM issue_identifier_series
WHERE workspace_id = sqlc.arg('workspace_id') AND prefix = sqlc.arg('prefix')
  AND project_id IS NOT DISTINCT FROM sqlc.narg('project_id') AND next_number = 0;

-- name: AllocateIssueIdentifier :one
WITH locked_workspace AS MATERIALIZED (
    SELECT w.issue_counter, COALESCE(NULLIF(UPPER(w.issue_prefix), ''), 'WS') AS issue_prefix
    FROM workspace w
    WHERE w.id = sqlc.arg('workspace_id')
    FOR UPDATE
), allocated AS (
    INSERT INTO issue_identifier_series (workspace_id, prefix, next_number, issued_at)
    SELECT sqlc.arg('workspace_id'), sqlc.arg('prefix'),
           GREATEST(
               COALESCE(MAX(i.number), 0),
               CASE WHEN sqlc.arg('prefix') = lw.issue_prefix THEN lw.issue_counter ELSE 0 END
           ) + 1,
           now()
    FROM locked_workspace lw
    LEFT JOIN issue i
      ON i.workspace_id = sqlc.arg('workspace_id')
     AND i.identifier_prefix = sqlc.arg('prefix')
    GROUP BY lw.issue_counter, lw.issue_prefix
    ON CONFLICT (workspace_id, prefix) DO UPDATE
    SET next_number = GREATEST(
            issue_identifier_series.next_number + 1,
            (SELECT COALESCE(MAX(number), 0) + 1
             FROM issue
             WHERE workspace_id = sqlc.arg('workspace_id') AND identifier_prefix = sqlc.arg('prefix')),
            (SELECT CASE WHEN sqlc.arg('prefix') = issue_prefix THEN issue_counter + 1 ELSE 0 END
             FROM locked_workspace)
        ),
        issued_at = COALESCE(issue_identifier_series.issued_at, now())
    RETURNING workspace_id, prefix, next_number
), mirror AS (
    UPDATE workspace w SET issue_counter = allocated.next_number
    FROM allocated
    WHERE w.id = allocated.workspace_id
      AND COALESCE(NULLIF(UPPER(w.issue_prefix), ''), 'WS') = allocated.prefix
)
SELECT next_number FROM allocated;

-- name: GetIssueIdentifierSeries :one
SELECT * FROM issue_identifier_series
WHERE workspace_id = sqlc.arg('workspace_id') AND prefix = sqlc.arg('prefix');
