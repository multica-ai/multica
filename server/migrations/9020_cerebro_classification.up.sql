-- JEH-1079: per-row classification labels on issues, comments, and
-- attachments. Persona reads this attribute from /v1/check requests and
-- decides allow/deny + per-field masking against the actor's grants.
--
-- Default 'internal' is the safe-default — existing rows become
-- internal-labelled in place, no backfill needed. Operators can promote
-- to 'pii' or 'secret' on rows that hold customer data; demote to
-- 'public' on rows that should be visible to anonymous link viewers.
--
-- members / agents / users do NOT get this column: their field-level
-- classification (email = pii, etc.) is static and lives in the cerebro
-- mask package, not per-row in the database.
--
-- ADD COLUMN IF NOT EXISTS for idempotency (TestMigrationsAreIdempotent).
ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS classification TEXT NOT NULL DEFAULT 'internal';

ALTER TABLE comment
    ADD COLUMN IF NOT EXISTS classification TEXT NOT NULL DEFAULT 'internal';

ALTER TABLE attachment
    ADD COLUMN IF NOT EXISTS classification TEXT NOT NULL DEFAULT 'internal';

-- CHECK constraints can't use IF NOT EXISTS, so guard with DO blocks.
-- Wrapped in a single block per table so the lookup + add are atomic.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'issue_classification_check'
    ) THEN
        ALTER TABLE issue
            ADD CONSTRAINT issue_classification_check
            CHECK (classification IN ('public', 'internal', 'pii', 'secret'));
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'comment_classification_check'
    ) THEN
        ALTER TABLE comment
            ADD CONSTRAINT comment_classification_check
            CHECK (classification IN ('public', 'internal', 'pii', 'secret'));
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'attachment_classification_check'
    ) THEN
        ALTER TABLE attachment
            ADD CONSTRAINT attachment_classification_check
            CHECK (classification IN ('public', 'internal', 'pii', 'secret'));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_issue_classification ON issue(workspace_id, classification);
CREATE INDEX IF NOT EXISTS idx_comment_classification ON comment(issue_id, classification);
CREATE INDEX IF NOT EXISTS idx_attachment_classification ON attachment(workspace_id, classification);
