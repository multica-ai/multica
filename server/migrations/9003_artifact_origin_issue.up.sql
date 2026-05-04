-- Origin issue: which issue the artifact was created in the context of, even
-- if the artifact's *scope* is workspace or project. This preserves the
-- "where did this come from" trail so docs that an agent produced while
-- working an issue stay discoverable from that issue.

ALTER TABLE artifact ADD COLUMN IF NOT EXISTS origin_issue_id UUID REFERENCES issue(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_artifact_origin_issue ON artifact(origin_issue_id) WHERE origin_issue_id IS NOT NULL;
