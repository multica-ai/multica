ALTER TABLE issue_property
    -- CEREBRO-PATCH(issue-properties-release-gates): deployment recovery may retry this rollback.
    DROP COLUMN IF EXISTS icon;
