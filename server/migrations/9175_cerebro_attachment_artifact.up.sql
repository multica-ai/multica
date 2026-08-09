-- 9175_cerebro_attachment_artifact: give a document image an owner row (FIR-4699).
--
-- attachment.artifact_id links an uploaded file to the artifact (Note/Document)
-- it belongs to, the same way issue_id / comment_id / chat_message_id already
-- scope an attachment to its owner. Without it a document image is an orphan
-- row scoped only to the workspace, so it can be neither listed for its document
-- nor cleaned up when the document is deleted.
--
-- ON DELETE CASCADE: deleting the artifact deletes its image rows, matching the
-- issue/comment associations. Nullable: existing attachments and every non-
-- document upload leave it NULL, so this is behaviour-preserving.
--
-- Width, alignment and caption are NOT stored here — they live in artifact.body
-- (the Markdown). This column is the owner link only.
ALTER TABLE attachment ADD COLUMN IF NOT EXISTS artifact_id UUID REFERENCES artifact(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_attachment_artifact_id ON attachment (artifact_id);
