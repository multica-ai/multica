-- No foreign keys: preferences remain explicit invalid selections when a
-- runtime is deleted, and task routing remains historical execution evidence.
CREATE TABLE agent_runtime_preference (
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    runtime_id UUID NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE agent_task_queue ADD COLUMN runtime_routing JSONB;

-- Modern routes freeze selection, not authority. Compare JSON strings with
-- database UUID text so malformed stored evidence fails closed without casts.
-- NULL snapshots retain the default binding used by pre-routing task rows.
CREATE FUNCTION task_runtime_allowed(task_agent_id UUID, task_runtime_id UUID, routing JSONB)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM agent a
        JOIN agent_runtime r ON r.id = task_runtime_id
        WHERE a.id = task_agent_id
          AND a.workspace_id = r.workspace_id
          AND a.archived_at IS NULL
          AND (
              (
                  routing IS NULL
                  AND a.runtime_id = r.id
                  AND (r.visibility = 'public' OR (r.visibility = 'private' AND (r.owner_id IS NULL OR a.owner_id IS NULL OR r.owner_id = a.owner_id)))
              )
              OR (
                  r.owner_id IS NOT NULL
                  AND routing->'version' = '1'::jsonb
                  AND jsonb_typeof(routing->'routes') = 'object'
                  AND routing->'routes'->a.id::text->>'runtime_id' = r.id::text
                  AND routing->'routes'->a.id::text->>'runtime_owner_id' = r.owner_id::text
                  AND routing->'routes'->a.id::text->>'provider' = r.provider
                  AND (
                      (
                          routing->'routes'->a.id::text->>'source' = 'personal'
                          AND r.owner_id::text = routing->>'execution_user_id'
                      )
                      OR (
                          routing->'routes'->a.id::text->>'source' = 'default'
                          AND (r.owner_id = a.owner_id OR r.visibility = 'public')
                      )
                  )
                  AND EXISTS (
                      SELECT 1 FROM member m
                      WHERE m.workspace_id = a.workspace_id
                        AND m.user_id::text = routing->>'execution_user_id'
                  )
                  AND (
                      a.owner_id::text = routing->>'execution_user_id'
                      OR (
                          a.permission_mode = 'public_to'
                          AND EXISTS (
                              SELECT 1 FROM agent_invocation_target ait
                              WHERE ait.agent_id = a.id
                                AND (
                                    (ait.target_type = 'member' AND ait.target_id::text = routing->>'execution_user_id')
                                    OR (ait.target_type = 'workspace' AND ait.target_id = a.workspace_id)
                                )
                          )
                      )
                  )
              )
          )
    );
$$;
