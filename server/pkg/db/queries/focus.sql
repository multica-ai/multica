-- name: GetFocusSession :one
SELECT * FROM focus_sessions
WHERE user_id = $1 AND workspace_id = $2;

-- name: UpsertFocusStart :one
INSERT INTO focus_sessions (
    user_id,
    workspace_id,
    mode,
    phase,
    preset,
    issue_id,
    description,
    commitment_text,
    label_ids,
    first_started_at,
    started_at,
    paused_at,
    elapsed_focus_seconds,
    suggested_break_seconds,
    status_reason,
    reason_note,
    updated_at
) VALUES (
    @user_id,
    @workspace_id,
    @mode,
    'focusing',
    @preset,
    @issue_id,
    @description,
    @commitment_text,
    @label_ids,
    NOW(),
    NOW(),
    NULL,
    0,
    NULL,
    @status_reason,
    @reason_note,
    NOW()
)
ON CONFLICT (user_id, workspace_id) DO UPDATE SET
    mode = EXCLUDED.mode,
    phase = EXCLUDED.phase,
    preset = EXCLUDED.preset,
    issue_id = EXCLUDED.issue_id,
    description = EXCLUDED.description,
    commitment_text = EXCLUDED.commitment_text,
    label_ids = EXCLUDED.label_ids,
    first_started_at = NOW(),
    started_at = NOW(),
    paused_at = NULL,
    elapsed_focus_seconds = 0,
    suggested_break_seconds = NULL,
    status_reason = EXCLUDED.status_reason,
    reason_note = EXCLUDED.reason_note,
    updated_at = NOW()
RETURNING *;

-- name: UpdateFocusSession :one
UPDATE focus_sessions
SET
    mode = @mode,
    phase = @phase,
    preset = @preset,
    issue_id = @issue_id,
    description = @description,
    commitment_text = @commitment_text,
    label_ids = @label_ids,
    first_started_at = @first_started_at,
    started_at = @started_at,
    paused_at = @paused_at,
    elapsed_focus_seconds = @elapsed_focus_seconds,
    suggested_break_seconds = @suggested_break_seconds,
    status_reason = @status_reason,
    reason_note = @reason_note,
    updated_at = NOW()
WHERE id = @id
  AND user_id = @user_id
  AND workspace_id = @workspace_id
RETURNING *;

-- name: CreateFocusEvent :one
INSERT INTO focus_events (
    workspace_id,
    user_id,
    focus_session_id,
    event_type,
    reason,
    note,
    duration_seconds,
    metadata
) VALUES (
    @workspace_id,
    @user_id,
    @focus_session_id,
    @event_type,
    @reason,
    @note,
    @duration_seconds,
    @metadata
)
RETURNING *;

-- name: ListFocusEventsBySession :many
SELECT * FROM focus_events
WHERE workspace_id = @workspace_id
  AND user_id = @user_id
  AND focus_session_id = @focus_session_id
ORDER BY created_at ASC;
