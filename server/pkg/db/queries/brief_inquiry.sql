-- The Secretary's inquiries to squad leaders. Every query filters by
-- workspace_id; there are no foreign keys.

-- name: LockBriefInquiryWorkspace :exec
-- Serializes asks within one workspace so the daily cap cannot be passed by two
-- concurrent asks. Released at commit.
SELECT pg_advisory_xact_lock(hashtext('brief_inquiry'), hashtext(sqlc.arg('workspace_id')::uuid::text));

-- name: CountBriefInquiriesOnDay :one
SELECT count(*) FROM brief_inquiry
WHERE workspace_id = $1 AND asked_on = $2;

-- name: CreateBriefInquiry :one
INSERT INTO brief_inquiry (
    id, workspace_id, asked_by_agent_id, squad_id, issue_id, comment_id, question, asked_on
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
) RETURNING *;

-- name: AnswerOpenBriefInquiries :execrows
-- A reply by a member agent of the asked squad, on the issue the question was
-- asked on, answers every open question on that issue for that squad.
UPDATE brief_inquiry bi
SET answered_at = now(), answer_excerpt = sqlc.arg('answer_excerpt')
WHERE bi.workspace_id = sqlc.arg('workspace_id')
  AND bi.issue_id = sqlc.arg('issue_id')
  AND bi.answered_at IS NULL
  AND EXISTS (
      SELECT 1 FROM squad_member sm
      WHERE sm.squad_id = bi.squad_id AND sm.member_type = 'agent' AND sm.member_id = sqlc.arg('agent_id')
  );

-- name: ListBriefInquiriesForDigest :many
-- Inquiries asked on or after the given UTC day, newest first. The cap bounds
-- each day to a few rows, so the digest takes them all.
SELECT * FROM brief_inquiry
WHERE workspace_id = $1 AND asked_on >= $2
ORDER BY asked_on DESC, created_at DESC;

-- name: DeleteWorkspaceBriefInquiries :exec
-- Workspace teardown: inquiries have no foreign keys to cascade.
DELETE FROM brief_inquiry WHERE workspace_id = $1;
