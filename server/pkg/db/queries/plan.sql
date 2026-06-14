-- name: GetPlanByDate :one
SELECT * FROM daily_plan
WHERE workspace_id = @workspace_id AND user_id = @user_id AND plan_date = @plan_date;

-- name: UpsertPlanForDate :one
INSERT INTO daily_plan (
    workspace_id,
    user_id,
    plan_date,
    draft_content,
    top_issue_ids,
    generated_by,
    energy_level,
    energy_note,
    recovery_need,
    capacity_minutes,
    capacity_note
) VALUES (
    @workspace_id,
    @user_id,
    @plan_date,
    COALESCE(@draft_content, ''),
    COALESCE(@top_issue_ids::uuid[], '{}'::uuid[]),
    COALESCE(@generated_by, 'manual'),
    @energy_level,
    @energy_note,
    COALESCE(@recovery_need, false),
    @capacity_minutes,
    @capacity_note
)
ON CONFLICT (workspace_id, user_id, plan_date)
DO UPDATE SET
    energy_level = COALESCE(EXCLUDED.energy_level, daily_plan.energy_level),
    energy_note = COALESCE(EXCLUDED.energy_note, daily_plan.energy_note),
    recovery_need = EXCLUDED.recovery_need,
    capacity_minutes = COALESCE(EXCLUDED.capacity_minutes, daily_plan.capacity_minutes),
    capacity_note = COALESCE(EXCLUDED.capacity_note, daily_plan.capacity_note),
    updated_at = now()
RETURNING *;

-- name: UpdatePlanCapacity :one
UPDATE daily_plan
SET
    energy_level = @energy_level,
    energy_note = @energy_note,
    recovery_need = @recovery_need,
    capacity_minutes = @capacity_minutes,
    capacity_note = @capacity_note,
    updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id
RETURNING *;

-- name: ListPlanItems :many
SELECT * FROM plan_item
WHERE workspace_id = @workspace_id AND plan_id = @plan_id AND user_id = @user_id
ORDER BY position ASC, created_at ASC;

-- name: GetPlanItem :one
SELECT * FROM plan_item
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id;

-- name: GetPlanItemByIssue :one
SELECT * FROM plan_item
WHERE workspace_id = @workspace_id
  AND plan_id = @plan_id
  AND user_id = @user_id
  AND issue_id = @issue_id;

-- name: CreatePlanItem :one
INSERT INTO plan_item (
    workspace_id,
    user_id,
    plan_id,
    issue_id,
    suggested_issue_type_id,
    title_snapshot,
    note,
    position,
    estimated_minutes,
    status,
    source
) VALUES (
    @workspace_id,
    @user_id,
    @plan_id,
    @issue_id,
    @suggested_issue_type_id,
    @title_snapshot,
    COALESCE(@note, ''),
    @position,
    @estimated_minutes,
    COALESCE(@status, 'planned'),
    COALESCE(@source, 'manual')
)
RETURNING *;

-- name: UpdatePlanItem :one
UPDATE plan_item
SET
    title_snapshot = COALESCE(sqlc.narg('title_snapshot'), title_snapshot),
    note = COALESCE(sqlc.narg('note'), note),
    estimated_minutes = COALESCE(sqlc.narg('estimated_minutes'), estimated_minutes),
    status = COALESCE(sqlc.narg('status'), status),
    status_reason = COALESCE(sqlc.narg('status_reason'), status_reason),
    suggested_issue_type_id = COALESCE(sqlc.narg('suggested_issue_type_id'), suggested_issue_type_id),
    completed_at = CASE WHEN sqlc.narg('status')::text = 'done' THEN now() ELSE completed_at END,
    skipped_at = CASE WHEN sqlc.narg('status')::text = 'skipped' THEN now() ELSE skipped_at END,
    updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id
RETURNING *;

-- name: UpdatePlanItemStatus :one
UPDATE plan_item
SET
    status = @status,
    status_reason = @status_reason,
    completed_at = CASE WHEN @status = 'done' THEN now() ELSE completed_at END,
    skipped_at = CASE WHEN @status = 'skipped' THEN now() ELSE skipped_at END,
    updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id
RETURNING *;

-- name: DeletePlanItem :exec
DELETE FROM plan_item
WHERE id = @id AND workspace_id = @workspace_id AND user_id = @user_id;

-- name: ListPlanCandidates :many
SELECT i.*
FROM issue i
WHERE i.workspace_id = @workspace_id
  AND i.archived_at IS NULL
  AND i.status NOT IN ('done', 'cancelled')
  AND (@issue_type_id::uuid IS NULL OR i.issue_type_id = @issue_type_id)
  AND NOT EXISTS (
      SELECT 1
      FROM plan_item pi
      WHERE pi.workspace_id = i.workspace_id
        AND pi.plan_id = @plan_id
        AND pi.issue_id = i.id
  )
ORDER BY
  CASE i.priority WHEN 'urgent' THEN 1 WHEN 'high' THEN 2 WHEN 'medium' THEN 3 WHEN 'low' THEN 4 ELSE 5 END,
  i.due_date ASC NULLS LAST,
  i.updated_at DESC
LIMIT @limit_count;

-- name: ListTimeEntriesByPlanItem :many
SELECT * FROM time_entry
WHERE workspace_id = @workspace_id AND plan_item_id = @plan_item_id
ORDER BY start_time DESC;
