-- Restore issue_dependency + origin_type/origin_id on issue.

CREATE TABLE issue_dependency (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    depends_on_issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('blocks', 'blocked_by', 'related'))
);

ALTER TABLE issue ADD COLUMN origin_type TEXT CHECK (origin_type IN ('autopilot'));
ALTER TABLE issue ADD COLUMN origin_id UUID;
CREATE INDEX idx_issue_origin ON issue (origin_type, origin_id);
