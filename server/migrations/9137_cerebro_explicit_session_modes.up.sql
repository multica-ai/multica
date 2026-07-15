UPDATE cerebro_session SET mode = 'build' WHERE mode IN ('auto', 'default');
UPDATE cerebro_session SET mode = phase WHERE phase IN ('plan', 'build', 'review');
ALTER TABLE cerebro_session ALTER COLUMN mode SET DEFAULT 'build';
ALTER TABLE cerebro_session DROP CONSTRAINT IF EXISTS cerebro_session_mode_check;
ALTER TABLE cerebro_session ADD CONSTRAINT cerebro_session_mode_check CHECK (mode IN ('auto', 'plan', 'build', 'research', 'review'));

UPDATE chat_session SET mode = 'build' WHERE mode IN ('auto', 'default');
ALTER TABLE chat_session ALTER COLUMN mode SET DEFAULT 'build';
ALTER TABLE chat_session DROP CONSTRAINT IF EXISTS chat_session_mode_check;
ALTER TABLE chat_session ADD CONSTRAINT chat_session_mode_check CHECK (mode IN ('auto', 'plan', 'build', 'research', 'review'));
