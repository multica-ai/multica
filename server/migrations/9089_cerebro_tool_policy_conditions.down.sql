-- Drop the optional Condition column.

ALTER TABLE cerebro_tool_policy
    DROP COLUMN IF EXISTS conditions;
