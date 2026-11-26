-- name: GetIssueDescriptionForCollab :one
-- The collaboration service's view of a description: the stored text plus the
-- Yjs state last saved with it, which is only reusable at the same revision.
SELECT i.workspace_id, i.description, i.description_revision,
    s.ystate AS ystate, s.revision AS ystate_revision
FROM issue i
LEFT JOIN issue_description_collab_state s ON s.issue_id = i.id
WHERE i.id = $1;

-- name: LockIssueByIDForUpdate :one
SELECT * FROM issue WHERE id = $1 FOR UPDATE;

-- name: UpsertIssueDescriptionCollabState :exec
INSERT INTO issue_description_collab_state (issue_id, workspace_id, ystate, revision)
VALUES (@issue_id, @workspace_id, @ystate, @revision)
ON CONFLICT (issue_id) DO UPDATE SET
    ystate = EXCLUDED.ystate,
    revision = EXCLUDED.revision,
    updated_at = now();

-- name: InsertIssueDescriptionRevision :exec
INSERT INTO issue_description_revision (issue_id, workspace_id, revision, content)
VALUES (@issue_id, @workspace_id, @revision, @content)
ON CONFLICT (issue_id, revision) DO UPDATE SET content = EXCLUDED.content;

-- name: PruneIssueDescriptionRevisions :exec
DELETE FROM issue_description_revision
WHERE issue_id = @issue_id AND revision < @keep_from;

-- name: GetIssueDescriptionRevisionContent :one
SELECT content FROM issue_description_revision
WHERE issue_id = @issue_id AND revision = @revision;

-- name: HasRecentLiveDescriptionActivity :one
-- A live editing session records one description_updated entry per editor:
-- saves within 30 minutes of that editor's last live entry add none.
SELECT EXISTS (
    SELECT 1 FROM activity_log
    WHERE issue_id = @issue_id
      AND action = 'description_updated'
      AND actor_type = @actor_type
      AND actor_id = @actor_id
      AND details->>'live' = 'true'
      AND created_at > now() - interval '30 minutes'
);

-- name: LinkUnboundAttachmentsToIssue :execrows
-- Binds files uploaded into a live description to its issue, as the solo
-- editor does with attachment_ids on save. Only files one of the editors
-- uploaded and nothing owns yet.
UPDATE attachment SET issue_id = @issue_id
WHERE workspace_id = @workspace_id
  AND id = ANY(@attachment_ids::uuid[])
  AND issue_id IS NULL
  AND comment_id IS NULL
  AND chat_session_id IS NULL
  AND chat_message_id IS NULL
  AND source_context_id IS NULL
  AND uploader_id = ANY(@uploader_ids::uuid[]);
