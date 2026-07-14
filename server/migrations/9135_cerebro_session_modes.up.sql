-- FIR-3111: five fixed execution profiles on Issue and Chat sessions.
ALTER TABLE cerebro_session DROP CONSTRAINT IF EXISTS cerebro_session_mode_check;
UPDATE cerebro_session SET mode = 'build' WHERE mode = 'default';
ALTER TABLE cerebro_session ALTER COLUMN mode SET DEFAULT 'auto';
ALTER TABLE cerebro_session
    ADD CONSTRAINT cerebro_session_mode_check
    CHECK (mode IN ('auto', 'plan', 'build', 'research', 'review'));

ALTER TABLE chat_session
    ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'auto';
ALTER TABLE chat_session DROP CONSTRAINT IF EXISTS chat_session_mode_check;
ALTER TABLE chat_session
    ADD CONSTRAINT chat_session_mode_check
    CHECK (mode IN ('auto', 'plan', 'build', 'research', 'review'));
