ALTER TABLE cerebro_session
    ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'default'
    CHECK (mode IN ('default', 'plan'));
