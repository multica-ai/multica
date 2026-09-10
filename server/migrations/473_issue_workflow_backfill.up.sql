-- Build one workspace-scoped Default Workflow from the legacy status catalog.
-- Every step is idempotent: unique indexes identify the durable mapping, and
-- the transition baseline is unique per (issue, revision).
INSERT INTO issue_workflow (workspace_id, scope_type, scope_id, name)
SELECT id, 'workspace', id, 'Default'
FROM workspace
ON CONFLICT (workspace_id, scope_type, scope_id) DO NOTHING;

UPDATE workspace AS w
SET default_issue_workflow_id = l.id
FROM issue_workflow AS l
WHERE l.workspace_id = w.id
  AND l.scope_type = 'workspace'
  AND l.scope_id = w.id
  AND w.default_issue_workflow_id IS DISTINCT FROM l.id;

INSERT INTO issue_workflow_status (
    workspace_id, workflow_id, legacy_status_key, name, description, color,
    position, phase, outcome, archived_at, created_at, updated_at
)
SELECT
    s.workspace_id,
    w.default_issue_workflow_id,
    s.key,
    s.name,
    s.description,
    s.color,
    (ROW_NUMBER() OVER (
        PARTITION BY s.workspace_id
        ORDER BY
            CASE s.category
                WHEN 'backlog' THEN 0
                WHEN 'todo' THEN 1
                WHEN 'in_progress' THEN 2
                WHEN 'in_review' THEN 3
                WHEN 'done' THEN 4
                WHEN 'blocked' THEN 5
                WHEN 'cancelled' THEN 6
                ELSE 7
            END,
            s.position,
            s.key
    ) - 1)::double precision,
    CASE s.category
        WHEN 'backlog' THEN 'backlog'
        WHEN 'todo' THEN 'unstarted'
        WHEN 'done' THEN 'completed'
        WHEN 'cancelled' THEN 'cancelled'
        ELSE 'started'
    END,
    CASE s.category
        WHEN 'done' THEN 'completed'
        WHEN 'cancelled' THEN 'cancelled'
        ELSE NULL
    END,
    s.archived_at,
    s.created_at,
    s.updated_at
FROM issue_status AS s
JOIN workspace AS w ON w.id = s.workspace_id
WHERE w.default_issue_workflow_id IS NOT NULL
ON CONFLICT (workflow_id, legacy_status_key) WHERE legacy_status_key IS NOT NULL
DO UPDATE SET
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    color = EXCLUDED.color,
    position = EXCLUDED.position,
    phase = EXCLUDED.phase,
    outcome = EXCLUDED.outcome,
    archived_at = EXCLUDED.archived_at,
    updated_at = EXCLUDED.updated_at;

UPDATE issue AS i
SET workflow_id = w.default_issue_workflow_id,
    workflow_status_id = s.id
FROM workspace AS w
JOIN issue_workflow_status AS s
  ON s.workflow_id = w.default_issue_workflow_id
WHERE w.id = i.workspace_id
  AND s.legacy_status_key = i.status
  AND (
      i.workflow_id IS DISTINCT FROM w.default_issue_workflow_id OR
      i.workflow_status_id IS DISTINCT FROM s.id
  );

INSERT INTO issue_transition (
    workspace_id, issue_id, workflow_id, workflow_revision,
    from_status_id, to_status_id, actor_type, actor_id, cause,
    issue_revision_before, issue_revision_after, created_at
)
SELECT
    i.workspace_id,
    i.id,
    i.workflow_id,
    l.revision,
    NULL,
    i.workflow_status_id,
    'system',
    NULL,
    'migration_backfill',
    i.revision,
    i.revision,
    i.updated_at
FROM issue AS i
JOIN issue_workflow AS l ON l.id = i.workflow_id
WHERE i.workflow_id IS NOT NULL
  AND i.workflow_status_id IS NOT NULL
ON CONFLICT (issue_id, issue_revision_after) DO NOTHING;

UPDATE issue AS i
SET last_transition_id = t.id
FROM issue_transition AS t
WHERE t.issue_id = i.id
  AND t.issue_revision_after = i.revision
  AND i.last_transition_id IS DISTINCT FROM t.id;
