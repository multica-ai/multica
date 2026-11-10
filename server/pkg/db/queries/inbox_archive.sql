-- Archived inbox filtering mirrors filterInboxItems (packages/core/inbox):
-- notification filters (type, sender, received) pick each group's newest
-- MATCHING notification, which is the row the list renders; issue filters and
-- "unread" then test that group. Issue filters never match a notification
-- without an issue, "is not" included. Type groups mirror INBOX_TYPE_GROUPS.

-- name: ListArchivedInboxPage :many
-- Select narrow group representatives before loading bodies or comment anchors.
-- Only types the Inbox lists (inbox.TurnTypes) are history here, and only an
-- active row of one of them keeps an issue out of this list.
WITH base AS MATERIALIZED (
    SELECT i.id, i.issue_id, COALESCE(i.issue_id, i.id) AS group_id, i.created_at, i.read,
           CASE
               WHEN i.type = 'mentioned' THEN 'mention'
               WHEN i.type IN ('new_comment', 'reaction_added') THEN 'comment'
               WHEN i.type IN ('issue_assigned', 'unassigned', 'assignee_changed', 'issue_subscribed') THEN 'assign'
               WHEN i.type = 'review_requested' THEN 'review'
               WHEN i.type IN ('status_changed', 'priority_changed', 'start_date_changed', 'due_date_changed', 'children_done') THEN 'change'
               WHEN i.type IN ('task_completed', 'task_failed', 'agent_blocked', 'agent_completed') THEN 'run'
               ELSE 'system'
           END AS type_group,
           CASE WHEN i.actor_type = 'system' THEN 'system'
                ELSE i.actor_type || ':' || i.actor_id::text END AS actor
    FROM inbox_item i
    WHERE i.workspace_id = sqlc.arg(workspace_id)::uuid
      AND i.recipient_type = 'member'
      AND i.recipient_id = sqlc.arg(recipient_id)::uuid
      AND i.archived = true
      AND i.type = ANY(sqlc.arg(turn_types)::text[])
      -- MUL-8472: private projects the caller cannot see.
      AND (sqlc.narg('hidden_project_ids')::uuid[] IS NULL OR i.issue_id IS NULL OR NOT EXISTS (
          SELECT 1 FROM issue hidden_issue
          WHERE hidden_issue.id = i.issue_id
            AND hidden_issue.project_id = ANY(sqlc.narg('hidden_project_ids')::uuid[])
      ))
      AND (sqlc.narg(group_id)::uuid IS NULL OR i.issue_id = sqlc.narg(group_id)::uuid
           OR (i.issue_id IS NULL AND i.id = sqlc.narg(group_id)::uuid))
      AND (i.issue_id IS NULL OR NOT EXISTS (
          SELECT 1 FROM inbox_item active
          WHERE active.workspace_id = i.workspace_id
            AND active.recipient_type = i.recipient_type
            AND active.recipient_id = i.recipient_id
            AND active.issue_id = i.issue_id AND active.archived = false
            AND active.type = ANY(sqlc.arg(turn_types)::text[])
      ))
), newest AS (
    SELECT DISTINCT ON (group_id) base.*
    FROM base
    WHERE (cardinality(sqlc.arg(types)::text[]) = 0
           OR (type_group = ANY(sqlc.arg(types)::text[])) <> ('type' = ANY(sqlc.arg(negated)::text[])))
      AND (cardinality(sqlc.arg(actors)::text[]) = 0
           OR COALESCE(actor = ANY(sqlc.arg(actors)::text[]), false) <> ('actor' = ANY(sqlc.arg(negated)::text[])))
      AND (sqlc.narg(received_after)::timestamptz IS NULL OR created_at >= sqlc.narg(received_after)::timestamptz)
    ORDER BY group_id, created_at DESC, id DESC
), projected AS (
    SELECT newest.id, newest.created_at, newest.read,
           iss.id AS issue_ref, iss.status AS issue_status, iss.priority AS issue_priority,
           COALESCE(iss.project_id::text, 'none') AS project_key,
           CASE WHEN iss.assignee_id IS NULL THEN 'none'
                ELSE iss.assignee_type || ':' || iss.assignee_id::text END AS assignee_key,
           iss.due_date,
           ARRAY(SELECT il.label_id::text FROM issue_to_label il
                 WHERE iss.id IS NOT NULL AND il.issue_id = iss.id)::text[] AS label_ids
    FROM newest
    LEFT JOIN issue iss ON iss.id = newest.issue_id
      AND iss.workspace_id = sqlc.arg(workspace_id)::uuid
), selected AS (
    SELECT projected.id, projected.created_at, projected.issue_status, projected.issue_priority
    FROM projected
    WHERE (cardinality(sqlc.arg(statuses)::text[]) = 0 OR (issue_ref IS NOT NULL
           AND (issue_status = ANY(sqlc.arg(statuses)::text[])) <> ('status' = ANY(sqlc.arg(negated)::text[]))))
      AND (cardinality(sqlc.arg(priorities)::text[]) = 0 OR (issue_ref IS NOT NULL
           AND (issue_priority = ANY(sqlc.arg(priorities)::text[])) <> ('priority' = ANY(sqlc.arg(negated)::text[]))))
      AND (cardinality(sqlc.arg(projects)::text[]) = 0 OR (issue_ref IS NOT NULL
           AND (project_key = ANY(sqlc.arg(projects)::text[])) <> ('project' = ANY(sqlc.arg(negated)::text[]))))
      AND (cardinality(sqlc.arg(assignees)::text[]) = 0 OR (issue_ref IS NOT NULL
           AND (assignee_key = ANY(sqlc.arg(assignees)::text[])) <> ('assignee' = ANY(sqlc.arg(negated)::text[]))))
      AND (cardinality(sqlc.arg(labels)::text[]) = 0 OR (issue_ref IS NOT NULL
           AND (label_ids && sqlc.arg(labels)::text[]) <> ('label' = ANY(sqlc.arg(negated)::text[]))))
      AND (cardinality(sqlc.arg(due_dates)::text[]) = 0 OR (issue_ref IS NOT NULL AND COALESCE(
               ('overdue' = ANY(sqlc.arg(due_dates)::text[]) AND due_date < sqlc.narg(today)::date)
            OR ('today' = ANY(sqlc.arg(due_dates)::text[]) AND due_date = sqlc.narg(today)::date)
            OR ('this_week' = ANY(sqlc.arg(due_dates)::text[])
                AND due_date >= sqlc.narg(today)::date - (EXTRACT(ISODOW FROM sqlc.narg(today)::date)::int - 1)
                AND due_date < sqlc.narg(today)::date - (EXTRACT(ISODOW FROM sqlc.narg(today)::date)::int - 1) + 7)
            OR ('next_7_days' = ANY(sqlc.arg(due_dates)::text[])
                AND due_date >= sqlc.narg(today)::date AND due_date < sqlc.narg(today)::date + 7)
            OR ('none' = ANY(sqlc.arg(due_dates)::text[]) AND due_date IS NULL), false)
           <> ('dueDate' = ANY(sqlc.arg(negated)::text[]))))
      AND (NOT sqlc.arg(unread_only)::boolean OR NOT read)
      AND (sqlc.narg(before_time)::timestamptz IS NULL OR
           (created_at, id) < (sqlc.narg(before_time)::timestamptz, sqlc.narg(before_id)::uuid))
    ORDER BY created_at DESC, id DESC
    LIMIT sqlc.arg(page_limit)::int
)
SELECT sqlc.embed(i), selected.issue_status, selected.issue_priority,
       iss.project_id AS issue_project_id,
       iss.assignee_type AS issue_assignee_type,
       iss.assignee_id AS issue_assignee_id,
       iss.due_date AS issue_due_date,
       ARRAY(SELECT il.label_id::text FROM issue_to_label il
             WHERE iss.id IS NOT NULL AND il.issue_id = iss.id
             ORDER BY il.label_id)::text[] AS issue_label_ids,
       -- The notification's own comment wins: a filter can pick an older row
       -- (a mention of C1) under a newer comment (C2). The group's newest
       -- anchor only fills rows that point at no comment.
       COALESCE(NULLIF(i.details->>'comment_id', ''), anchor.comment_id, '')::text AS comment_id
FROM selected
JOIN inbox_item i ON i.id = selected.id
LEFT JOIN issue iss ON iss.id = i.issue_id AND iss.workspace_id = i.workspace_id
LEFT JOIN LATERAL (
    SELECT a.details->>'comment_id' AS comment_id
    FROM inbox_item a
    WHERE i.issue_id IS NOT NULL AND a.issue_id = i.issue_id
      AND a.workspace_id = i.workspace_id AND a.recipient_type = i.recipient_type
      AND a.recipient_id = i.recipient_id AND a.archived = true
      AND NULLIF(a.details->>'comment_id', '') IS NOT NULL
    ORDER BY a.created_at DESC, a.id DESC LIMIT 1
) anchor ON true
ORDER BY i.created_at DESC, i.id DESC;

-- name: ArchivedInboxFacets :many
-- Every count ignores its own dimension, so it says how many rows selecting
-- that value alone would show. Type and sender counts pick the newest
-- notification of that value in each group, the row selecting it renders.
WITH base AS MATERIALIZED (
    SELECT i.id, i.issue_id, COALESCE(i.issue_id, i.id) AS group_id, i.created_at, i.read,
           CASE
               WHEN i.type = 'mentioned' THEN 'mention'
               WHEN i.type IN ('new_comment', 'reaction_added') THEN 'comment'
               WHEN i.type IN ('issue_assigned', 'unassigned', 'assignee_changed', 'issue_subscribed') THEN 'assign'
               WHEN i.type = 'review_requested' THEN 'review'
               WHEN i.type IN ('status_changed', 'priority_changed', 'start_date_changed', 'due_date_changed', 'children_done') THEN 'change'
               WHEN i.type IN ('task_completed', 'task_failed', 'agent_blocked', 'agent_completed') THEN 'run'
               ELSE 'system'
           END AS type_group,
           CASE WHEN i.actor_type = 'system' THEN 'system'
                ELSE i.actor_type || ':' || i.actor_id::text END AS actor
    FROM inbox_item i
    WHERE i.workspace_id = sqlc.arg(workspace_id)::uuid
      AND i.recipient_type = 'member'
      AND i.recipient_id = sqlc.arg(recipient_id)::uuid
      AND i.archived = true
      AND i.type = ANY(sqlc.arg(turn_types)::text[])
      -- MUL-8472: private projects the caller cannot see.
      AND (sqlc.narg('hidden_project_ids')::uuid[] IS NULL OR i.issue_id IS NULL OR NOT EXISTS (
          SELECT 1 FROM issue hidden_issue
          WHERE hidden_issue.id = i.issue_id
            AND hidden_issue.project_id = ANY(sqlc.narg('hidden_project_ids')::uuid[])
      ))
      AND (i.issue_id IS NULL OR NOT EXISTS (
          SELECT 1 FROM inbox_item active
          WHERE active.workspace_id = i.workspace_id
            AND active.recipient_type = i.recipient_type
            AND active.recipient_id = i.recipient_id
            AND active.issue_id = i.issue_id AND active.archived = false
            AND active.type = ANY(sqlc.arg(turn_types)::text[])
      ))
), notification_match AS MATERIALIZED (
    SELECT base.*,
           (cardinality(sqlc.arg(types)::text[]) = 0
            OR (type_group = ANY(sqlc.arg(types)::text[])) <> ('type' = ANY(sqlc.arg(negated)::text[]))) AS type_match,
           (cardinality(sqlc.arg(actors)::text[]) = 0
            OR COALESCE(actor = ANY(sqlc.arg(actors)::text[]), false) <> ('actor' = ANY(sqlc.arg(negated)::text[]))) AS actor_match,
           (sqlc.narg(received_after)::timestamptz IS NULL OR created_at >= sqlc.narg(received_after)::timestamptz) AS received_match
    FROM base
), groups AS (
    SELECT DISTINCT group_id, issue_id FROM base
), projected AS (
    SELECT groups.group_id,
           iss.id AS issue_ref, iss.status AS issue_status, iss.priority AS issue_priority,
           COALESCE(iss.project_id::text, 'none') AS project_key,
           CASE WHEN iss.assignee_id IS NULL THEN 'none'
                ELSE iss.assignee_type || ':' || iss.assignee_id::text END AS assignee_key,
           iss.due_date,
           ARRAY(SELECT il.label_id::text FROM issue_to_label il
                 WHERE iss.id IS NOT NULL AND il.issue_id = iss.id)::text[] AS label_ids
    FROM groups
    LEFT JOIN issue iss ON iss.id = groups.issue_id
      AND iss.workspace_id = sqlc.arg(workspace_id)::uuid
), issue_match AS MATERIALIZED (
    SELECT projected.*,
           (cardinality(sqlc.arg(statuses)::text[]) = 0 OR (issue_ref IS NOT NULL
            AND (issue_status = ANY(sqlc.arg(statuses)::text[])) <> ('status' = ANY(sqlc.arg(negated)::text[])))) AS status_match,
           (cardinality(sqlc.arg(priorities)::text[]) = 0 OR (issue_ref IS NOT NULL
            AND (issue_priority = ANY(sqlc.arg(priorities)::text[])) <> ('priority' = ANY(sqlc.arg(negated)::text[])))) AS priority_match,
           (cardinality(sqlc.arg(projects)::text[]) = 0 OR (issue_ref IS NOT NULL
            AND (project_key = ANY(sqlc.arg(projects)::text[])) <> ('project' = ANY(sqlc.arg(negated)::text[])))) AS project_match,
           (cardinality(sqlc.arg(assignees)::text[]) = 0 OR (issue_ref IS NOT NULL
            AND (assignee_key = ANY(sqlc.arg(assignees)::text[])) <> ('assignee' = ANY(sqlc.arg(negated)::text[])))) AS assignee_match,
           (cardinality(sqlc.arg(labels)::text[]) = 0 OR (issue_ref IS NOT NULL
            AND (label_ids && sqlc.arg(labels)::text[]) <> ('label' = ANY(sqlc.arg(negated)::text[])))) AS label_match,
           (cardinality(sqlc.arg(due_dates)::text[]) = 0 OR (issue_ref IS NOT NULL AND COALESCE(
                ('overdue' = ANY(sqlc.arg(due_dates)::text[]) AND due_date < sqlc.narg(today)::date)
             OR ('today' = ANY(sqlc.arg(due_dates)::text[]) AND due_date = sqlc.narg(today)::date)
             OR ('this_week' = ANY(sqlc.arg(due_dates)::text[])
                 AND due_date >= sqlc.narg(today)::date - (EXTRACT(ISODOW FROM sqlc.narg(today)::date)::int - 1)
                 AND due_date < sqlc.narg(today)::date - (EXTRACT(ISODOW FROM sqlc.narg(today)::date)::int - 1) + 7)
             OR ('next_7_days' = ANY(sqlc.arg(due_dates)::text[])
                 AND due_date >= sqlc.narg(today)::date AND due_date < sqlc.narg(today)::date + 7)
             OR ('none' = ANY(sqlc.arg(due_dates)::text[]) AND due_date IS NULL), false)
            <> ('dueDate' = ANY(sqlc.arg(negated)::text[])))) AS due_match
    FROM projected
), reps AS (
    SELECT DISTINCT ON (group_id) group_id, read
    FROM notification_match
    WHERE type_match AND actor_match AND received_match
    ORDER BY group_id, created_at DESC, id DESC
), matched AS (
    SELECT issue_match.*, (NOT sqlc.arg(unread_only)::boolean OR NOT reps.read) AS read_match, reps.read
    FROM reps JOIN issue_match USING (group_id)
), type_reps AS (
    SELECT DISTINCT ON (group_id, type_group) group_id, type_group, read
    FROM notification_match
    WHERE actor_match AND received_match
    ORDER BY group_id, type_group, created_at DESC, id DESC
), actor_reps AS (
    SELECT DISTINCT ON (group_id, actor) group_id, actor, read
    FROM notification_match
    WHERE type_match AND received_match AND actor IS NOT NULL
    ORDER BY group_id, actor, created_at DESC, id DESC
)
SELECT 'statuses'::text AS dimension, issue_status::text AS key,
       count(*) FILTER (WHERE priority_match AND project_match AND assignee_match AND label_match AND due_match AND read_match)::bigint AS count
FROM matched WHERE issue_status IS NOT NULL GROUP BY issue_status
UNION ALL
SELECT 'priorities'::text, issue_priority::text,
       count(*) FILTER (WHERE status_match AND project_match AND assignee_match AND label_match AND due_match AND read_match)::bigint
FROM matched WHERE issue_priority IS NOT NULL GROUP BY issue_priority
UNION ALL
SELECT 'projects'::text, project_key::text,
       count(*) FILTER (WHERE status_match AND priority_match AND assignee_match AND label_match AND due_match AND read_match)::bigint
FROM matched WHERE issue_ref IS NOT NULL GROUP BY project_key
UNION ALL
SELECT 'assignees'::text, assignee_key::text,
       count(*) FILTER (WHERE status_match AND priority_match AND project_match AND label_match AND due_match AND read_match)::bigint
FROM matched WHERE issue_ref IS NOT NULL GROUP BY assignee_key
UNION ALL
SELECT 'labels'::text, label::text,
       count(*) FILTER (WHERE status_match AND priority_match AND project_match AND assignee_match AND due_match AND read_match)::bigint
FROM matched, unnest(label_ids) AS label GROUP BY label
UNION ALL
SELECT 'types'::text, type_reps.type_group::text,
       count(*) FILTER (WHERE status_match AND priority_match AND project_match AND assignee_match AND label_match AND due_match
                        AND (NOT sqlc.arg(unread_only)::boolean OR NOT type_reps.read))::bigint
FROM type_reps JOIN issue_match USING (group_id) GROUP BY type_reps.type_group
UNION ALL
SELECT 'actors'::text, actor_reps.actor::text,
       count(*) FILTER (WHERE status_match AND priority_match AND project_match AND assignee_match AND label_match AND due_match
                        AND (NOT sqlc.arg(unread_only)::boolean OR NOT actor_reps.read))::bigint
FROM actor_reps JOIN issue_match USING (group_id) GROUP BY actor_reps.actor
UNION ALL
SELECT 'unread'::text, 'unread'::text,
       count(*) FILTER (WHERE status_match AND priority_match AND project_match AND assignee_match AND label_match AND due_match AND NOT read)::bigint
FROM matched;
