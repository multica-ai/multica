-- name: GetIssueShareToken :one
SELECT token FROM issue_share WHERE issue_id = $1 AND workspace_id = $2;

-- name: CreateIssueShare :one
INSERT INTO issue_share (issue_id, workspace_id, token, created_by)
SELECT i.id, i.workspace_id, sqlc.arg(token), sqlc.arg(user_id)
FROM issue i
JOIN member m ON m.workspace_id = i.workspace_id AND m.user_id = sqlc.arg(user_id)
WHERE i.id = sqlc.arg(issue_id) AND i.workspace_id = sqlc.arg(workspace_id)
  AND i.project_id IS NOT DISTINCT FROM sqlc.narg(project_id)::uuid
  AND (
      m.role IN ('owner', 'admin')
      OR (m.role = 'member' AND (
          (i.creator_type = 'member' AND i.creator_id = m.user_id)
          OR (i.assignee_type = 'member' AND i.assignee_id = m.user_id)
      ))
  )
FOR SHARE OF i
ON CONFLICT (issue_id) DO UPDATE SET token = issue_share.token
WHERE issue_share.workspace_id = EXCLUDED.workspace_id
RETURNING token;

-- name: RevokeIssueShare :execrows
DELETE FROM issue_share s
USING issue i, member m
WHERE s.issue_id = sqlc.arg(issue_id) AND s.workspace_id = sqlc.arg(workspace_id)
  AND s.token = sqlc.arg(token)
  AND i.id = s.issue_id AND i.workspace_id = s.workspace_id
  AND m.workspace_id = s.workspace_id AND m.user_id = sqlc.arg(user_id)
  AND (
      m.role IN ('owner', 'admin')
      OR (m.role = 'member' AND (
          (i.creator_type = 'member' AND i.creator_id = m.user_id)
          OR (i.assignee_type = 'member' AND i.assignee_id = m.user_id)
      ))
  );

-- name: GetSharedIssue :one
SELECT jsonb_build_object(
    'identifier', w.issue_prefix || '-' || i.number::text,
    'title', i.title,
    'description', i.description,
    'status', i.status,
    'status_label', COALESCE(st.name, ''),
    'comments', COALESCE((
        SELECT jsonb_agg(jsonb_build_object(
            'id', c.id, 'author_type', c.author_type,
            'content', c.content, 'created_at', c.created_at
        ) ORDER BY c.created_at, c.id)
        FROM comment c
        WHERE c.issue_id = i.id AND c.workspace_id = i.workspace_id
          AND c.deleted_at IS NULL AND c.quick_action_id IS NULL
          AND c.author_type IN ('member', 'agent') AND c.type = 'comment'
          AND (c.source_task_id IS NULL OR EXISTS (
              SELECT 1 FROM agent_task_queue source
              WHERE source.id = c.source_task_id AND source.issue_id = i.id
                AND source.chat_session_id IS NULL
          ))
    ), '[]'::jsonb),
    'runs', COALESCE((
        SELECT jsonb_agg(jsonb_build_object(
            'id', r.id, 'status', r.status, 'completed_at', r.completed_at
        ) ORDER BY r.completed_at DESC, r.id)
        FROM agent_task_queue r
        WHERE r.issue_id = i.id AND r.chat_session_id IS NULL
          AND r.status IN ('completed', 'failed', 'cancelled')
          AND r.completed_at IS NOT NULL
          AND NOT (r.escalation_for_task_id IS NOT NULL AND r.started_at IS NULL AND r.status = 'cancelled')
    ), '[]'::jsonb)
)::jsonb AS shared_issue
FROM issue_share s
JOIN issue i ON i.id = s.issue_id AND i.workspace_id = s.workspace_id
JOIN workspace w ON w.id = i.workspace_id
JOIN "user" u ON u.id = sqlc.arg(viewer_id)
LEFT JOIN issue_status st ON st.workspace_id = i.workspace_id AND st.key = i.status
WHERE s.token = sqlc.arg(token);
