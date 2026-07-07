ALTER TABLE project
    ADD COLUMN issue_prefix TEXT,
    ADD CONSTRAINT project_issue_prefix_format
        CHECK (
            issue_prefix IS NULL
            OR issue_prefix = upper(issue_prefix)
            AND issue_prefix ~ '^[A-Z][A-Z0-9]{1,9}$'
        );
