ALTER TABLE cerebro_round
    ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'batch'
    CHECK (mode IN ('live', 'batch'));
