-- Cap agent.description at 255 characters so the column matches the
-- product-side limit enforced by the UI (counter + disabled save) and the
-- handler validation. The TEXT type stays — char_length is a constraint
-- on top, not a type change, which keeps the existing column data intact
-- and avoids a rewrite of the table.
--
-- Pre-flight truncate: any existing row that already exceeds the new
-- ceiling gets clipped to 255 chars. Without this the constraint add
-- would abort on the first over-limit row. Affected rows are rare (no UI
-- ever encouraged long descriptions), but defensive trimming keeps
-- self-hosted installs from blocking on the migration.
UPDATE agent
SET description = substring(description from 1 for 255)
WHERE char_length(description) > 255;

-- Two-step add: NOT VALID skips the table scan and only briefly takes an
-- ACCESS EXCLUSIVE lock to register the constraint, so concurrent writes
-- are not blocked. New inserts/updates are enforced from this point on.
-- The follow-up VALIDATE CONSTRAINT runs the actual scan under SHARE
-- UPDATE EXCLUSIVE, which permits concurrent writes during the scan.
--
-- At today's agent-table size the difference is invisible, but the
-- pattern is free defensively and matches Squawk's recommended migration
-- shape (https://squawkhq.com/docs/constraint-missing-not-valid).
ALTER TABLE agent
    ADD CONSTRAINT agent_description_length
    CHECK (char_length(description) <= 255) NOT VALID;

ALTER TABLE agent VALIDATE CONSTRAINT agent_description_length;
