-- FIR-3102 — artifact.issue_id is the single note-level issue coupling.
-- Existing issue references are migrated without loss: the oldest becomes the
-- primary issue when the note has none, and every extra issue becomes a
-- comment-level link. After this migration, issue references no longer drive
-- note comment routing.

-- One source comment may create at most one issue, including under concurrent
-- retries. The handler recovers the existing row through GetIssueByOrigin.
CREATE UNIQUE INDEX IF NOT EXISTS idx_issue_note_comment_origin_unique
    ON issue (origin_type, origin_id)
    WHERE origin_type = 'note_comment';

-- Promote the oldest valid legacy issue reference when the note has no primary
-- issue. An artifact cannot be both project- and issue-scoped.
UPDATE artifact AS a
SET issue_id = (
        SELECT i.id
        FROM cerebro_note_reference AS r
        JOIN issue AS i
          ON i.id::text = r.ref_id
         AND i.workspace_id = a.workspace_id
        WHERE r.note_id = a.id
          AND r.object = 'issue'
        ORDER BY r.created_at, r.id
        LIMIT 1
    ),
    project_id = NULL,
    updated_at = now()
WHERE a.issue_id IS NULL
  AND a.kind IN ('note', 'document')
  AND EXISTS (
      SELECT 1
      FROM cerebro_note_reference AS r
      JOIN issue AS i
        ON i.id::text = r.ref_id
       AND i.workspace_id = a.workspace_id
      WHERE r.note_id = a.id AND r.object = 'issue'
  );

-- Preserve every additional legacy issue as an explicit comment-level link.
INSERT INTO cerebro_note_comment (
    note_id, kind, body, author_type, author_id, issue_id, created_at, updated_at
)
SELECT r.note_id,
       'comment',
       'Linked issue preserved from an earlier note reference.',
       r.created_by_type,
       r.created_by_id,
       i.id,
       r.created_at,
       r.updated_at
FROM cerebro_note_reference AS r
JOIN artifact AS a ON a.id = r.note_id
JOIN issue AS i
  ON i.id::text = r.ref_id
 AND i.workspace_id = a.workspace_id
WHERE r.object = 'issue'
  AND i.id IS DISTINCT FROM a.issue_id
  AND NOT EXISTS (
      SELECT 1
      FROM cerebro_note_comment AS c
      WHERE c.note_id = r.note_id AND c.issue_id = i.id
  );

-- The valid issue links now live only in artifact.issue_id or on a comment.
DELETE FROM cerebro_note_reference AS r
USING artifact AS a, issue AS i
WHERE r.note_id = a.id
  AND r.object = 'issue'
  AND i.id::text = r.ref_id
  AND i.workspace_id = a.workspace_id;
