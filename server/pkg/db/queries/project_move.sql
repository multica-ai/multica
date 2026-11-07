-- name: LockProjectMoveWorkspaces :many
-- Serialize moves/counter allocation without blocking ordinary workspace FK
-- KEY SHARE. This is not a fence for ordinary writers or late task enqueue.
SELECT id FROM workspace WHERE id=ANY(@workspace_ids::uuid[]) ORDER BY id FOR NO KEY UPDATE;

-- name: LockProjectMoveIssues :many
SELECT * FROM issue WHERE project_id=sqlc.arg('project_id')::uuid AND workspace_id=sqlc.arg('source_workspace_id')::uuid ORDER BY id FOR UPDATE;

-- name: GetProjectMoveState :one
-- One snapshot supplies both the deterministic plan and its revision digest.
WITH p AS (SELECT @project_id::uuid AS project, @source_workspace_id::uuid AS source, @target_workspace_id::uuid AS target),
moving AS (SELECT i.* FROM issue i,p WHERE i.project_id=p.project AND i.workspace_id=p.source),
deleted_views AS (SELECT r.* FROM issue_view r,p WHERE r.workspace_id=p.source AND (r.scope_id=p.project OR r.scope_id IN (SELECT id FROM moving) OR r.query::text LIKE '%'||p.project::text||'%' OR EXISTS (SELECT 1 FROM moving i WHERE r.query::text LIKE '%'||i.id::text||'%'))),
blockers AS (
SELECT 'active_task'::text AS kind,t.issue_id,t.id AS resource_id FROM agent_task_queue t WHERE t.issue_id IN (SELECT id FROM moving) AND t.status NOT IN ('completed','failed','cancelled')
UNION ALL
SELECT 'enabled_wakeup',w.issue_id,w.id FROM issue_wakeup w WHERE w.issue_id IN (SELECT id FROM moving) AND w.enabled
UNION ALL
SELECT 'autopilot',NULL::uuid,a.id FROM autopilot a,p WHERE a.project_id=p.project
UNION ALL
SELECT 'local_directory',NULL::uuid,r.id FROM project_resource r,p WHERE r.project_id=p.project AND r.resource_type='local_directory'
UNION ALL
SELECT 'open_github_pr',l.issue_id,l.pull_request_id FROM issue_pull_request l JOIN github_pull_request pr ON pr.id=l.pull_request_id WHERE l.issue_id IN (SELECT id FROM moving) AND pr.state NOT IN ('closed','merged')
UNION ALL
SELECT 'open_vcs_pr',l.issue_id,l.pull_request_id FROM issue_vcs_pull_request l JOIN vcs_pull_request pr ON pr.id=l.pull_request_id WHERE l.issue_id IN (SELECT id FROM moving) AND pr.state NOT IN ('closed','merged')
UNION ALL
SELECT 'parent_link',i.id,i.parent_issue_id FROM issue i WHERE i.parent_issue_id IS NOT NULL AND (i.id IN (SELECT id FROM moving))<>(i.parent_issue_id IN (SELECT id FROM moving))
UNION ALL
SELECT 'duplicate_link',i.id,i.duplicate_of_issue_id FROM issue i WHERE i.duplicate_of_issue_id IS NOT NULL AND (i.id IN (SELECT id FROM moving))<>(i.duplicate_of_issue_id IN (SELECT id FROM moving))
UNION ALL
SELECT 'dependency_link',d.issue_id,d.depends_on_issue_id FROM issue_dependency d WHERE (d.issue_id IN (SELECT id FROM moving))<>(d.depends_on_issue_id IN (SELECT id FROM moving))
UNION ALL
SELECT 'source_context_link',c.issue_id,c.source_issue_id FROM issue_source_context c WHERE COALESCE(c.issue_id IN (SELECT id FROM moving),false)<>(c.source_issue_id IN (SELECT id FROM moving)))
SELECT (jsonb_build_object(
 'project',(SELECT to_jsonb(r) FROM project r WHERE r.id=p.project AND r.workspace_id=p.source),
 'source',(SELECT to_jsonb(r) FROM workspace r WHERE r.id=p.source),
 'target',(SELECT to_jsonb(r) FROM workspace r WHERE r.id=p.target),
 'issues',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM moving r WHERE true), '[]'::jsonb),
 'members',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM member r WHERE workspace_id IN (p.source,p.target)), '[]'::jsonb),
 'statuses',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM issue_status r WHERE workspace_id IN (p.source,p.target)), '[]'::jsonb),
 'properties',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM issue_property r WHERE workspace_id IN (p.source,p.target)), '[]'::jsonb),
 'target_labels',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM issue_label r WHERE workspace_id=p.target AND resource_type='issue'), '[]'::jsonb),
 'target_numbers',COALESCE((SELECT jsonb_agg(number ORDER BY number) FROM issue WHERE workspace_id=p.target),'[]'::jsonb),
 'blockers',COALESCE((SELECT jsonb_agg(to_jsonb(b) ORDER BY kind,issue_id,resource_id) FROM blockers b),'[]'::jsonb)) || jsonb_build_object(
 'comments',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM comment r WHERE issue_id IN (SELECT id FROM moving)), '[]'::jsonb),
 'attachments',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM attachment r WHERE issue_id IN (SELECT id FROM moving) OR comment_id IN (SELECT id FROM comment WHERE issue_id IN (SELECT id FROM moving)) OR source_context_id IN (SELECT id FROM issue_source_context WHERE issue_id IN (SELECT id FROM moving))), '[]'::jsonb),
 'runs',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM agent_task_queue r WHERE issue_id IN (SELECT id FROM moving)), '[]'::jsonb),
 'labels',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM issue_label r WHERE id IN (SELECT label_id FROM issue_to_label WHERE issue_id IN (SELECT id FROM moving))), '[]'::jsonb),
 'label_links',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM issue_to_label r WHERE issue_id IN (SELECT id FROM moving)), '[]'::jsonb),
 'subscribers',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM issue_subscriber r WHERE issue_id IN (SELECT id FROM moving)), '[]'::jsonb),
 'pins',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM pinned_item r WHERE workspace_id=p.source AND (item_id=p.project OR item_id IN (SELECT id FROM moving) OR (item_type='view' AND item_id IN (SELECT id FROM deleted_views)))), '[]'::jsonb),
 'views',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM deleted_views r), '[]'::jsonb),
 'view_preferences',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM issue_view_preference r WHERE workspace_id=p.source AND (scope_id=p.project OR scope_id IN (SELECT id FROM moving) OR prefs::text LIKE '%'||p.project::text||'%' OR EXISTS (SELECT 1 FROM moving i WHERE prefs::text LIKE '%'||i.id::text||'%'))), '[]'::jsonb),
 'inbox_items',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM inbox_item r WHERE issue_id IN (SELECT id FROM moving)), '[]'::jsonb),
 'unread_marks',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM issue_unread r WHERE issue_id IN (SELECT id FROM moving)), '[]'::jsonb),
 'chat_contexts',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM chat_session r WHERE project_id=p.project), '[]'::jsonb),
 'wakeups',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM issue_wakeup r WHERE issue_id IN (SELECT id FROM moving)), '[]'::jsonb),
 'wakeup_receipts',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM issue_wakeup_receipt r WHERE wakeup_id IN (SELECT id FROM issue_wakeup WHERE issue_id IN (SELECT id FROM moving))), '[]'::jsonb),
 'pr_links',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM issue_pull_request r WHERE issue_id IN (SELECT id FROM moving)), '[]'::jsonb),
 'vcs_pr_links',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM issue_vcs_pull_request r WHERE issue_id IN (SELECT id FROM moving)), '[]'::jsonb),
 'source_contexts',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM issue_source_context r WHERE issue_id IN (SELECT id FROM moving)), '[]'::jsonb),
 'resources',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM project_resource r WHERE project_id=p.project), '[]'::jsonb),
 'aliases',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM issue_key_alias r WHERE issue_id IN (SELECT id FROM moving)), '[]'::jsonb)) )::text AS state FROM p;

-- name: RehomeProjectMoveData :exec
-- Explicit application-owned graph; rows without workspace_id retain identity.
WITH p AS (SELECT @source_workspace_id::uuid AS source,@target_workspace_id::uuid AS target,@project_id::uuid AS project,@issue_ids::uuid[] AS issues),
m_project_resource AS (UPDATE project_resource r SET workspace_id=p.target FROM p WHERE r.workspace_id=p.source AND (project_id=p.project) RETURNING 1),
m_comment AS (UPDATE comment r SET workspace_id=p.target FROM p WHERE r.workspace_id=p.source AND (issue_id=ANY(p.issues)) RETURNING 1),
m_comment_reaction AS (UPDATE comment_reaction r SET workspace_id=p.target FROM p WHERE r.workspace_id=p.source AND (comment_id IN (SELECT id FROM comment WHERE issue_id=ANY(p.issues))) RETURNING 1),
m_issue_reaction AS (UPDATE issue_reaction r SET workspace_id=p.target FROM p WHERE r.workspace_id=p.source AND (issue_id=ANY(p.issues)) RETURNING 1),
m_attachment AS (UPDATE attachment r SET workspace_id=p.target FROM p WHERE r.workspace_id=p.source AND (issue_id=ANY(p.issues) OR comment_id IN (SELECT id FROM comment WHERE issue_id=ANY(p.issues)) OR source_context_id IN (SELECT id FROM issue_source_context WHERE issue_id=ANY(p.issues))) RETURNING 1),
m_activity_log AS (UPDATE activity_log r SET workspace_id=p.target FROM p WHERE r.workspace_id=p.source AND (issue_id=ANY(p.issues)) RETURNING 1),
m_issue_child_event AS (UPDATE issue_child_event r SET workspace_id=p.target FROM p WHERE r.workspace_id=p.source AND (parent_id=ANY(p.issues) OR child_id=ANY(p.issues)) RETURNING 1),
m_issue_pr_automation AS (UPDATE issue_pr_automation r SET workspace_id=p.target FROM p WHERE r.workspace_id=p.source AND (issue_id=ANY(p.issues)) RETURNING 1),
m_issue_pull_request_exclusion AS (UPDATE issue_pull_request_exclusion r SET workspace_id=p.target FROM p WHERE r.workspace_id=p.source AND (issue_id=ANY(p.issues)) RETURNING 1),
m_issue_source_context AS (UPDATE issue_source_context r SET workspace_id=p.target FROM p WHERE r.workspace_id=p.source AND (issue_id=ANY(p.issues)) RETURNING 1),
m_issue_source_context_object_intent AS (UPDATE issue_source_context_object_intent r SET workspace_id=p.target FROM p WHERE r.workspace_id=p.source AND (source_context_id IN (SELECT id FROM issue_source_context WHERE issue_id=ANY(p.issues))) RETURNING 1),
m_task_supplement AS (UPDATE task_supplement r SET workspace_id=p.target FROM p WHERE r.workspace_id=p.source AND (issue_id=ANY(p.issues)) RETURNING 1),
m_task_supplement_capability AS (UPDATE task_supplement_capability r SET workspace_id=p.target FROM p WHERE r.workspace_id=p.source AND (issue_id=ANY(p.issues)) RETURNING 1),
d_views AS (DELETE FROM issue_view r USING p WHERE r.workspace_id=p.source AND (r.scope_id=p.project OR r.scope_id=ANY(p.issues) OR r.query::text LIKE '%'||p.project::text||'%' OR EXISTS (SELECT 1 FROM unnest(p.issues) i WHERE r.query::text LIKE '%'||i::text||'%')) RETURNING r.id),
-- Consume deleted view IDs so view locks precede their pin cleanup, just as
-- ordinary DeleteIssueView does. Unrelated view pins remain untouched.
d_pins AS (DELETE FROM pinned_item r USING p WHERE r.workspace_id=p.source AND (r.item_id=p.project OR r.item_id=ANY(p.issues) OR (r.item_type='view' AND r.item_id IN (SELECT id FROM d_views))) RETURNING 1),
d_preferences AS (DELETE FROM issue_view_preference r USING p WHERE r.workspace_id=p.source AND (r.scope_id=p.project OR r.scope_id=ANY(p.issues) OR r.prefs::text LIKE '%'||p.project::text||'%' OR EXISTS (SELECT 1 FROM unnest(p.issues) i WHERE r.prefs::text LIKE '%'||i::text||'%')) RETURNING 1),
d_inbox AS (DELETE FROM inbox_item r USING p WHERE r.workspace_id=p.source AND r.issue_id=ANY(p.issues) RETURNING 1),
d_unread AS (DELETE FROM issue_unread r USING p WHERE r.workspace_id=p.source AND r.issue_id=ANY(p.issues) RETURNING 1),
d_subscribers AS (DELETE FROM issue_subscriber r USING p WHERE r.issue_id=ANY(p.issues) AND (r.user_type<>'member' OR NOT EXISTS (SELECT 1 FROM member m WHERE m.workspace_id=p.target AND m.user_id=r.user_id)) RETURNING 1),
d_wakeups AS (DELETE FROM issue_wakeup r USING p WHERE r.workspace_id=p.source AND r.issue_id=ANY(p.issues) AND NOT r.enabled RETURNING id),
d_receipts AS (DELETE FROM issue_wakeup_receipt r WHERE r.wakeup_id IN (SELECT id FROM d_wakeups) RETURNING 1),
d_github_links AS (DELETE FROM issue_pull_request r USING p WHERE r.issue_id=ANY(p.issues) RETURNING 1),
d_vcs_links AS (DELETE FROM issue_vcs_pull_request r USING p WHERE r.issue_id=ANY(p.issues) RETURNING 1),
clear_chats AS (UPDATE chat_session r SET project_id=NULL FROM p WHERE r.workspace_id=p.source AND r.project_id=p.project RETURNING 1)
SELECT 1;

-- name: MapProjectMoveLabel :exec
WITH copied AS (
 INSERT INTO issue_label(id,workspace_id,name,color,description,resource_type)
 SELECT @target_label_id::uuid,@target_workspace_id::uuid,name,color,description,'issue'
 FROM issue_label WHERE id= @source_label_id::uuid AND workspace_id= @source_workspace_id::uuid
 AND NOT EXISTS (SELECT 1 FROM issue_label WHERE id= @target_label_id::uuid)
 RETURNING id
), removed AS (
 DELETE FROM issue_to_label WHERE label_id= @source_label_id::uuid AND issue_id=ANY(@issue_ids::uuid[]) RETURNING issue_id
)
INSERT INTO issue_to_label(issue_id,label_id)
SELECT issue_id,@target_label_id::uuid FROM removed ON CONFLICT DO NOTHING;

-- name: AllocateProjectMoveNumbers :exec
UPDATE workspace SET issue_counter=issue_counter+ @count::integer WHERE id= @target_workspace_id::uuid;

-- name: MoveProjectIssue :execrows
UPDATE issue SET workspace_id= @target_workspace_id::uuid,number= @target_number::integer,
 status= @target_status::text,properties= @properties::jsonb,
 assignee_type=CASE WHEN @clear_assignee::boolean THEN NULL ELSE assignee_type END,
 assignee_id=CASE WHEN @clear_assignee::boolean THEN NULL ELSE assignee_id END,
 updated_at=now(),revision=revision+1
WHERE id= @issue_id::uuid AND workspace_id= @source_workspace_id::uuid;

-- name: RecordLeavingIssueAlias :exec
INSERT INTO issue_key_alias(source_workspace_id,prefix,number,issue_id)
VALUES (@source_workspace_id::uuid,@prefix::text,@number::integer,@issue_id::uuid);

-- name: ConsumeReturningIssueAlias :exec
DELETE FROM issue_key_alias WHERE source_workspace_id= @target_workspace_id::uuid
 AND issue_id = @issue_id::uuid;

-- name: MoveProjectRoot :execrows
-- workflow_id names a source-workspace workflow; the target's Default applies.
UPDATE project SET workspace_id= @target_workspace_id::uuid,workflow_id=NULL,
 lead_type=CASE WHEN @clear_lead::boolean THEN NULL ELSE lead_type END,
 lead_id=CASE WHEN @clear_lead::boolean THEN NULL ELSE lead_id END,updated_at=now()
WHERE id= @project_id::uuid AND workspace_id= @source_workspace_id::uuid;


-- name: ResolveIssueMove :one
SELECT i.id,i.workspace_id,i.number,w.slug,w.issue_prefix,w.name,a.moved_at
FROM issue_key_alias a JOIN issue i ON i.id=a.issue_id JOIN workspace w ON w.id=i.workspace_id
WHERE a.source_workspace_id = @source_workspace_id::uuid AND a.prefix = @prefix::text AND a.number = @number::integer;

-- name: ResolveProjectMove :one
SELECT p.id,p.workspace_id,w.slug,m.moved_at
FROM project_move m JOIN project p ON p.id=m.project_id JOIN workspace w ON w.id=p.workspace_id
WHERE m.source_workspace_id = @source_workspace_id::uuid AND m.project_id = @project_id::uuid
 AND p.workspace_id <> @source_workspace_id::uuid
ORDER BY m.moved_at DESC,m.id DESC LIMIT 1;

-- name: RecordProjectMove :exec
INSERT INTO project_move(project_id,source_workspace_id,target_workspace_id,moved_by,summary)
VALUES (@project_id::uuid,@source_workspace_id::uuid,@target_workspace_id::uuid,@moved_by::uuid,@summary::jsonb);
