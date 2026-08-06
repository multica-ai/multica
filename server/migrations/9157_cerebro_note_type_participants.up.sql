-- FIR-3589 item 6: a recurring note type (a cycle / business review) can carry
-- a list of participants — the people and agents who attend. Stored as a JSON
-- array of {type: 'member'|'agent', id: uuid} so the list travels with the type
-- without a join table.

ALTER TABLE cerebro_note_type
    ADD COLUMN IF NOT EXISTS participants JSONB NOT NULL DEFAULT '[]'::jsonb;
