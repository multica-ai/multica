ALTER TABLE conversation ADD COLUMN completed_turn_index INTEGER NOT NULL DEFAULT -1;
ALTER TABLE conversation ADD COLUMN completed_turn_id TEXT NOT NULL DEFAULT '';
ALTER TABLE conversation ADD COLUMN completion_seq INTEGER NOT NULL DEFAULT 0;
ALTER TABLE conversation ADD COLUMN read_completion_seq INTEGER NOT NULL DEFAULT 0;
ALTER TABLE conversation ADD COLUMN read_turn_index INTEGER NOT NULL DEFAULT -1;

-- Existing successful history is the baseline; subsequently completed turns are unread.
UPDATE conversation SET completed_turn_index = COALESCE((
    SELECT MAX(turn_index) FROM turn WHERE conversation_id = conversation.id AND status = 'completed'
), -1);
UPDATE conversation SET completed_turn_id = COALESCE((
    SELECT id FROM turn WHERE conversation_id = conversation.id AND turn_index = conversation.completed_turn_index
), ''), read_turn_index = completed_turn_index;
