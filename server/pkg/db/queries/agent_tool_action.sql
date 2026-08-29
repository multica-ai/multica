-- name: CreateOrGetAgentToolActionEvent :one
WITH inserted AS (
    INSERT INTO agent_tool_action_event (
        id, workspace_id, agent_id, task_id, issue_id, invocation_id,
        approval_request_id, transport_kind, server_key, tool_name,
        schema_digest, coverage_kind, event_type, argument_bytes,
        result_bytes, duration_ms, outcome_code, error_class,
        actor_user_id, created_at
    ) VALUES (
        COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()),
        @workspace_id, @agent_id, @task_id, sqlc.narg('issue_id')::uuid,
        @invocation_id, sqlc.narg('approval_request_id')::uuid,
        @transport_kind, @server_key, @tool_name, @schema_digest,
        @coverage_kind, @event_type, sqlc.narg('argument_bytes')::integer,
        sqlc.narg('result_bytes')::integer, sqlc.narg('duration_ms')::bigint,
        sqlc.narg('outcome_code')::text, sqlc.narg('error_class')::text,
        sqlc.narg('actor_user_id')::uuid, @created_at
    )
    ON CONFLICT DO NOTHING
    RETURNING *
)
SELECT * FROM inserted
UNION ALL
SELECT existing.*
FROM agent_tool_action_event AS existing
WHERE NOT EXISTS (SELECT 1 FROM inserted)
  AND existing.workspace_id = @workspace_id
  AND existing.agent_id = @agent_id
  AND existing.task_id = @task_id
  AND existing.issue_id IS NOT DISTINCT FROM sqlc.narg('issue_id')::uuid
  AND existing.invocation_id = @invocation_id
  AND existing.approval_request_id IS NOT DISTINCT FROM sqlc.narg('approval_request_id')::uuid
  AND existing.transport_kind = @transport_kind
  AND existing.server_key = @server_key
  AND existing.tool_name = @tool_name
  AND existing.schema_digest = @schema_digest
  AND existing.coverage_kind = @coverage_kind
  AND existing.event_type = @event_type
  AND existing.argument_bytes IS NOT DISTINCT FROM sqlc.narg('argument_bytes')::integer
  AND existing.result_bytes IS NOT DISTINCT FROM sqlc.narg('result_bytes')::integer
  AND existing.duration_ms IS NOT DISTINCT FROM sqlc.narg('duration_ms')::bigint
  AND existing.outcome_code IS NOT DISTINCT FROM sqlc.narg('outcome_code')::text
  AND existing.error_class IS NOT DISTINCT FROM sqlc.narg('error_class')::text
  AND existing.actor_user_id IS NOT DISTINCT FROM sqlc.narg('actor_user_id')::uuid
LIMIT 1;

-- name: GetAgentToolActionEvent :one
SELECT * FROM agent_tool_action_event
WHERE workspace_id = @workspace_id AND id = @id;

-- name: ListAgentToolActionEvents :many
SELECT * FROM agent_tool_action_event
WHERE workspace_id = @workspace_id
  AND agent_id = @agent_id
  AND (
      sqlc.narg('filter_event_type')::text IS NULL
      OR event_type = sqlc.narg('filter_event_type')::text
  )
  AND (
      sqlc.narg('since')::timestamptz IS NULL
      OR created_at >= sqlc.narg('since')::timestamptz
  )
  AND (
      sqlc.narg('cursor_created_at')::timestamptz IS NULL
      OR (created_at, id) < (
          sqlc.narg('cursor_created_at')::timestamptz,
          sqlc.narg('cursor_id')::uuid
      )
  )
ORDER BY created_at DESC, id DESC
LIMIT @page_size::int;

-- name: ListWorkspaceAgentToolActionEvents :many
SELECT * FROM agent_tool_action_event
WHERE workspace_id = @workspace_id
  AND (
      sqlc.narg('cursor_created_at')::timestamptz IS NULL
      OR (created_at, id) < (
          sqlc.narg('cursor_created_at')::timestamptz,
          sqlc.narg('cursor_id')::uuid
      )
  )
ORDER BY created_at DESC, id DESC
LIMIT @page_size::int;
