package handler

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Fixed projections keep the graph a read-only view of native records. All
// Issue owners and source references share the caller's project visibility.
// Match hiddenProjectPredicate by applying the no-project sentinel to NULL.
const trajectoryGraphIssueSQL = `-- trajectory graph issues
SELECT jsonb_build_object(
 'id','issue:'||i.id,'kind','issue','title',i.title,'status',i.status,
 'issue_id',i.id,'created_at',i.created_at,
 'details',jsonb_build_object(
  'parent_id',parent.id,'identifier',i.number,'description',i.description,'revision',i.revision,
  'project_id',i.project_id,'project_title',p.title,'stage',i.stage,'status_category',s.category,
  'run_count',(SELECT count(*) FROM agent_task_queue t WHERE t.issue_id=i.id AND t.chat_session_id IS NULL),
  'issue_key',w.issue_prefix||'-'||i.number,'origin_type',i.origin_type,'origin_id',i.origin_id,
  'creation_run_id',creation.id,'creation_issue_id',creation_issue.id,
  'creation_workflow_step',creation.workflow_step,'creation_trigger_kind',creation.context->>'trigger_kind',
  'creation_trigger_evidence_kind',creation.trigger_evidence_kind,
  'creation_trigger_evidence_ref_id',creation.trigger_evidence_ref_id,
  'creation_source_status',creation_status.details->>'to',
  'creation_status_event_id',creation_status.id,'creation_status_event_at',creation_status.created_at))
FROM issue i JOIN workspace w ON w.id=i.workspace_id
LEFT JOIN issue parent ON parent.id=i.parent_issue_id AND parent.workspace_id=$1
 AND NOT (COALESCE(parent.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[]))
LEFT JOIN project p ON p.id=i.project_id AND p.workspace_id=i.workspace_id
LEFT JOIN issue_status s ON s.workspace_id=i.workspace_id AND s.key=i.status
LEFT JOIN agent_task_queue creation ON creation.id=i.origin_id
 AND i.origin_type IN ('agent_create','quick_create') AND creation.chat_session_id IS NULL
 AND EXISTS (SELECT 1 FROM issue source WHERE source.id=creation.issue_id AND source.workspace_id=$1
  AND NOT (COALESCE(source.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[])))
LEFT JOIN issue creation_issue ON creation_issue.id=creation.issue_id AND creation_issue.workspace_id=$1
 AND NOT (COALESCE(creation_issue.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[]))
LEFT JOIN LATERAL (
 SELECT a.id,a.created_at,a.details FROM activity_log a
 WHERE a.workspace_id=$1 AND a.issue_id=creation_issue.id
 AND a.action='status_changed' AND a.created_at<=i.created_at
 ORDER BY a.created_at DESC,a.id DESC LIMIT 1
) creation_status ON true
WHERE i.workspace_id=$1 AND i.id=ANY($2::uuid[])
 AND NOT (COALESCE(i.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[]))
ORDER BY i.created_at,i.id`

const trajectoryGraphNeighborsSQL = `-- trajectory graph neighbors
SELECT i.id FROM issue i
WHERE i.workspace_id=$1 AND NOT i.id=ANY($2::uuid[])
 AND NOT (COALESCE(i.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[]))
 AND (
  i.parent_issue_id=ANY($2::uuid[])
  OR i.id IN (SELECT parent_issue_id FROM issue WHERE workspace_id=$1 AND id=ANY($2::uuid[]))
  OR i.id IN (SELECT t.issue_id FROM issue child JOIN agent_task_queue t ON t.id=child.origin_id
   WHERE child.workspace_id=$1 AND child.origin_type IN ('agent_create','quick_create')
   AND child.id=ANY($2::uuid[]) AND t.chat_session_id IS NULL)
  OR (i.origin_type IN ('agent_create','quick_create') AND i.origin_id IN (
   SELECT t.id FROM agent_task_queue t WHERE t.issue_id=ANY($2::uuid[]) AND t.chat_session_id IS NULL))
  OR i.id IN (
   SELECT d.depends_on_issue_id FROM issue_dependency d JOIN issue owner ON owner.id=d.issue_id AND owner.workspace_id=$1
   WHERE d.issue_id=ANY($2::uuid[])
   UNION
   SELECT d.issue_id FROM issue_dependency d JOIN issue target ON target.id=d.depends_on_issue_id AND target.workspace_id=$1
   WHERE d.depends_on_issue_id=ANY($2::uuid[]))
  OR i.id IN (SELECT duplicate_of_issue_id FROM issue WHERE workspace_id=$1 AND status='cancelled' AND id=ANY($2::uuid[]))
  OR (i.status='cancelled' AND i.duplicate_of_issue_id=ANY($2::uuid[])))
ORDER BY i.created_at,i.id LIMIT 201`

// Native comment-trigger summaries contain comment text, so their graph title
// uses the same explicit source-access gate as trigger_content. A call needs
// a matching native trigger comment, not the Issue creator's attribution.
// Exclude merged input and comments added after enqueue: neither establishes
// the original dispatch. The client still needs both allowed Runs and the
// allowed comment after it joins the separately loaded Issue graphs.
const trajectoryGraphRunsSQL = `-- trajectory graph runs
SELECT jsonb_build_object(
 'id','run:'||t.id,'kind','run','title',CASE
  WHEN (t.trigger_comment_id IS NULL OR trigger_comment.id IS NOT NULL)
   AND (t.trigger_evidence_kind IS DISTINCT FROM 'comment' OR t.trigger_evidence_ref_id IS NULL OR evidence_comment.id IS NOT NULL)
  THEN COALESCE(t.trigger_summary,'Agent run') ELSE 'Agent run' END,
 'status',t.status,'issue_id',t.issue_id,'run_id',t.id,'created_at',t.created_at,
 'details',jsonb_build_object(
  'attempt',t.attempt,'work_dir',t.work_dir,'error',t.error,
  'retry_of',t.retry_of_task_id,'rerun_of',t.rerun_of_task_id,
  'delegated_from',t.delegated_from_task_id,'parent_run',t.parent_task_id,
  'instructions',t.context->>'instructions','workflow_step',t.workflow_step,
  'wait_reason',t.wait_reason,'failure_reason',t.failure_reason,
  'run_call',(SELECT jsonb_build_object('comment_id',evidence_comment.id,'source_issue_id',source.id)
   FROM agent_task_queue caller JOIN issue source ON source.id=caller.issue_id
   WHERE caller.id=evidence_comment.source_task_id AND caller.chat_session_id IS NULL
   AND source.workspace_id=$1
   AND NOT (COALESCE(source.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[]))
   AND NOT (COALESCE(i.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[]))
   AND t.originator_source IN ('delegation','comment_source','owner_fallback')
   AND t.trigger_comment_id=evidence_comment.id AND evidence_comment.issue_id=t.issue_id
   AND evidence_comment.author_type='agent' AND caller.id=t.delegated_from_task_id
   AND evidence_comment.created_at<=t.created_at AND COALESCE(cardinality(t.coalesced_comment_ids),0)=0
   AND t.retry_of_task_id IS NULL AND t.rerun_of_task_id IS NULL
   AND COALESCE(t.context->>'wakeup_id','')='' AND COALESCE(t.context->>'trigger_kind','') !~ 'wakeup|^autopilot|^(retry|rerun)$'),
  'trigger_kind',t.context->>'trigger_kind','trigger_evidence_kind',t.trigger_evidence_kind,
  'trigger_evidence_ref_id',t.trigger_evidence_ref_id,
  'handoff_note',t.handoff_note,'trigger_comment_id',t.trigger_comment_id,
  'trigger_content',CASE WHEN (t.trigger_comment_id IS NULL OR trigger_comment.id IS NOT NULL)
   AND (t.trigger_evidence_kind IS DISTINCT FROM 'comment' OR t.trigger_evidence_ref_id IS NULL OR evidence_comment.id IS NOT NULL)
   THEN trigger_comment.content END,
  'output',left(t.result->>'output',16000),'output_truncated',length(t.result->>'output')>16000,
  'issue_snapshot',t.issue_snapshot,'agent_id',t.agent_id,'agent_name',a.name,
  'started_at',t.started_at,'completed_at',t.completed_at,
  'models',COALESCE((SELECT jsonb_agg(DISTINCT u.model ORDER BY u.model) FROM task_usage u
   WHERE u.task_id=t.id AND u.model<>''),'[]'::jsonb)))
FROM agent_task_queue t JOIN issue i ON i.id=t.issue_id
LEFT JOIN agent a ON a.id=t.agent_id AND a.workspace_id=$1 AND a.kind='user'
 AND ($5::boolean OR a.owner_id=$4::uuid OR (a.permission_mode='public_to' AND EXISTS (
  SELECT 1 FROM agent_invocation_target target WHERE target.agent_id=a.id
  AND ((target.target_type='workspace' AND target.target_id=$1)
   OR (target.target_type='member' AND target.target_id=$4::uuid)))))
LEFT JOIN comment trigger_comment ON trigger_comment.id=t.trigger_comment_id AND trigger_comment.workspace_id=$1
 AND trigger_comment.deleted_at IS NULL AND EXISTS (SELECT 1 FROM issue ci WHERE ci.id=trigger_comment.issue_id
  AND ci.workspace_id=$1 AND NOT (COALESCE(ci.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[])))
LEFT JOIN comment evidence_comment ON t.trigger_evidence_kind='comment' AND evidence_comment.id=t.trigger_evidence_ref_id
 AND evidence_comment.workspace_id=$1 AND evidence_comment.deleted_at IS NULL
 AND EXISTS (SELECT 1 FROM issue ci WHERE ci.id=evidence_comment.issue_id
  AND ci.workspace_id=$1 AND NOT (COALESCE(ci.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[])))
WHERE i.workspace_id=$1 AND i.id=ANY($2::uuid[]) AND t.chat_session_id IS NULL
 AND NOT (COALESCE(i.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[]))
ORDER BY t.created_at,t.id LIMIT 1001`

const trajectoryGraphActivitiesSQL = `-- trajectory graph activities
SELECT jsonb_build_object('id','fact:'||a.id,'kind','platform_event','title',a.action,'status','recorded',
 'issue_id',a.issue_id,'created_at',a.created_at,
 'details',jsonb_build_object('event',a.action,'source_kind','platform_fact','actor_type',a.actor_type,
  'actor_id',a.actor_id,'activity_id',a.id,'record',a.details))
FROM activity_log a JOIN issue i ON i.id=a.issue_id AND i.workspace_id=a.workspace_id
WHERE a.workspace_id=$1 AND a.issue_id=ANY($2::uuid[])
 AND NOT (COALESCE(i.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[]))
ORDER BY a.created_at,a.id LIMIT 2001`

const trajectoryGraphCommentsSQL = `-- trajectory graph comments
SELECT jsonb_build_object('id','comment:'||c.id,'kind','comment','title',c.type,'status','recorded',
 'issue_id',c.issue_id,'run_id',bound.id,'created_at',c.created_at,
 'details',jsonb_build_object('comment_id',c.id,'content',c.content,'author_type',c.author_type,
  'author_id',c.author_id,'source_run_id',bound.id))
FROM comment c JOIN issue i ON i.id=c.issue_id AND i.workspace_id=c.workspace_id
LEFT JOIN agent_task_queue bound ON bound.id=c.source_task_id AND bound.chat_session_id IS NULL
 AND EXISTS (SELECT 1 FROM issue source WHERE source.id=bound.issue_id AND source.workspace_id=$1
  AND NOT (COALESCE(source.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[])))
WHERE c.workspace_id=$1 AND c.issue_id=ANY($2::uuid[]) AND c.deleted_at IS NULL
 AND NOT (COALESCE(i.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[]))
ORDER BY c.created_at,c.id LIMIT 2001`

const trajectoryGraphAttachmentsSQL = `-- trajectory graph attachments
SELECT jsonb_build_object('id','attachment:'||a.id,'kind','artifact','title',a.filename,'status','recorded',
 'issue_id',a.issue_id,'run_id',bound.id,'created_at',a.created_at,
 'details',jsonb_build_object('attachment_id',a.id,'comment_id',c.id,'content_type',a.content_type,
  'size_bytes',a.size_bytes,'source_run_id',bound.id,'source_kind','attachment'))
FROM attachment a JOIN issue i ON i.id=a.issue_id AND i.workspace_id=a.workspace_id
LEFT JOIN comment c ON c.id=a.comment_id AND c.workspace_id=a.workspace_id AND c.issue_id=a.issue_id AND c.deleted_at IS NULL
LEFT JOIN agent_task_queue bound ON bound.id=c.source_task_id AND bound.chat_session_id IS NULL
 AND EXISTS (SELECT 1 FROM issue source WHERE source.id=bound.issue_id AND source.workspace_id=$1
  AND NOT (COALESCE(source.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[])))
WHERE a.workspace_id=$1 AND a.issue_id=ANY($2::uuid[]) AND (a.comment_id IS NULL OR c.id IS NOT NULL)
 AND NOT (COALESCE(i.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[]))
ORDER BY a.created_at,a.id LIMIT 2001`

const trajectoryGraphNativeEdgesSQL = `-- trajectory graph native edges
SELECT jsonb_build_object('id','issue_dependency:'||d.id,
 'from','issue:'||CASE WHEN d.type='blocked_by' THEN d.depends_on_issue_id ELSE d.issue_id END,
 'to','issue:'||CASE WHEN d.type='blocked_by' THEN d.issue_id ELSE d.depends_on_issue_id END,
 'kind',CASE WHEN d.type='related' THEN 'issue_related' ELSE 'issue_blocks' END) AS edge
FROM issue_dependency d JOIN issue owner ON owner.id=d.issue_id AND owner.workspace_id=$1
 JOIN issue target ON target.id=d.depends_on_issue_id AND target.workspace_id=$1
WHERE d.issue_id=ANY($2::uuid[]) AND d.depends_on_issue_id=ANY($2::uuid[])
 AND NOT (COALESCE(owner.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[]))
 AND NOT (COALESCE(target.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[]))
UNION ALL
SELECT jsonb_build_object('id','issue_duplicate:'||i.id,'from','issue:'||i.id,
 'to','issue:'||i.duplicate_of_issue_id,'kind','issue_duplicate') AS edge
FROM issue i JOIN issue target ON target.id=i.duplicate_of_issue_id AND target.workspace_id=$1
WHERE i.workspace_id=$1 AND i.status='cancelled' AND i.id=ANY($2::uuid[]) AND i.duplicate_of_issue_id=ANY($2::uuid[])
 AND NOT (COALESCE(i.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[]))
 AND NOT (COALESCE(target.project_id, '` + noProjectSQL + `'::uuid)=ANY($3::uuid[]))
ORDER BY edge LIMIT 2001`

func readTrajectoryGraphNodes(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]trajectoryGraphNode, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []trajectoryGraphNode{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var node trajectoryGraphNode
		if err = json.Unmarshal(raw, &node); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func expandTrajectoryGraph(ctx context.Context, tx pgx.Tx, workspace pgtype.UUID, ids, hidden []pgtype.UUID) ([]pgtype.UUID, bool, error) {
	// Eight expansion hops; a ninth query only probes whether more remain.
	for hop := 0; len(ids) > 0; hop++ {
		rows, err := tx.Query(ctx, trajectoryGraphNeighborsSQL, workspace, ids, hidden)
		if err != nil {
			return nil, false, err
		}
		neighbors := []pgtype.UUID{}
		for rows.Next() {
			var id pgtype.UUID
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return nil, false, err
			}
			neighbors = append(neighbors, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, false, err
		}
		if len(neighbors) == 0 {
			return ids, false, nil
		}
		if hop == 8 {
			return ids, true, nil
		}
		for _, id := range neighbors {
			if len(ids) >= 200 {
				return ids, true, nil
			}
			ids = append(ids, id)
		}
	}
	return ids, false, nil
}
