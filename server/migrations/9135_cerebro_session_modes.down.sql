ALTER TABLE chat_session DROP COLUMN IF EXISTS mode;

ALTER TABLE cerebro_session DROP CONSTRAINT IF EXISTS cerebro_session_mode_check;
UPDATE cerebro_session SET mode = 'default' WHERE mode IN ('auto', 'build', 'research', 'review');
ALTER TABLE cerebro_session ALTER COLUMN mode SET DEFAULT 'default';
ALTER TABLE cerebro_session
    ADD CONSTRAINT cerebro_session_mode_check CHECK (mode IN ('default', 'plan'));
