-- FIR-3493 bid 8: convert every saved Issue workflow recipe to Chain v2 and
-- remove generated rules from the retired fixed-loop compiler.

CREATE OR REPLACE FUNCTION pg_temp.loop_v1_check_block(v jsonb, prefix text, pos integer)
RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
SELECT jsonb_strip_nulls(jsonb_build_object(
  'id', prefix || '-' || pos,
  'type', CASE v->>'type' WHEN 'programmatic' THEN 'command' WHEN 'judge' THEN 'review' ELSE 'human' END,
  'name', v->>'label',
  'check', CASE WHEN v->>'type' = 'programmatic' THEN v->'check' END,
  'expect', CASE WHEN v->>'type' = 'programmatic' THEN COALESCE(v->>'expect', 'exit_zero') END,
  'rubric', CASE WHEN v->>'type' = 'judge' THEN v->>'rubric' END,
  'skill', CASE WHEN v->>'type' = 'judge' THEN v->>'skill' END,
  'agents', CASE WHEN v->>'type' = 'judge' AND v->>'assignee_type' = 'agent' AND COALESCE(v->>'assignee_id','') <> ''
                 THEN jsonb_build_array(jsonb_strip_nulls(jsonb_build_object('agent_id',v->>'assignee_id','model',v->>'model','thinking_level',v->>'thinking_level'))) END,
  'prompt', CASE WHEN v->>'type' = 'human' THEN v->>'prompt' END,
  'approver_type', CASE WHEN v->>'type' = 'human' THEN v->>'assignee_type' END,
  'approver_id', CASE WHEN v->>'type' = 'human' THEN v->>'assignee_id' END
));
$$;

CREATE OR REPLACE FUNCTION pg_temp.loop_v1_blocks(spec jsonb, build jsonb, checks jsonb, prefix text)
RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
SELECT jsonb_build_array(jsonb_strip_nulls(jsonb_build_object(
  'id', prefix || '-session', 'type', 'session', 'name', COALESCE(build->>'name', initcap(prefix)),
  'skill', COALESCE(build->>'build_skill', spec->>'build_skill'),
  'goal', COALESCE(build->>'goal', spec->>'goal'),
  'agents', CASE WHEN COALESCE(build->>'build_agent_id', spec->>'build_agent_id','') <> ''
                 THEN jsonb_build_array(jsonb_strip_nulls(jsonb_build_object(
                   'agent_id', COALESCE(build->>'build_agent_id',spec->>'build_agent_id'),
                   'model', COALESCE(build->>'model',spec->>'build_model'),
                   'thinking_level', COALESCE(build->>'thinking_level',spec->>'build_thinking')))) END
))) || COALESCE((
  SELECT jsonb_agg(pg_temp.loop_v1_check_block(value, prefix || '-check', ordinality::integer) ORDER BY ordinality)
  FROM jsonb_array_elements(COALESCE(checks,'[]'::jsonb)) WITH ORDINALITY
), '[]'::jsonb);
$$;

CREATE OR REPLACE FUNCTION pg_temp.loop_v1_limits(spec jsonb)
RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
SELECT jsonb_build_object(
  'max_steps', GREATEST(1, COALESCE((spec#>>'{caps,max_iterations}')::integer, 6)),
  'max_rounds', GREATEST(1, COALESCE((spec#>>'{caps,max_revisions}')::integer, 6)),
  'no_progress_stalls', GREATEST(1, COALESCE((spec#>>'{caps,no_progress_stalls}')::integer, 2))
);
$$;

CREATE OR REPLACE FUNCTION pg_temp.loop_v1_to_chain(spec jsonb)
RETURNS jsonb LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
  phases jsonb := '[]'::jsonb;
  p jsonb;
  idx integer := 0;
BEGIN
  IF COALESCE((spec->>'planning')::boolean, false) THEN
    phases := phases || jsonb_build_array(jsonb_build_object(
      'id','plan', 'name','Plan', 'status',COALESCE(spec->>'planning_status','todo'),
      'blocks', pg_temp.loop_v1_blocks(spec, jsonb_build_object('build_skill',spec->>'plan_skill','build_agent_id',spec->>'plan_agent_id','model',spec->>'plan_model','thinking_level',spec->>'plan_thinking'), spec->'plan_gate', 'plan'),
      'limits',pg_temp.loop_v1_limits(spec)));
  END IF;

  IF jsonb_array_length(COALESCE(spec->'phases','[]'::jsonb)) > 0 THEN
    FOR p IN SELECT value FROM jsonb_array_elements(spec->'phases') LOOP
      idx := idx + 1;
      phases := phases || jsonb_build_array(jsonb_build_object(
        'id','build-' || idx, 'name',COALESCE(p->>'name','Build ' || idx),
        'status',CASE WHEN idx = 1 THEN COALESCE(spec->>'build_status','in_progress') ELSE NULL END,
        'blocks',pg_temp.loop_v1_blocks(spec,p,p->'verification','build-' || idx),
        'limits',pg_temp.loop_v1_limits(spec)));
    END LOOP;
  ELSE
    phases := phases || jsonb_build_array(jsonb_build_object(
      'id','build', 'name','Build', 'status',COALESCE(spec->>'build_status','in_progress'),
      'blocks',pg_temp.loop_v1_blocks(spec,'{}'::jsonb,spec->'verification','build'),
      'limits',pg_temp.loop_v1_limits(spec)));
  END IF;

  RETURN jsonb_strip_nulls(jsonb_build_object('version',2,'phases',phases,'done_status',COALESCE(spec->>'done_status','done')));
END;
$$;

UPDATE cerebro_workflow
SET loop_spec = pg_temp.loop_v1_to_chain(loop_spec::jsonb), updated_at = now()
WHERE workflow_type = 'issue_loop' AND (loop_spec::jsonb->>'version')::integer = 1;

CREATE TEMP TABLE active_issue_loop_cutover ON COMMIT DROP AS
SELECT DISTINCT ON (child.generated_for_issue_id)
  recipe.id AS recipe_id, child.generated_for_issue_id AS issue_id,
  recipe.workspace_id, recipe.project_id, recipe.created_by_id, recipe.created_by_type
FROM cerebro_workflow child
JOIN cerebro_workflow recipe ON recipe.id = child.generated_from_workflow_id
WHERE recipe.workflow_type = 'issue_loop' AND child.generated_for_issue_id IS NOT NULL
ORDER BY child.generated_for_issue_id, child.created_at DESC;

DELETE FROM cerebro_workflow child
USING cerebro_workflow recipe
WHERE child.generated_from_workflow_id = recipe.id
  AND recipe.workflow_type = 'issue_loop';

-- Keep the active recipe<->issue binding, but never keep executable legacy
-- rules. The application resumes these bindings through ChainDriver.
INSERT INTO cerebro_workflow (
  workspace_id, project_id, name, enabled, trigger_type, trigger_config,
  conditions, action_type, action_config, editor_mode, editor_layout,
  created_by_id, created_by_type, generated_from_workflow_id, generated_for_issue_id
)
SELECT workspace_id, project_id, 'loop:chain-activation', false,
  'status_changed', '{"to_status":"__chain_driver__"}'::jsonb, '[]'::jsonb,
  'set_status', '{"status":"done"}'::jsonb, 'form', 'null'::jsonb,
  created_by_id, created_by_type, recipe_id, issue_id
FROM active_issue_loop_cutover;
