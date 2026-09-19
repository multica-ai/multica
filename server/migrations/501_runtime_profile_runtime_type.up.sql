-- Empty defaults support older servers writing profiles during rollout.
ALTER TABLE runtime_profile ADD COLUMN runtime_type text NOT NULL DEFAULT '';
UPDATE runtime_profile SET runtime_type = protocol_family;
