-- Forward-only, idempotent backfill for runs that existed before the
-- relationship tables were introduced. The legacy JSON body remains the
-- compatibility source for v1 history; new writes use the normalized tables.

INSERT INTO workflow_scope_instance(
  id,workspace_id,run_id,definition_scope_id,parent_scope_id,generation,
  rework_count,state,branch_states
)
SELECT
  gen_random_uuid(),
  r.workspace_id,
  r.id,
  'root',
  NULL,
  1,
  0,
  CASE WHEN r.status IN ('succeeded','failed','cancelled') THEN 'completed'
       ELSE COALESCE(NULLIF(r.status,''),'active') END,
  '{}'::jsonb
FROM workflow_run r
WHERE NOT EXISTS (
  SELECT 1
  FROM workflow_scope_instance existing
  WHERE existing.workspace_id=r.workspace_id
    AND existing.run_id=r.id
    AND existing.definition_scope_id='root'
    AND existing.parent_scope_id IS NULL
);

WITH candidates AS (
  SELECT
    r.workspace_id,
    r.id AS run_id,
    scopes.id AS scope_instance_id,
    node.value AS node,
    CASE
      WHEN node.value->>'generation' ~ '^[0-9]+$' THEN (node.value->>'generation')::integer
      ELSE 1
    END AS generation
  FROM workflow_run r
  JOIN workflow_scope_instance scopes
    ON scopes.workspace_id=r.workspace_id
   AND scopes.run_id=r.id
   AND scopes.definition_scope_id='root'
   AND scopes.parent_scope_id IS NULL
  CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(r.body->'nodes')='array' THEN r.body->'nodes' ELSE '[]'::jsonb END
  ) AS node(value)
  WHERE COALESCE(node.value->>'node_id','')<>''
)
INSERT INTO workflow_node_activation(
  id,workspace_id,run_id,node_id,scope_instance_id,generation,activation_no,
  automatic_retries_used,status,reason_code,issue_id,assignee_snapshot,
  input_snapshot,override,created_at,finished_at
)
SELECT
  CASE
    WHEN c.node->>'activation_id' ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
      THEN (c.node->>'activation_id')::uuid
    ELSE gen_random_uuid()
  END,
  c.workspace_id,
  c.run_id,
  c.node->>'node_id',
  c.scope_instance_id,
	  c.generation,
	  CASE
	    WHEN c.node->>'activation_no' ~ '^[0-9]+$' THEN (c.node->>'activation_no')::integer
	    ELSE 1
	  END,
  CASE
    WHEN c.node->>'automatic_retries_used' ~ '^[0-9]+$' THEN (c.node->>'automatic_retries_used')::integer
    ELSE 0
  END,
  COALESCE(NULLIF(c.node->>'status',''),'pending'),
  COALESCE(c.node->>'reason_code',''),
  CASE
    WHEN c.node->>'issue_id' ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
      THEN (c.node->>'issue_id')::uuid
    ELSE NULL
  END,
  CASE WHEN jsonb_typeof(c.node->'assignee')='object' THEN c.node->'assignee' ELSE '{}'::jsonb END,
  COALESCE(r.body->'input_values', jsonb_build_object('text', COALESCE(r.body->>'input',''))),
  '{}'::jsonb,
  now(),
  CASE WHEN c.node->>'status' IN ('succeeded','failed','cancelled','skipped') THEN now() ELSE NULL END
FROM candidates c
JOIN workflow_run r ON r.workspace_id=c.workspace_id AND r.id=c.run_id
ON CONFLICT DO NOTHING;

INSERT INTO workflow_node_attempt(
  id,workspace_id,activation_id,attempt_no,task_id,status,error_class,error_detail,
  started_at,finished_at,effect_state
)
SELECT
  gen_random_uuid(),
  r.workspace_id,
  activation.id,
  legacy.attempt,
  CASE
    WHEN legacy.body->>'task_id' ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
      THEN (legacy.body->>'task_id')::uuid
    ELSE NULL
  END,
  CASE WHEN legacy.body->>'status'='succeeded' THEN 'completed'
       WHEN legacy.body->>'status'='skipped' THEN 'cancelled'
       ELSE COALESCE(NULLIF(legacy.body->>'status',''),'pending') END,
  COALESCE(legacy.body->>'reason_code',''),
  COALESCE(legacy.body->>'error',''),
  now(),
  CASE WHEN legacy.body->>'status' IN ('succeeded','failed','cancelled','skipped') THEN now() ELSE NULL END,
  '{}'::jsonb
FROM workflow_node_run legacy
JOIN workflow_run r ON r.id=legacy.run_id
JOIN LATERAL (
  SELECT activation.id
  FROM workflow_node_activation activation
  WHERE activation.workspace_id=r.workspace_id
    AND activation.run_id=r.id
    AND activation.node_id=legacy.node_id
  ORDER BY activation.generation DESC,activation.activation_no DESC
  LIMIT 1
) activation ON TRUE
WHERE legacy.attempt>0
ON CONFLICT DO NOTHING;

WITH current_outputs AS (
  SELECT DISTINCT ON (r.workspace_id,r.id,activation.node_id)
    r.workspace_id,
    r.id AS run_id,
    activation.id AS activation_id,
    node.value->>'output' AS output
  FROM workflow_run r
  JOIN workflow_node_activation activation
    ON activation.workspace_id=r.workspace_id AND activation.run_id=r.id
  CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(r.body->'nodes')='array' THEN r.body->'nodes' ELSE '[]'::jsonb END
  ) AS node(value)
  WHERE activation.node_id=node.value->>'node_id'
    AND activation.status='succeeded'
    AND COALESCE(node.value->>'output','')<>''
  ORDER BY r.workspace_id,r.id,activation.node_id,activation.generation DESC
)
INSERT INTO workflow_output(
  id,workspace_id,activation_id,schema_snapshot,"values",artifact_refs,content_hash
)
SELECT
  gen_random_uuid(),
  output.workspace_id,
  output.activation_id,
  '{}'::jsonb,
  jsonb_build_object('text',output.output),
  '[]'::jsonb,
  md5(output.output)
FROM current_outputs output
ON CONFLICT DO NOTHING;

UPDATE workflow_node_activation activation
SET effective_output_id=output.id
FROM workflow_output output
WHERE output.workspace_id=activation.workspace_id
  AND output.activation_id=activation.id
  AND output.superseded_at IS NULL
  AND activation.effective_output_id IS NULL;

WITH latest_activation AS (
  SELECT DISTINCT ON (workspace_id,run_id,node_id)
    workspace_id,run_id,node_id,id
  FROM workflow_node_activation
  ORDER BY workspace_id,run_id,node_id,generation DESC,activation_no DESC
)
UPDATE workflow_work_item item
SET activation_id=latest.id
FROM latest_activation latest
WHERE item.activation_id IS NULL
  AND item.status='open'
  AND item.workspace_id=latest.workspace_id
  AND item.run_id=latest.run_id
  AND item.node_id=latest.node_id;

WITH graph_edges AS (
  SELECT
    r.workspace_id,
    r.id AS run_id,
    edge.value AS edge
  FROM workflow_run r
  CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(r.body->'graph'->'edges')='array' THEN r.body->'graph'->'edges' ELSE '[]'::jsonb END
  ) AS edge(value)
), activations AS (
  SELECT DISTINCT ON (workspace_id,run_id,node_id)
    workspace_id,run_id,node_id,id,generation,status
  FROM workflow_node_activation
  ORDER BY workspace_id,run_id,node_id,generation DESC,activation_no DESC
)
INSERT INTO workflow_transition(
  id,workspace_id,run_id,source_activation_id,outlet,target_scope_id,
  generation,branch_id,payload_ref
)
SELECT
  gen_random_uuid(),
  edges.workspace_id,
  edges.run_id,
  source.id,
  COALESCE(NULLIF(edges.edge->>'id',''),edges.edge->>'source_port'),
  target_scope.id,
  source.generation,
  COALESCE(edges.edge->>'target',''),
  jsonb_build_object('target',edges.edge->>'target','target_port',edges.edge->>'target_port')
FROM graph_edges edges
JOIN activations source
  ON source.workspace_id=edges.workspace_id
 AND source.run_id=edges.run_id
 AND source.node_id=edges.edge->>'source'
JOIN activations target
  ON target.workspace_id=edges.workspace_id
 AND target.run_id=edges.run_id
 AND target.node_id=edges.edge->>'target'
JOIN workflow_scope_instance target_scope
  ON target_scope.workspace_id=edges.workspace_id
 AND target_scope.run_id=edges.run_id
 AND target_scope.definition_scope_id='root'
 AND target_scope.parent_scope_id IS NULL
WHERE COALESCE(edges.edge->>'kind','flow') NOT IN ('rework','compensation')
  AND source.status<>'skipped'
  AND target.status<>'skipped'
ON CONFLICT DO NOTHING;
