-- Add archive support to issues (soft-delete replacement).
-- archived_at IS NOT NULL means the issue is archived.
ALTER TABLE issue ADD COLUMN archived_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE issue ADD COLUMN archived_by UUID DEFAULT NULL REFERENCES "user"(id);

-- Create index for faster archive filtering
CREATE INDEX idx_issue_archived_at ON issue (archived_at DESC NULLS LAST);

-- Create index for archived by filtering
CREATE INDEX idx_issue_archived_by ON issue (archived_by DESC NULLS LAST);
