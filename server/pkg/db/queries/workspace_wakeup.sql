-- name: ListWorkspaceWakeups :one
-- Counts, filter choices, and page share one snapshot and the same access scope.
WITH base AS MATERIALIZED (
 SELECT w.id,w.issue_id,i.title AS issue_title,ws.issue_prefix||'-'||i.number AS issue_identifier,
  w.agent_id,a.name AS agent_name,w.kind,w.mode,w.event_types,w.filter_actor_type,
 (CASE WHEN actor_agent.id IS NOT NULL OR actor_member.user_id IS NOT NULL THEN w.filter_actor_id END)::uuid AS filter_actor_id,
 COALESCE(actor_agent.name,actor_user.name,'')::text AS filter_actor_name,
  CASE WHEN source.id IS NOT NULL THEN w.filter_agent_id END AS filter_agent_id,
  source.name AS filter_agent_name,
  CASE WHEN EXISTS(SELECT 1 FROM agent_task_queue ft JOIN agent fa ON fa.id=ft.agent_id AND fa.workspace_id=w.workspace_id
   WHERE ft.id=w.filter_task_id AND ft.issue_id=w.issue_id AND fa.id=ANY(@agent_ids::uuid[])) THEN w.filter_task_id END AS filter_task_id,
  w.interval_seconds,w.cron_expression,w.timezone,
  w.next_fire_at,w.enabled,w.revision,w.disabled_at,w.last_task_id,w.last_error,w.created_at,
  (i.status IN ('done','cancelled') OR EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=i.workspace_id AND s.key=i.status AND s.category IN ('done','closed'))) AS issue_closed,
  COALESCE((w.created_by= @member_id::uuid OR @is_admin::boolean),false) AS can_manage,
  COALESCE(r.active_runs,0)::int AS active_runs
 FROM issue_wakeup w
 JOIN workspace ws ON ws.id=w.workspace_id
 JOIN issue i ON i.id=w.issue_id AND i.workspace_id=w.workspace_id
 JOIN agent a ON a.id=w.agent_id AND a.workspace_id=w.workspace_id
 LEFT JOIN agent actor_agent ON w.filter_actor_type='agent' AND actor_agent.id=w.filter_actor_id AND actor_agent.workspace_id=w.workspace_id AND actor_agent.id=ANY(@agent_ids::uuid[])
LEFT JOIN member actor_member ON w.filter_actor_type='member' AND actor_member.user_id=w.filter_actor_id AND actor_member.workspace_id=w.workspace_id
LEFT JOIN "user" actor_user ON actor_user.id=actor_member.user_id
 LEFT JOIN agent source ON source.id=w.filter_agent_id AND source.workspace_id=w.workspace_id AND source.id=ANY(@agent_ids::uuid[])
 LEFT JOIN LATERAL (
  SELECT count(*) AS active_runs FROM agent_task_queue t
  WHERE t.context->>'wakeup_id'=w.id::text AND t.issue_id=w.issue_id AND t.agent_id=w.agent_id
   AND t.status IN ('queued','deferred','dispatched','running','waiting_local_directory')
 ) r ON true
 WHERE w.workspace_id= @workspace_id
), classified AS (
 SELECT *,CASE WHEN (enabled AND NOT issue_closed) OR active_runs>0 THEN 'active'
  WHEN NOT issue_closed AND disabled_at IS NOT NULL THEN 'disabled' ELSE 'ended' END AS scope
 FROM base
), filtered AS (
 SELECT * FROM classified WHERE (@scope::text='all' OR scope= @scope)
  AND (@kind::text='all' OR (@kind='event' AND kind='event') OR (@kind='at' AND kind='at') OR (@kind='recurring' AND kind IN ('every','cron')))
  AND (@agent_id::text='' OR agent_id::text= @agent_id)
  AND (@search::text='' OR strpos(lower(issue_title||' '||issue_identifier||' '||agent_name),lower(@search))>0)
), page AS (
 SELECT * FROM filtered ORDER BY created_at DESC,id DESC LIMIT @page_limit::int OFFSET @page_offset::int
), details AS (
 SELECT p.*,r.status AS last_task_status,
  CASE WHEN r.id IS NOT NULL THEN jsonb_build_object(
   'id',r.id,'agent_id',r.agent_id,'runtime_id',r.runtime_id,'issue_id',r.issue_id,'wakeup_id',p.id,
   'status',r.status,'priority',r.priority,'created_at',r.created_at,'started_at',r.started_at,
   'dispatched_at',r.dispatched_at,'completed_at',r.completed_at
  ) END AS task
 FROM page p
 LEFT JOIN LATERAL (
  SELECT candidate.* FROM (
   (SELECT t.id,t.agent_id,t.runtime_id,t.issue_id,t.status,t.priority,t.created_at,t.started_at,t.dispatched_at,t.completed_at,1 AS active
    FROM agent_task_queue t WHERE t.context->>'wakeup_id'=p.id::text AND t.issue_id=p.issue_id AND t.agent_id=p.agent_id
     AND t.status IN ('queued','deferred','dispatched','running','waiting_local_directory')
    ORDER BY (t.status IN ('running','waiting_local_directory','dispatched')) DESC,t.created_at DESC,t.id DESC LIMIT 1)
   UNION ALL
   (SELECT t.id,t.agent_id,t.runtime_id,t.issue_id,t.status,t.priority,t.created_at,t.started_at,t.dispatched_at,t.completed_at,0 AS active
    FROM agent_task_queue t WHERE t.context->>'wakeup_id'=p.id::text AND t.issue_id=p.issue_id AND t.agent_id=p.agent_id
     AND t.status NOT IN ('queued','deferred','dispatched','running','waiting_local_directory')
    ORDER BY t.created_at DESC,t.id DESC LIMIT 1)
  ) candidate ORDER BY active DESC LIMIT 1
 ) r ON true
)
SELECT jsonb_build_object(
 'items',COALESCE((SELECT jsonb_agg(to_jsonb(details)-'created_at'-'scope' ORDER BY created_at DESC,id DESC) FROM details),'[]'::jsonb),
 'total',(SELECT count(*) FROM filtered),
 'counts',jsonb_build_object('all',(SELECT count(*) FROM classified),'active',(SELECT count(*) FROM classified WHERE scope='active'),
  'disabled',(SELECT count(*) FROM classified WHERE scope='disabled'),'ended',(SELECT count(*) FROM classified WHERE scope='ended')),
 'agents',COALESCE((SELECT jsonb_agg(x ORDER BY x.name,x.id) FROM (SELECT DISTINCT agent_id AS id,agent_name AS name FROM base) x),'[]'::jsonb)
) AS result;
