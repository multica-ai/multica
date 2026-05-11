ALTER TABLE comment
    ADD COLUMN resolved_at TIMESTAMPTZ NULL,
    ADD COLUMN resolved_by_type TEXT NULL,
    ADD COLUMN resolved_by_id UUID NULL;

ALTER TABLE comment
    ADD CONSTRAINT comment_resolved_consistency CHECK (
        (resolved_at IS NULL AND resolved_by_type IS NULL AND resolved_by_id IS NULL)
        OR (resolved_at IS NOT NULL AND resolved_by_type IS NOT NULL AND resolved_by_id IS NOT NULL)
    );

CREATE INDEX comment_issue_resolved_at_idx ON comment (issue_id, resolved_at);
