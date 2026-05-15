-- CEREBRO-PATCH(migration-083-runtime-visibility): make upstream migration idempotent for fork deploy retries.
ALTER TABLE agent_runtime
    ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'private'
        CHECK (visibility IN ('private', 'public'));
