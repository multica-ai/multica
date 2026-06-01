-- Reverse 9054. Drop any rows that carry a non-empty resource_pattern first —
-- they have no representation in the pre-9054 schema and silently collapsing
-- them under the narrower unique constraint would risk a constraint violation
-- (two rows that disagree on resource_pattern would suddenly conflict). The
-- DELETE keeps the schema rollback safe; rows with the empty pattern survive
-- and behave exactly as they did before slice 1.
DELETE FROM cerebro_tool_policy WHERE resource_pattern <> '';

DROP INDEX IF EXISTS idx_cerebro_tool_policy_lookup;
CREATE INDEX IF NOT EXISTS idx_cerebro_tool_policy_lookup
    ON cerebro_tool_policy (workspace_id, tool_key);

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_unique;

ALTER TABLE cerebro_tool_policy
    ADD CONSTRAINT cerebro_tool_policy_unique
        UNIQUE (workspace_id, tool_key, layer, subject_id);

ALTER TABLE cerebro_tool_policy
    DROP COLUMN IF EXISTS resource_pattern;
