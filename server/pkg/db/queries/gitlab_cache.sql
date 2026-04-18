-- gitlab_label CRUD --------------------------------------------------------

-- name: UpsertGitlabLabel :one
INSERT INTO gitlab_label (
    workspace_id, gitlab_label_id, name, color, description, external_updated_at
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, gitlab_label_id) DO UPDATE SET
    name = EXCLUDED.name,
    color = EXCLUDED.color,
    description = EXCLUDED.description,
    external_updated_at = EXCLUDED.external_updated_at
RETURNING *;

-- name: ListGitlabLabels :many
SELECT * FROM gitlab_label
WHERE workspace_id = $1
ORDER BY name;

-- name: GetGitlabLabelByName :one
SELECT * FROM gitlab_label
WHERE workspace_id = $1 AND name = $2;

-- name: DeleteWorkspaceGitlabLabels :exec
DELETE FROM gitlab_label WHERE workspace_id = $1;

-- name: DeleteGitlabLabel :exec
DELETE FROM gitlab_label
WHERE workspace_id = $1 AND gitlab_label_id = $2;

-- issue_gitlab_label (issue ↔ label association) --------------------------

-- name: ClearIssueLabels :exec
DELETE FROM issue_gitlab_label WHERE issue_id = $1;

-- name: AddIssueLabels :exec
INSERT INTO issue_gitlab_label (issue_id, workspace_id, gitlab_label_id)
SELECT $1, $2, unnest(sqlc.arg(label_ids)::bigint[])
ON CONFLICT DO NOTHING;

-- name: ListIssueGitlabLabels :many
SELECT l.*
FROM gitlab_label l
JOIN issue_gitlab_label il ON il.workspace_id = l.workspace_id
                          AND il.gitlab_label_id = l.gitlab_label_id
WHERE il.issue_id = $1
ORDER BY l.name;

-- gitlab_project_member ----------------------------------------------------

-- name: UpsertGitlabProjectMember :one
INSERT INTO gitlab_project_member (
    workspace_id, gitlab_user_id, username, name, avatar_url, external_updated_at
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, gitlab_user_id) DO UPDATE SET
    username = EXCLUDED.username,
    name = EXCLUDED.name,
    avatar_url = EXCLUDED.avatar_url,
    external_updated_at = EXCLUDED.external_updated_at
RETURNING *;

-- name: ListGitlabProjectMembers :many
SELECT * FROM gitlab_project_member
WHERE workspace_id = $1
ORDER BY username;

-- name: GetGitlabProjectMember :one
-- Reverse lookup of a GitLab user in a workspace's cached project-member list.
SELECT * FROM gitlab_project_member
WHERE workspace_id = $1 AND gitlab_user_id = $2
LIMIT 1;

-- name: GetGitlabProjectMemberByID :one
-- Lookup by the UUID id (used when resolving an issue.assignee_id where
-- assignee_type='gitlab_user').
SELECT * FROM gitlab_project_member WHERE id = $1 LIMIT 1;

-- name: DeleteWorkspaceGitlabMembers :exec
DELETE FROM gitlab_project_member WHERE workspace_id = $1;

-- issue cache upserts ------------------------------------------------------

-- name: UpsertIssueFromGitlab :one
-- On INSERT: atomically allocate issue.number by incrementing the workspace's
-- issue_counter. The `next_num` CTE only fires when `existing` is empty
-- (new row), so re-upserts of an already-cached issue don't consume counter
-- values. On UPDATE: `number` is omitted from the SET clause so the value
-- assigned on first insert is preserved.
--
-- Without this, INSERTs fell back to the column DEFAULT of 0 and the second
-- GitLab-synced issue in any workspace collided on uq_issue_workspace_number.
WITH existing AS (
    SELECT number FROM issue
    WHERE workspace_id = $1 AND gitlab_iid = $2
),
next_num AS (
    UPDATE workspace SET issue_counter = issue_counter + 1
    WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM existing)
    RETURNING issue_counter
),
assigned AS (
    SELECT COALESCE(
        (SELECT number FROM existing),
        (SELECT issue_counter FROM next_num)
    )::int AS num
)
INSERT INTO issue (
    workspace_id,
    gitlab_iid,
    gitlab_project_id,
    gitlab_issue_id,
    title,
    description,
    status,
    priority,
    assignee_type,
    assignee_id,
    creator_type,
    creator_id,
    due_date,
    external_updated_at,
    number
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, (SELECT num FROM assigned))
ON CONFLICT (workspace_id, gitlab_iid) WHERE gitlab_iid IS NOT NULL DO UPDATE SET
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    status = EXCLUDED.status,
    priority = EXCLUDED.priority,
    assignee_type = EXCLUDED.assignee_type,
    assignee_id = EXCLUDED.assignee_id,
    gitlab_project_id = EXCLUDED.gitlab_project_id,
    gitlab_issue_id = EXCLUDED.gitlab_issue_id,
    due_date = EXCLUDED.due_date,
    external_updated_at = EXCLUDED.external_updated_at,
    updated_at = now()
WHERE issue.external_updated_at IS NULL
   OR issue.external_updated_at < EXCLUDED.external_updated_at
RETURNING *;

-- name: GetIssueByGitlabIID :one
SELECT * FROM issue
WHERE workspace_id = $1 AND gitlab_iid = $2;

-- name: GetIssueByGitlabID :one
-- GitLab's Emoji Hook payload uses awardable_id which is the GLOBAL issue
-- id (not the per-project IID). This query resolves an emoji event's
-- awardable_id back to the cached issue row.
SELECT * FROM issue
WHERE workspace_id = $1 AND gitlab_issue_id = $2;

-- name: DeleteWorkspaceCachedIssues :exec
DELETE FROM issue WHERE workspace_id = $1 AND gitlab_iid IS NOT NULL;

-- name: ListCachedGitlabIssues :many
-- Used by the reconciler's deletion sweep: we diff the returned IIDs against
-- the set currently present in GitLab and tear down any cached rows that have
-- no counterpart upstream. Returns full issue rows because the deleter needs
-- the row to cancel agent tasks, fail autopilot runs, and clean up S3 before
-- the DELETE itself runs.
SELECT * FROM issue
WHERE workspace_id = $1 AND gitlab_iid IS NOT NULL;

-- comment cache upserts ----------------------------------------------------

-- name: UpsertCommentFromGitlab :one
-- Multica author refs are nullable: synced rows from human GitLab users have
-- no Multica mapping yet (Phase 3 backfills via user_gitlab_connection).
-- gitlab_author_user_id is always populated so the UI can render "by @user".
INSERT INTO comment (
    workspace_id,
    issue_id,
    author_type,
    author_id,
    gitlab_author_user_id,
    content,
    type,
    gitlab_note_id,
    external_updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (gitlab_note_id) WHERE gitlab_note_id IS NOT NULL DO UPDATE SET
    content = EXCLUDED.content,
    type = EXCLUDED.type,
    author_type = EXCLUDED.author_type,
    author_id = EXCLUDED.author_id,
    gitlab_author_user_id = EXCLUDED.gitlab_author_user_id,
    external_updated_at = EXCLUDED.external_updated_at,
    updated_at = now()
WHERE comment.external_updated_at IS NULL
   OR comment.external_updated_at < EXCLUDED.external_updated_at
RETURNING *;

-- issue_reaction cache upserts --------------------------------------------

-- name: UpsertIssueReactionFromGitlab :one
-- Same pattern as UpsertCommentFromGitlab: Multica actor refs nullable,
-- gitlab_actor_user_id always populated.
INSERT INTO issue_reaction (
    workspace_id,
    issue_id,
    actor_type,
    actor_id,
    gitlab_actor_user_id,
    emoji,
    gitlab_award_id,
    external_updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (gitlab_award_id) WHERE gitlab_award_id IS NOT NULL DO UPDATE SET
    emoji = EXCLUDED.emoji,
    actor_type = EXCLUDED.actor_type,
    actor_id = EXCLUDED.actor_id,
    gitlab_actor_user_id = EXCLUDED.gitlab_actor_user_id,
    external_updated_at = EXCLUDED.external_updated_at
WHERE issue_reaction.external_updated_at IS NULL
   OR issue_reaction.external_updated_at < EXCLUDED.external_updated_at
RETURNING *;

-- name: GetIssueReactionByGitlabAwardID :one
-- Used by the write-through path when the clobber guard short-circuits
-- (pgx.ErrNoRows from the upsert) to load the row the concurrent webhook
-- already wrote.
SELECT * FROM issue_reaction WHERE gitlab_award_id = $1 LIMIT 1;

-- name: UpsertCommentReactionFromGitlab :one
INSERT INTO comment_reaction (
    workspace_id,
    comment_id,
    actor_type,
    actor_id,
    gitlab_actor_user_id,
    emoji,
    gitlab_award_id,
    external_updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (gitlab_award_id) WHERE gitlab_award_id IS NOT NULL DO UPDATE SET
    actor_type = EXCLUDED.actor_type,
    actor_id = EXCLUDED.actor_id,
    gitlab_actor_user_id = EXCLUDED.gitlab_actor_user_id,
    emoji = EXCLUDED.emoji,
    external_updated_at = EXCLUDED.external_updated_at
WHERE comment_reaction.external_updated_at IS NULL
   OR comment_reaction.external_updated_at < EXCLUDED.external_updated_at
RETURNING *;

-- name: GetCommentReactionByGitlabAwardID :one
-- Used by the write-through path when the clobber guard short-circuits
-- (pgx.ErrNoRows from the upsert) to load the row the concurrent webhook
-- already wrote.
SELECT * FROM comment_reaction WHERE gitlab_award_id = $1 LIMIT 1;

-- name: DeleteCommentReactionByGitlabAwardID :exec
DELETE FROM comment_reaction WHERE gitlab_award_id = $1;
