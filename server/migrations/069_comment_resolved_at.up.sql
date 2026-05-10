ALTER TABLE comment
    ADD COLUMN IF NOT EXISTS resolved_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS resolved_by_type TEXT NULL,
    ADD COLUMN IF NOT EXISTS resolved_by_id UUID NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'comment_resolved_consistency'
          AND conrelid = 'comment'::regclass
    ) THEN
        ALTER TABLE comment
            ADD CONSTRAINT comment_resolved_consistency CHECK (
                (resolved_at IS NULL AND resolved_by_type IS NULL AND resolved_by_id IS NULL)
                OR (resolved_at IS NOT NULL AND resolved_by_type IS NOT NULL AND resolved_by_id IS NOT NULL)
            );
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS comment_issue_resolved_at_idx ON comment (issue_id, resolved_at);
