-- Reverse Phase 2a's cache schema additions.

DROP TABLE IF EXISTS issue_position;
DROP TABLE IF EXISTS gitlab_project_member;
DROP TABLE IF EXISTS issue_gitlab_label;
DROP TABLE IF EXISTS gitlab_label;

ALTER TABLE attachment DROP COLUMN IF EXISTS gitlab_upload_url;

DROP INDEX IF EXISTS idx_issue_reaction_gitlab_award;
ALTER TABLE issue_reaction
    DROP COLUMN IF EXISTS external_updated_at,
    DROP COLUMN IF EXISTS gitlab_award_id;

DROP INDEX IF EXISTS idx_comment_gitlab_note;
ALTER TABLE comment
    DROP COLUMN IF EXISTS external_updated_at,
    DROP COLUMN IF EXISTS gitlab_note_id;

-- creator_id NOT NULL constraint cannot be safely re-added if any rows have
-- NULL — fail noisily if so.
ALTER TABLE issue ALTER COLUMN creator_type SET NOT NULL;
ALTER TABLE issue ALTER COLUMN creator_id SET NOT NULL;

DROP INDEX IF EXISTS idx_issue_gitlab_iid;
ALTER TABLE issue
    DROP COLUMN IF EXISTS external_updated_at,
    DROP COLUMN IF EXISTS gitlab_project_id,
    DROP COLUMN IF EXISTS gitlab_iid;
