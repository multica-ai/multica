-- Validate the widened issue_origin_type_check that migration 259 added
-- NOT VALID. This takes SHARE UPDATE EXCLUSIVE while scanning issue, which
-- permits normal SELECT/INSERT/UPDATE/DELETE traffic to continue — the whole
-- point of splitting the two steps across migration files (the 197/198
-- pattern).
--
-- 259 only WIDENED the allowed set, so every pre-existing row already
-- satisfies the constraint and this scan cannot fail on legacy data.
ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;
