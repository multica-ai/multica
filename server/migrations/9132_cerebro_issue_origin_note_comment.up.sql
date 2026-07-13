-- FIR-3102: extend issue.origin_type to allow 'note_comment' so an issue
-- created FROM a specific note comment ("Create issue from this comment") can be
-- stamped with origin_id=<cerebro_note_comment.id>. That gives the backlink from
-- the new issue to the exact source comment (and, via the comment's note_id, the
-- source note). The forward link lives on cerebro_note_comment.issue_id (9131).
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'runtime_approval', 'agent_task', 'lark_chat', 'note_comment'));
