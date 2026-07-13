-- FIR-3102 — a note comment can carry its OWN issue.
--
-- Product model for note<->issue coupling: a note couples to one issue, while
-- an individual comment can couple EITHER to its own separate issue OR inherit
-- the note's overarching issue. This column models "this comment has its own
-- issue":
--   issue_id IS NULL  -> the comment inherits the note's issue (the default),
--   issue_id = <uuid> -> the comment was turned into / linked to its own issue
--                        (e.g. via "Create issue from this comment").
--
-- ON DELETE SET NULL: deleting the issue just detaches the comment; it never
-- cascade-deletes the note discussion. The backlink from the issue back to the
-- source note lives on cerebro_issue_reference (object='note'), not here.
ALTER TABLE cerebro_note_comment
    ADD COLUMN IF NOT EXISTS issue_id UUID REFERENCES issue(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_cerebro_note_comment_issue
    ON cerebro_note_comment (issue_id) WHERE issue_id IS NOT NULL;
