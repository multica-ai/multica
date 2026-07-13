-- Restore the legacy reference representation before 9131 removes the
-- comment-level issue column. ON CONFLICT keeps rollback idempotent.
INSERT INTO cerebro_note_reference (
    note_id, object, ref_id, metadata, created_by_type, created_by_id
)
SELECT a.id, 'issue', a.issue_id::text, '{}'::jsonb, a.author_type, a.author_id
FROM artifact AS a
WHERE a.issue_id IS NOT NULL
ON CONFLICT (note_id, object, ref_id) DO NOTHING;

INSERT INTO cerebro_note_reference (
    note_id, object, ref_id, metadata, created_by_type, created_by_id
)
SELECT c.note_id, 'issue', c.issue_id::text, '{}'::jsonb,
       c.author_type, c.author_id
FROM cerebro_note_comment AS c
WHERE c.issue_id IS NOT NULL
ON CONFLICT (note_id, object, ref_id) DO NOTHING;

DROP INDEX IF EXISTS idx_issue_note_comment_origin_unique;
