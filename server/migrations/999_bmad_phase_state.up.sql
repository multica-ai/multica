-- BMAD phase_state column + partial indexes.
-- Idempotent: DB may already have these (applied out-of-band by bmad_002).
-- Keeping this migration lets sqlc see the column when regenerating.

ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS phase_state jsonb;

CREATE INDEX IF NOT EXISTS idx_issue_phase_state_phase
    ON issue ((phase_state->>'phase'))
    WHERE phase_state IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_issue_phase_state_status
    ON issue ((phase_state->>'status'))
    WHERE phase_state IS NOT NULL;

COMMENT ON COLUMN issue.phase_state IS
    'BMAD phase-progress tracking. Managed by the bmad-sidecar service.';

-- Extend the status CHECK with BMAD board columns if not already.
-- Runs as a DO block so we can guard against duplicate extension.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'issue_status_check'
          AND pg_get_constraintdef(oid) LIKE '%planning%'
    ) THEN
        ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;
        ALTER TABLE issue ADD CONSTRAINT issue_status_check CHECK (
            status = ANY (ARRAY[
                'backlog','todo','in_progress','in_review','done','blocked','cancelled',
                'planning','code_review','fixing','testing','checkpoint','staged'
            ])
        );
    END IF;
END $$;
