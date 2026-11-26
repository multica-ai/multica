-- Issue-scoped labels an autopilot attaches to every issue it creates, in the
-- same transaction as the issue insert. No foreign key: deleting a label
-- removes its id from this array in application code.
ALTER TABLE autopilot
    ADD COLUMN IF NOT EXISTS label_ids UUID[] NOT NULL DEFAULT '{}';
