-- name: UpsertDailyPlan :one
INSERT INTO daily_plan (workspace_id, user_id, plan_date, draft_content, top_issue_ids, generated_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, user_id, plan_date)
DO UPDATE SET
    draft_content = EXCLUDED.draft_content,
    top_issue_ids = EXCLUDED.top_issue_ids,
    generated_by = EXCLUDED.generated_by,
    status = 'draft',
    confirmed_at = NULL,
    updated_at = now()
RETURNING *;

-- name: GetDailyPlanByDate :one
SELECT * FROM daily_plan
WHERE workspace_id = $1 AND user_id = $2 AND plan_date = $3;

-- name: GetDailyPlanByID :one
SELECT * FROM daily_plan
WHERE id = $1 AND workspace_id = $2;

-- name: ListDailyPlans :many
SELECT * FROM daily_plan
WHERE workspace_id = $1 AND user_id = $2
ORDER BY plan_date DESC
LIMIT $3;

-- name: ConfirmDailyPlan :one
UPDATE daily_plan
SET status = 'confirmed', confirmed_at = now(), updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;
